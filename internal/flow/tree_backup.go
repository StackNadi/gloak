package flow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"gloak/internal/chunker"
	"gloak/internal/core"
	"gloak/internal/crypto"
	"gloak/internal/index"
	"gloak/internal/repo"
	"gloak/internal/storage"
)

func runTreeBackup(dirPath, identityPath string, backend storage.Backend, snapshotID string, progress func(int)) (BackupResult, error) {
	r, err := repo.Open(backend, identityPath)
	if err != nil {
		return BackupResult{}, err
	}
	ix, err := index.Load(backend, identityPath)
	if err != nil {
		return BackupResult{}, fmt.Errorf("failed to load index: %w", err)
	}
	cdc, err := chunker.NewCDC(r.Config.ChunkMin, r.Config.ChunkAvg, r.Config.ChunkMax)
	if err != nil {
		return BackupResult{}, fmt.Errorf("invalid chunk config: %w", err)
	}

	abs, err := filepath.Abs(dirPath)
	if err != nil {
		return BackupResult{}, err
	}
	// Resolve symlinks before the walk: walking a symlinked root never
	// matches the root itself, and every relative path would start with
	// ../, producing a silently empty snapshot. The manifest keeps the
	// path the user passed so restore still uses the familiar name.
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return BackupResult{}, fmt.Errorf("failed to resolve %s: %w", abs, err)
	}

	var entries []core.TreeEntry
	var sinceCheckpoint int
	res := BackupResult{SnapshotID: snapshotID}

	walkErr := filepath.WalkDir(resolved, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if p == resolved {
			return nil
		}
		rel, err := filepath.Rel(resolved, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := uint32(info.Mode().Perm())
		mtime := info.ModTime().UTC().Format(time.RFC3339)

		switch {
		case d.IsDir():
			entries = append(entries, core.TreeEntry{
				RelPath: rel,
				Type:    "dir",
				Mode:    mode,
				Mtime:   mtime,
			})
			return nil
		case !info.Mode().IsRegular():
			// Symlinks and special files are skipped.
			return nil
		}

		file, err := os.Open(p)
		if err != nil {
			return err
		}
		defer file.Close()

		var chunks []core.ChunkMeta
		payloadHasher := sha256.New()
		splitErr := cdc.Split(file, func(chunk []byte) error {
			payloadHasher.Write(chunk)
			hash := hashBytes(chunk)
			res.TotalChunks++
			res.TotalPlainBytes += int64(len(chunk))
			dataPath := repo.DataPath(hash)

			if ix.Has(hash) {
				res.SkippedChunks++
			} else {
				sealed, err := crypto.SealChunk(r.MasterKey, hash, chunk)
				if err != nil {
					return fmt.Errorf("failed to seal chunk %s: %w", hash, err)
				}
				encSum := sha256.Sum256(sealed)
				if err := backend.Upload(dataPath, bytes.NewReader(sealed)); err != nil {
					return fmt.Errorf("failed to upload chunk %s: %w", hash, err)
				}
				ix.Add(index.Entry{
					PlainSHA256:     hash,
					PlainSize:       int64(len(chunk)),
					EncryptedSize:   int64(len(sealed)),
					EncryptedSHA256: hex.EncodeToString(encSum[:]),
				})
				res.UploadedChunks++
				sinceCheckpoint++
				if sinceCheckpoint >= indexCheckpointEvery {
					if _, err := ix.Save(backend, r.Config.Recipients); err != nil {
						return fmt.Errorf("failed to checkpoint index: %w", err)
					}
					sinceCheckpoint = 0
				}
			}

			encEntry, _ := ix.Get(hash)
			chunks = append(chunks, core.ChunkMeta{
				Index:           len(chunks),
				Name:            dataPath,
				PlainSize:       encEntry.PlainSize,
				PlainSHA256:     hash,
				EncryptedSize:   encEntry.EncryptedSize,
				EncryptedSHA256: encEntry.EncryptedSHA256,
			})
			if progress != nil {
				progress(res.TotalChunks)
			}
			return nil
		})
		if splitErr != nil {
			return splitErr
		}
		entries = append(entries, core.TreeEntry{
			RelPath: rel,
			Type:    "file",
			Mode:    mode,
			Mtime:   mtime,
			Size:    info.Size(),
			Chunks:  chunks,
		})
		return nil
	})
	if walkErr != nil {
		return BackupResult{}, walkErr
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].RelPath < entries[j].RelPath
	})

	if _, err := ix.Save(backend, r.Config.Recipients); err != nil {
		return BackupResult{}, fmt.Errorf("failed to save index: %w", err)
	}

	host, _ := os.Hostname()
	manifest := core.TreeManifest{
		Version:   repo.FormatVersion,
		UUID:      snapshotID,
		Kind:      core.KindTree,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Hostname:  host,
		Paths:     []string{abs},
		Entries:   entries,
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return BackupResult{}, fmt.Errorf("failed to marshal tree manifest: %w", err)
	}
	sealed, err := crypto.EncryptMulti(r.Config.Recipients, manifestJSON)
	if err != nil {
		return BackupResult{}, fmt.Errorf("failed to encrypt tree manifest: %w", err)
	}
	snapshotPath, err := repo.SnapshotPath(snapshotID)
	if err != nil {
		return BackupResult{}, err
	}
	if err := backend.Upload(snapshotPath, bytes.NewReader(sealed)); err != nil {
		return BackupResult{}, fmt.Errorf("failed to upload snapshot: %w", err)
	}
	return res, nil
}
