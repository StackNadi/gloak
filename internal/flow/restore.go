package flow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pterm/pterm"

	"gloak/internal/core"
	"gloak/internal/crypto"
	"gloak/internal/repo"
	"gloak/internal/storage"
)

// RunRestore restores a standalone file backup or repository snapshot to
// outputDir. For trees, pathFilter selects an exact relative path and its
// descendants; empty selects all entries. File backups ignore the filter.
// Access, decryption, integrity, and filesystem errors are returned; existing
// files may be truncated and partial output remains on failure.
func RunRestore(snapshotID, identityPath, remote, outputDir, pathFilter string) error {
	backend := storage.NewRcloneBackend(remote)
	outputPath, err := runRestore(snapshotID, outputDir, identityPath, backend, pathFilter)
	if err != nil {
		return err
	}
	pterm.Success.Println("Restore completed successfully! File saved to:", outputPath)
	return nil
}

// runRestore restores a backup and returns the output file path or tree root.
// A downloadable locator selects standalone file mode; any locator download
// failure falls back to the repository flow. pathFilter applies only to trees.
// Repository, manifest, and restore errors are returned without cleaning up output.
func runRestore(snapshotID, outputDir, identityPath string, backend storage.Backend, pathFilter string) (string, error) {
	if isFileMode(backend, snapshotID) {
		return runFileRestore(snapshotID, identityPath, outputDir, backend)
	}
	r, err := repo.Open(backend, identityPath)
	if err != nil {
		return "", err
	}
	kind, fileM, treeM, err := readAnyManifest(backend, identityPath, snapshotID)
	if err != nil {
		return "", err
	}
	if kind == core.KindTree {
		return runTreeRestore(treeM, r, outputDir, pathFilter, backend)
	}
	return runFileRestoreFromManifest(fileM, r, backend, outputDir)
}

// runFileRestoreFromManifest writes a repository file snapshot under outputDir
// and returns its path, truncating any existing file. It checks chunk plaintext
// sizes and hashes and the overall payload hash. Invalid names or chunk paths,
// filesystem, download, decryption, and integrity errors can leave partial output;
// file close errors are ignored.
func runFileRestoreFromManifest(manifest *core.Manifest, r *repo.Repo, backend storage.Backend, outputDir string) (string, error) {
	if filepath.Base(manifest.OriginalName) != manifest.OriginalName {
		return "", fmt.Errorf("invalid original filename in manifest: %s", manifest.OriginalName)
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}
	outputPath := filepath.Join(outputDir, manifest.OriginalName)
	outFile, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return "", fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	payloadHasher := sha256.New()
	for _, meta := range manifest.Chunks {
		if err := validateChunkPath(meta.Name); err != nil {
			return "", err
		}
		var buf bytes.Buffer
		if err := backend.Download(meta.Name, &buf); err != nil {
			return "", fmt.Errorf("CHUNK CORRUPT %d (%s): missing object: %w", meta.Index, meta.Name, err)
		}
		plain, err := crypto.OpenChunk(r.MasterKey, meta.PlainSHA256, buf.Bytes())
		if err != nil {
			return "", fmt.Errorf("CHUNK CORRUPT %d (%s): %w", meta.Index, meta.Name, err)
		}
		if int64(len(plain)) != meta.PlainSize || hashBytes(plain) != meta.PlainSHA256 {
			return "", fmt.Errorf("CHUNK CORRUPT %d (%s): plaintext mismatch", meta.Index, meta.Name)
		}
		if _, err := outFile.Write(plain); err != nil {
			return "", fmt.Errorf("failed to write chunk %d: %w", meta.Index, err)
		}
		payloadHasher.Write(plain)
	}

	if hex.EncodeToString(payloadHasher.Sum(nil)) != manifest.PayloadSHA256 {
		return "", fmt.Errorf("fatal: overall payload hash does not match manifest")
	}
	return outputPath, nil
}

