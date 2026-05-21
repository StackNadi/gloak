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

	"securebackup/internal/core"
	"securebackup/internal/crypto"
	"securebackup/internal/storage"
)

func RunUpload(filePath string, recipientKey string, remote string) error {
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

	backend := storage.NewRcloneBackend(remote)
	backupID := core.GenerateUUID()
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

	chunksMeta, err := core.StreamChunker(tee, func(chunkReader io.Reader, index int) error {
		chunkName := fmt.Sprintf("chunk_%05d", index)
		remotePath := fmt.Sprintf("%s/%s", backupID, chunkName)

		return backend.Upload(remotePath, chunkReader)
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
