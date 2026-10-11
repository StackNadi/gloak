package flow

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/pterm/pterm"

	"gloak/internal/core"
	"gloak/internal/crypto"
	"gloak/internal/storage"
)

const fileFormatVersion = "file-2"

const masterKeySize = 32

var fileChunkNameRe = regexp.MustCompile(`^chunk_\d{5}$`)

type fileManifest struct {
	Version       string           `json:"version"`
	UUID          string           `json:"uuid"`
	OriginalName  string           `json:"original_name"`
	OriginalSize  int64            `json:"original_size"`
	CreatedAt     string           `json:"created_at"`
	PayloadSHA256 string           `json:"payload_sha256"`
	MasterKey     string           `json:"master_key"`
	Chunks        []core.ChunkMeta `json:"chunks"`
}

type fileLocator struct {
	Version string `json:"version"`
	UUID    string `json:"uuid"`
}

func fileChunkName(index int) string {
	return fmt.Sprintf("chunk_%05d", index)
}

// validateFileChunkName accepts only relative chunk_NNNNN names.
func validateFileChunkName(name string) error {
	if !fileChunkNameRe.MatchString(name) {
		return fmt.Errorf("invalid chunk name: %s", name)
	}
	return nil
}

// RunUpload seals filePath into a self-contained {uuid}/ folder on backend.
func RunUpload(filePath string, recipients []string, backend storage.Backend) (string, error) {
	if len(recipients) == 0 {
		return "", fmt.Errorf("at least one recipient is required")
	}
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open source file: %w", err)
	}
	defer file.Close()

	backupID := core.GenerateUUID()
	masterKey := make([]byte, masterKeySize)
	if _, err := rand.Read(masterKey); err != nil {
		return "", fmt.Errorf("failed to generate master key: %w", err)
	}

	spinner, _ := pterm.DefaultSpinner.Start("Encrypting and uploading...")
	payloadHasher := sha256.New()
	buffer := make([]byte, int(core.ChunkSize))
	var metas []core.ChunkMeta
	var total int64

	for index := 0; ; index++ {
		n, readErr := io.ReadFull(file, buffer)
		if readErr == io.EOF {
			break
		}
		if readErr != nil && readErr != io.ErrUnexpectedEOF {
			_ = spinner.Stop()
			return "", fmt.Errorf("failed to read chunk %d: %w", index, readErr)
		}
		if n == 0 {
			break
		}

		plain := buffer[:n]
		payloadHasher.Write(plain)
		plainHash := hashBytes(plain)
		sealed, err := crypto.SealChunk(masterKey, plainHash, plain)
		if err != nil {
			_ = spinner.Stop()
			return "", fmt.Errorf("failed to seal chunk %d: %w", index, err)
		}
		encSum := sha256.Sum256(sealed)
		name := fileChunkName(index)
		remotePath := backupID + "/" + name
		if err := backend.Upload(remotePath, bytes.NewReader(sealed)); err != nil {
			_ = spinner.Stop()
			return "", fmt.Errorf("failed to upload chunk %d: %w", index, err)
		}
		metas = append(metas, core.ChunkMeta{
			Index:           index,
			Name:            name,
			PlainSize:       int64(n),
			PlainSHA256:     plainHash,
			EncryptedSize:   int64(len(sealed)),
			EncryptedSHA256: hex.EncodeToString(encSum[:]),
		})
		total += int64(n)
		spinner.UpdateText(fmt.Sprintf("Uploaded %d chunks", len(metas)))

		if readErr == io.ErrUnexpectedEOF {
			break
		}
	}

	manifest := fileManifest{
		Version:       fileFormatVersion,
		UUID:          backupID,
		OriginalName:  filepath.Base(filePath),
		OriginalSize:  total,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		PayloadSHA256: hex.EncodeToString(payloadHasher.Sum(nil)),
		MasterKey:     hex.EncodeToString(masterKey),
		Chunks:        metas,
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		_ = spinner.Stop()
		return "", fmt.Errorf("failed to marshal manifest: %w", err)
	}
	sealedManifest, err := crypto.EncryptMulti(recipients, manifestJSON)
	if err != nil {
		_ = spinner.Stop()
		return "", fmt.Errorf("failed to encrypt manifest: %w", err)
	}
	if err := backend.Upload(backupID+"/manifest.age", bytes.NewReader(sealedManifest)); err != nil {
		_ = spinner.Stop()
		return "", fmt.Errorf("failed to upload manifest: %w", err)
	}

	locator := fileLocator{Version: fileFormatVersion, UUID: backupID}
	locatorJSON, err := json.Marshal(locator)
	if err != nil {
		_ = spinner.Stop()
		return "", fmt.Errorf("failed to marshal locator: %w", err)
	}
	if err := backend.Upload(backupID+"/locator.json", bytes.NewReader(locatorJSON)); err != nil {
		_ = spinner.Stop()
		return "", fmt.Errorf("failed to upload locator: %w", err)
	}

	_ = spinner.Stop()
	pterm.Success.Printf("Upload complete: %s (%d chunks, %d bytes)\n", backupID, len(metas), total)
	return backupID, nil
}