// runTreeRestore restores entries below a root named for the first source
// path, falling back to the snapshot UUID for an empty, . or root basename.
// pathFilter selects an exact slash-separated path and its descendants; empty
// selects all. It returns the destination root even when nothing matches.
// Missing source paths, unsafe entry paths, unknown selected entry types, and
// filesystem or chunk restore errors are returned; partial output remains.
func runTreeRestore(m *core.TreeManifest, r *repo.Repo, outputDir, pathFilter string, backend storage.Backend) (string, error) {
	if len(m.Paths) == 0 {
		return "", fmt.Errorf("tree snapshot has no source paths")
	}
	// The manifest is remote input: fall back to the snapshot UUID when
	// the recorded root name is not a single safe path segment, and
	// reject the manifest when even the UUID is unsafe.
	rootName := filepath.Base(filepath.Clean(m.Paths[0]))
	if err := core.ValidateRelPath(rootName); err != nil {
		rootName = m.UUID
	}
	if err := core.ValidateRelPath(rootName); err != nil {
		return "", fmt.Errorf("unsafe restore root name: %w", err)
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}
	destRoot := filepath.Join(outputDir, rootName)
	// Defense in depth: the join above must stay under outputDir.
	if rel, err := filepath.Rel(outputDir, destRoot); err != nil ||
		rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("restore root escapes output directory: %s", rootName)
	}

	type dirSetting struct {
		path  string
		mode  os.FileMode
		mtime time.Time
	}
	var dirSettings []dirSetting
	var matched int

	for _, entry := range m.Entries {
		if err := core.ValidateRelPath(entry.RelPath); err != nil {
			return "", fmt.Errorf("unsafe path in manifest: %w", err)
		}
		if pathFilter != "" {
			if entry.RelPath != pathFilter && !strings.HasPrefix(entry.RelPath, pathFilter+"/") {
				continue
			}
		}
		matched++
		target := filepath.Join(destRoot, filepath.FromSlash(entry.RelPath))
		// Defense in depth: ensure target stays under destRoot.
		if !strings.HasPrefix(target, destRoot+string(filepath.Separator)) && target != destRoot {
			return "", fmt.Errorf("restored path escapes output root: %s", entry.RelPath)
		}
		switch entry.Type {
		case "dir":
			// Keep the directory writable while children are restored;
			// the recorded mode is applied after the loop.
			if err := os.MkdirAll(target, 0700); err != nil {
				return "", fmt.Errorf("failed to create directory %s: %w", entry.RelPath, err)
			}
			if err := os.Chmod(target, 0700); err != nil {
				return "", fmt.Errorf("failed to open directory %s for restore: %w", entry.RelPath, err)
			}
			setting := dirSetting{path: target, mode: os.FileMode(entry.Mode) & 0777}
			if mt, err := time.Parse(time.RFC3339, entry.Mtime); err == nil {
				setting.mtime = mt
			}
			dirSettings = append(dirSettings, setting)
		case "file":
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return "", fmt.Errorf("failed to create parent for %s: %w", entry.RelPath, err)
			}
			if err := restoreTreeFile(entry, r, backend, target); err != nil {
				return "", err
			}
		default:
			return "", fmt.Errorf("unknown tree entry type %q for %s", entry.Type, entry.RelPath)
		}
	}
	if pathFilter != "" && matched == 0 {
		return "", fmt.Errorf("path filter %q matches nothing in snapshot %s", pathFilter, m.UUID)
	}

	// Apply directory modes after every child is written, deepest paths
	// first so a read-only parent never blocks chmod on its children.
	sort.Slice(dirSettings, func(i, j int) bool {
		return len(dirSettings[i].path) > len(dirSettings[j].path)
	})
	for _, d := range dirSettings {
		if err := os.Chmod(d.path, d.mode); err != nil {
			return "", fmt.Errorf("failed to set mode on %s: %w", d.path, err)
		}
		if !d.mtime.IsZero() {
			_ = os.Chtimes(d.path, d.mtime, d.mtime)
		}
	}
	return destRoot, nil
}

// restoreTreeFile creates or truncates target, writes verified plaintext chunks,
// and attempts to restore its modification time. New files use entry.Mode's
// permission bits, subject to the umask. Path, download, decryption, checksum,
// and file open/write errors are returned and can leave a partial file.
// Invalid timestamps, timestamp update failures, and close errors are ignored.
func restoreTreeFile(entry core.TreeEntry, r *repo.Repo, backend storage.Backend, target string) error {
	mode := os.FileMode(entry.Mode) & 0777
	outFile, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		// An earlier restore may have left the file read-only; make it
		// writable once and retry.
		if chmodErr := os.Chmod(target, mode|0200); chmodErr != nil {
			return fmt.Errorf("failed to create output file %s: %w", entry.RelPath, err)
		}
		outFile, err = os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			return fmt.Errorf("failed to create output file %s: %w", entry.RelPath, err)
		}
	}
	defer outFile.Close()

	for _, meta := range entry.Chunks {
		if err := validateChunkPath(meta.Name); err != nil {
			return err
		}
		var buf bytes.Buffer
		if err := backend.Download(meta.Name, &buf); err != nil {
			return fmt.Errorf("CHUNK CORRUPT %d (%s): missing object: %w", meta.Index, meta.Name, err)
		}
		plain, err := crypto.OpenChunk(r.MasterKey, meta.PlainSHA256, buf.Bytes())
		if err != nil {
			return fmt.Errorf("CHUNK CORRUPT %d (%s): %w", meta.Index, meta.Name, err)
		}
		if int64(len(plain)) != meta.PlainSize || hashBytes(plain) != meta.PlainSHA256 {
			return fmt.Errorf("CHUNK CORRUPT %d (%s): plaintext mismatch", meta.Index, meta.Name)
		}
		if _, err := outFile.Write(plain); err != nil {
			return fmt.Errorf("failed to write chunk %d: %w", meta.Index, err)
		}
	}
	// Re-applying the mode matters when the file already existed (a
	// previous restore may have left it read-only or writable).
	if err := os.Chmod(target, mode); err != nil {
		return fmt.Errorf("failed to set mode on %s: %w", entry.RelPath, err)
	}
	if mt, err := time.Parse(time.RFC3339, entry.Mtime); err == nil {
		_ = os.Chtimes(target, mt, mt)
	}
	return nil
}
