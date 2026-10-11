package flow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/pterm/pterm"

	"gloak/internal/chunker"
	"gloak/internal/core"
	"gloak/internal/crypto"
	"gloak/internal/index"
	"gloak/internal/repo"
	"gloak/internal/storage"
)

const indexCheckpointEvery = 64

type BackupResult struct {
	SnapshotID      string
	TotalChunks     int
	UploadedChunks  int
	SkippedChunks   int
	TotalPlainBytes int64
}

// RunBackup backs up filePath into the repository on remote using identityPath.
func RunBackup(filePath, identityPath, remote string) (string, error) {
	backend := storage.NewRcloneBackend(remote)
	snapshotID := core.GenerateUUID()

	spinner, _ := pterm.DefaultSpinner.Start("Backing up...")
	res, err := runBackupPath(filePath, identityPath, backend, snapshotID, func(done int) {
		spinner.UpdateText(fmt.Sprintf("Backed up %d chunks", done))
	})
	if err != nil {
		_ = spinner.Stop()
		return "", err
	}
	_ = spinner.Stop()

	pterm.Success.Println("Backup complete")
	pterm.Printf("Snapshot ID: %s\n", res.SnapshotID)
	pterm.Printf("Chunks: %d total, %d uploaded, %d skipped\n", res.TotalChunks, res.UploadedChunks, res.SkippedChunks)
	pterm.Printf("Data size: %d bytes\n", res.TotalPlainBytes)
	return res.SnapshotID, nil
}

// runBackupPath dispatches to the file or tree backup flow based on whether
// filePath is a regular file or a directory.
func runBackupPath(filePath, identityPath string, backend storage.Backend, snapshotID string, progress func(int)) (BackupResult, error) {
	if _, err := uuid.Parse(snapshotID); err != nil {
		return BackupResult{}, fmt.Errorf("invalid snapshot id: %w", err)
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return BackupResult{}, fmt.Errorf("failed to stat path: %w", err)
	}
	if info.IsDir() {
		return runTreeBackup(filePath, identityPath, backend, snapshotID, progress)
	}
	return runBackup(filePath, identityPath, backend, snapshotID, progress)
}

func runBackup(filePath, identityPath string, backend storage.Backend, snapshotID string, progress func(int)) (BackupResult, error) {
	if _, err := uuid.Parse(snapshotID); err != nil {
		return BackupResult{}, fmt.Errorf("invalid snapshot id: %w", err)
	}

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

	file, err := os.Open(filePath)
	if err != nil {
		return BackupResult{}, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	res := BackupResult{SnapshotID: snapshotID}
	var metas []core.ChunkMeta
	var sinceCheckpoint int
	payloadHasher := sha256.New()

	err = cdc.Split(file, func(chunk []byte) error {
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
		metas = append(metas, core.ChunkMeta{
			Index:           len(metas),
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
	if err != nil {
		return BackupResult{}, err
	}

	if _, err := ix.Save(backend, r.Config.Recipients); err != nil {
		return BackupResult{}, fmt.Errorf("failed to save index: %w", err)
	}

	recipient := ""
	if len(r.Config.Recipients) > 0 {
		recipient = r.Config.Recipients[0]
	}
	manifest := core.Manifest{
		Version:       repo.FormatVersion,
		UUID:          snapshotID,
		OriginalName:  filepath.Base(filePath),
		OriginalSize:  res.TotalPlainBytes,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		Recipient:     recipient,
		PayloadSHA256: hex.EncodeToString(payloadHasher.Sum(nil)),
		Chunks:        metas,
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return BackupResult{}, fmt.Errorf("failed to marshal manifest: %w", err)
	}
	sealed, err := crypto.EncryptMulti(r.Config.Recipients, manifestJSON)
	if err != nil {
		return BackupResult{}, fmt.Errorf("failed to encrypt manifest: %w", err)
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
