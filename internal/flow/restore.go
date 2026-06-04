package flow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/pterm/pterm"

	"gloak/internal/core"
	"gloak/internal/crypto"
	"gloak/internal/storage"
)

func RunRestore(backupID string, identityPath string, remote string, outputDir string) error {
	return runRestore(backupID, identityPath, storage.NewRcloneBackend(remote), outputDir)
}

func runRestore(backupID string, identityPath string, backend storage.Backend, outputDir string) error {
	pterm.Info.Printf("Starting restore for UUID: %s\n", backupID)

	pterm.Info.Println("Downloading encrypted manifest...")
	var manifestBuf bytes.Buffer
	err := backend.Download(fmt.Sprintf("%s/manifest.age", backupID), &manifestBuf)
	if err != nil {
		return fmt.Errorf("failed to download manifest.age: %w", err)
	}

	pterm.Info.Println("Decrypting manifest...")
	manifestReader, err := crypto.DecryptReader(identityPath, &manifestBuf)
	if err != nil {
		return fmt.Errorf("failed to decrypt manifest: %w", err)
	}

	manifestBytes, err := io.ReadAll(manifestReader)
	if err != nil {
		return fmt.Errorf("failed to read manifest contents: %w", err)
	}

	var manifest core.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return fmt.Errorf("failed to parse manifest JSON: %w", err)
	}

	pterm.Success.Printf("Manifest valid! Restoring file: %s (%d bytes)\n", manifest.OriginalName, manifest.OriginalSize)

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}
	if filepath.Base(manifest.OriginalName) != manifest.OriginalName {
		return fmt.Errorf("invalid original filename in manifest: %s", manifest.OriginalName)
	}
	outputPath := filepath.Join(outputDir, manifest.OriginalName)
	outFile, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	p, _ := pterm.DefaultProgressbar.
		WithTotal(int(manifest.OriginalSize)).
		WithTitle("Downloading & Decrypting...").
		WithRemoveWhenDone(false).
		Start()

	payloadHasher := sha256.New()
	for _, chunk := range manifest.Chunks {
		var encryptedChunk bytes.Buffer
		if err := backend.Download(fmt.Sprintf("%s/%s", backupID, chunk.Name), &encryptedChunk); err != nil {
			p.Stop()
			return fmt.Errorf("failed to download chunk %s: %w", chunk.Name, err)
		}

		encryptedBytes := encryptedChunk.Bytes()
		if int64(len(encryptedBytes)) != chunk.EncryptedSize {
			p.Stop()
			return fmt.Errorf("chunk %s corrupted/tampered! Size mismatch", chunk.Name)
		}
		if hashBytes(encryptedBytes) != chunk.EncryptedSHA256 {
			p.Stop()
			return fmt.Errorf("chunk %s corrupted/tampered! Hash mismatch", chunk.Name)
		}

		decReader, err := crypto.DecryptReader(identityPath, bytes.NewReader(encryptedBytes))
		if err != nil {
			p.Stop()
			return fmt.Errorf("failed to decrypt chunk %s: %w", chunk.Name, err)
		}
		plainBytes, err := io.ReadAll(decReader)
		if err != nil {
			p.Stop()
			return fmt.Errorf("failed to read decrypted chunk %s: %w", chunk.Name, err)
		}
		if int64(len(plainBytes)) != chunk.PlainSize {
			p.Stop()
			return fmt.Errorf("chunk %s corrupted/tampered! Plain size mismatch", chunk.Name)
		}
		if hashBytes(plainBytes) != chunk.PlainSHA256 {
			p.Stop()
			return fmt.Errorf("chunk %s corrupted/tampered! Plain hash mismatch", chunk.Name)
		}
		if _, err := outFile.Write(plainBytes); err != nil {
			p.Stop()
			return fmt.Errorf("failed to write restored chunk %s: %w", chunk.Name, err)
		}
		payloadHasher.Write(plainBytes)
		p.Add(len(plainBytes))
	}

	if hex.EncodeToString(payloadHasher.Sum(nil)) != manifest.PayloadSHA256 {
		p.Stop()
		return fmt.Errorf("fatal: overall payload hash does not match manifest")
	}

	p.Stop()
	pterm.Success.Println("Restore completed successfully! File saved to:", outputPath)
	return nil
}

type RestoreProgressReader struct {
	io.Reader
	pb *pterm.ProgressbarPrinter
}

func (pr *RestoreProgressReader) Read(p []byte) (int, error) {
	n, err := pr.Reader.Read(p)
	if n > 0 {
		pr.pb.Add(n)
	}
	return n, err
}
