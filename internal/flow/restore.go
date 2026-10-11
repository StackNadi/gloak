package flow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pterm/pterm"

	"gloak/internal/core"
	"gloak/internal/crypto"
	"gloak/internal/repo"
	"gloak/internal/storage"
)

func RunRestore(snapshotID, identityPath, remote, outputDir, pathFilter string) error {
	backend := storage.NewRcloneBackend(remote)
	outputPath, err := runRestore(snapshotID, outputDir, identityPath, backend, pathFilter)
	if err != nil {
		return err
	}
	pterm.Success.Println("Restore completed successfully! File saved to:", outputPath)
	return nil
}

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

func runTreeRestore(m *core.TreeManifest, r *repo.Repo, outputDir, pathFilter string, backend storage.Backend) (string, error) {
	if len(m.Paths) == 0 {
		return "", fmt.Errorf("tree snapshot has no source paths")
	}
	rootName := filepath.Base(filepath.Clean(m.Paths[0]))
	if rootName == "" || rootName == "." || rootName == string(filepath.Separator) {
		rootName = m.UUID
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}
	destRoot := filepath.Join(outputDir, rootName)

	for _, entry := range m.Entries {
		if err := core.ValidateRelPath(entry.RelPath); err != nil {
			return "", fmt.Errorf("unsafe path in manifest: %w", err)
		}
		if pathFilter != "" {
			if entry.RelPath != pathFilter && !strings.HasPrefix(entry.RelPath, pathFilter+"/") {
				continue
			}
		}
		target := filepath.Join(destRoot, filepath.FromSlash(entry.RelPath))
		// Defense in depth: ensure target stays under destRoot.
		if !strings.HasPrefix(target, destRoot+string(filepath.Separator)) && target != destRoot {
			return "", fmt.Errorf("restored path escapes output root: %s", entry.RelPath)
		}
		switch entry.Type {
		case "dir":
			if err := os.MkdirAll(target, os.FileMode(entry.Mode)&0777); err != nil {
				return "", fmt.Errorf("failed to create directory %s: %w", entry.RelPath, err)
			}
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
	return destRoot, nil
}

func restoreTreeFile(entry core.TreeEntry, r *repo.Repo, backend storage.Backend, target string) error {
	outFile, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(entry.Mode)&0777)
	if err != nil {
		return fmt.Errorf("failed to create output file %s: %w", entry.RelPath, err)
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
	if mt, err := time.Parse(time.RFC3339, entry.Mtime); err == nil {
		_ = os.Chtimes(target, mt, mt)
	}
	return nil
}
