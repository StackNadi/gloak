package flow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/pterm/pterm"

	"gloak/internal/core"
	"gloak/internal/crypto"
	"gloak/internal/storage"
)

func RunUpload(filePath string, recipientKey string, remote string) error {
	return runUploadWithStateAndChunkSize(filePath, recipientKey, storage.NewRcloneBackend(remote), core.GenerateUUID(), remote, defaultUploadStateDir(), core.ChunkSize)
}

func runUpload(filePath string, recipientKey string, backend storage.Backend, backupID string) error {
	return runUploadWithState(filePath, recipientKey, backend, backupID, "", "")
}

func runUploadWithState(filePath string, recipientKey string, backend storage.Backend, backupID string, remote string, stateDir string) error {
	return runUploadWithStateAndChunkSize(filePath, recipientKey, backend, backupID, remote, stateDir, core.ChunkSize)
}

func runUploadWithStateAndChunkSize(filePath string, recipientKey string, backend storage.Backend, backupID string, remote string, stateDir string, chunkSize int64) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}
	originalSize := stat.Size()
	originalName := filepath.Base(filePath)
	state := UploadState{}
	if stateDir != "" {
		absPath, err := filepath.Abs(filePath)
		if err != nil {
			return fmt.Errorf("failed to resolve source path: %w", err)
		}
		state = newUploadStateWithChunkSize(backupID, absPath, originalName, originalSize, remote, recipientKey, chunkSize)
		if err := saveUploadState(stateDir, state); err != nil {
			return err
		}
	}

	pterm.Success.Printf("Backup ID: %s\n", backupID)

	p, _ := pterm.DefaultProgressbar.
		WithTotal(int(originalSize)).
		WithTitle("Encrypting & Uploading...").
		WithRemoveWhenDone(false).
		Start()

	progressReader := &ProgressReader{Reader: file, pb: p}

	pr, pw := io.Pipe()

	go func() {
		defer pw.Close()

		encWriter, err := crypto.EncryptWriter(recipientKey, pw)
		if err != nil {
			pw.CloseWithError(fmt.Errorf("failed to initialize age encryptor: %w", err))
			return
		}
		defer encWriter.Close()

		_, err = io.Copy(encWriter, progressReader)
		if err != nil {
			pw.CloseWithError(fmt.Errorf("failed to stream data: %w", err))
		}
	}()

	overallHasher := sha256.New()
	tee := io.TeeReader(pr, overallHasher)

	chunksMeta, err := core.StreamChunkerWithSize(tee, chunkSize, func(chunkReader io.Reader, index int) error {
		chunkName := fmt.Sprintf("chunk_%05d", index)
		remotePath := fmt.Sprintf("%s/%s", backupID, chunkName)

		return backend.Upload(remotePath, chunkReader)
	}, func(chunk core.ChunkMeta) error {
		if stateDir == "" {
			return nil
		}
		state.UploadedChunks = append(state.UploadedChunks, chunk)
		return saveUploadState(stateDir, state)
	})

	if err != nil {
		p.Stop()
		return fmt.Errorf("chunking/upload process failed: %w", err)
	}

	p.Stop()
	payloadEncryptedHash := hex.EncodeToString(overallHasher.Sum(nil))

	pterm.Info.Println("Building and encrypting manifest...")
	manifest := core.CreateManifest(backupID, originalName, originalSize, recipientKey, payloadEncryptedHash, chunksMeta)

	encryptedManifestBytes, err := core.EncryptManifest(recipientKey, manifest)
	if err != nil {
		return fmt.Errorf("failed to encrypt manifest: %w", err)
	}

	err = backend.Upload(fmt.Sprintf("%s/manifest.age", backupID), bytes.NewReader(encryptedManifestBytes))
	if err != nil {
		return fmt.Errorf("failed to upload manifest.age: %w", err)
	}

	locator := core.CreateLocator(backupID)
	locatorBytes, _ := core.SerializeLocator(locator)
	err = backend.Upload(fmt.Sprintf("%s/locator.json", backupID), bytes.NewReader(locatorBytes))
	if err != nil {
		return fmt.Errorf("failed to upload locator.json: %w", err)
	}
	if stateDir != "" {
		if err := deleteUploadState(stateDir, backupID); err != nil {
			return fmt.Errorf("backup completed but failed to delete upload state: %w", err)
		}
	}

	pterm.Success.Println("Backup completed successfully!")
	pterm.DefaultBasicText.Printf("Save this UUID for restoration: %s\n", backupID)
	return nil
}

type ProgressReader struct {
	io.Reader
	pb *pterm.ProgressbarPrinter
}

func (pr *ProgressReader) Read(p []byte) (int, error) {
	n, err := pr.Reader.Read(p)
	if n > 0 {
		pr.pb.Add(n)
	}
	return n, err
}
