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

	chunksMeta, payloadHash, err := uploadPlaintextChunks(file, recipientKey, backend, backupID, &state, stateDir, chunkSize, nil, func(n int) {
		p.Add(n)
	})

	if err != nil {
		p.Stop()
		return fmt.Errorf("chunking/upload process failed: %w", err)
	}

	p.Stop()

	pterm.Info.Println("Building and encrypting manifest...")
	manifest := core.CreateManifest(backupID, originalName, originalSize, recipientKey, payloadHash, chunksMeta)

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

func uploadPlaintextChunks(file *os.File, recipientKey string, backend storage.Backend, backupID string, state *UploadState, stateDir string, chunkSize int64, onSkip func(core.ChunkMeta), onProgress func(int)) ([]core.ChunkMeta, string, error) {
	if chunkSize <= 0 {
		return nil, "", fmt.Errorf("chunk size must be positive")
	}

	existingChunks := make(map[int]core.ChunkMeta, len(state.UploadedChunks))
	for _, chunk := range state.UploadedChunks {
		existingChunks[chunk.Index] = chunk
	}

	payloadHasher := sha256.New()
	chunksMeta := make([]core.ChunkMeta, 0, len(state.UploadedChunks))
	buffer := make([]byte, int(chunkSize))

	for index := 0; ; index++ {
		n, readErr := io.ReadFull(file, buffer)
		if readErr == io.EOF {
			break
		}
		if readErr != nil && readErr != io.ErrUnexpectedEOF {
			return nil, "", fmt.Errorf("failed to read source chunk %d: %w", index, readErr)
		}

		plainChunk := append([]byte(nil), buffer[:n]...)
		payloadHasher.Write(plainChunk)
		plainHash := hashBytes(plainChunk)
		chunkName := fmt.Sprintf("chunk_%05d", index)

		if existing, ok := existingChunks[index]; ok {
			if existing.Name != chunkName {
				return nil, "", fmt.Errorf("upload state chunk %d name mismatch: got %s, want %s", index, existing.Name, chunkName)
			}
			if existing.PlainSize != int64(n) || existing.PlainSHA256 != plainHash {
				return nil, "", fmt.Errorf("upload state chunk %d does not match source file", index)
			}
			chunksMeta = append(chunksMeta, existing)
			if onSkip != nil {
				onSkip(existing)
			}
		} else {
			encryptedChunk, err := encryptChunk(recipientKey, plainChunk)
			if err != nil {
				return nil, "", fmt.Errorf("failed to encrypt chunk %d: %w", index, err)
			}

			chunk := core.ChunkMeta{
				Index:           index,
				Name:            chunkName,
				PlainSize:       int64(n),
				PlainSHA256:     plainHash,
				EncryptedSize:   int64(len(encryptedChunk)),
				EncryptedSHA256: hashBytes(encryptedChunk),
			}

			if err := backend.Upload(fmt.Sprintf("%s/%s", backupID, chunkName), bytes.NewReader(encryptedChunk)); err != nil {
				return nil, "", fmt.Errorf("chunk %d failed to process: %w", index, err)
			}
			chunksMeta = append(chunksMeta, chunk)
			if stateDir != "" {
				state.UploadedChunks = append(state.UploadedChunks, chunk)
				if err := saveUploadState(stateDir, *state); err != nil {
					return nil, "", fmt.Errorf("chunk %d callback failed: %w", index, err)
				}
			}
		}

		if onProgress != nil {
			onProgress(n)
		}
		if readErr == io.ErrUnexpectedEOF {
			break
		}
	}

	return chunksMeta, hex.EncodeToString(payloadHasher.Sum(nil)), nil
}

func encryptChunk(recipientKey string, plainChunk []byte) ([]byte, error) {
	var encrypted bytes.Buffer
	encWriter, err := crypto.EncryptWriter(recipientKey, &encrypted)
	if err != nil {
		return nil, err
	}
	if _, err := encWriter.Write(plainChunk); err != nil {
		_ = encWriter.Close()
		return nil, err
	}
	if err := encWriter.Close(); err != nil {
		return nil, err
	}
	return encrypted.Bytes(), nil
}

func hashBytes(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
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
