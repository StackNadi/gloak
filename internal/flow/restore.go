package flow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pterm/pterm"

	"gloak/internal/crypto"
	"gloak/internal/repo"
	"gloak/internal/storage"
)

func RunRestore(snapshotID, identityPath, remote, outputDir string) error {
	backend := storage.NewRcloneBackend(remote)
	outputPath, err := runRestore(snapshotID, outputDir, identityPath, backend)
	if err != nil {
		return err
	}
	pterm.Success.Println("Restore completed successfully! File saved to:", outputPath)
	return nil
}

func runRestore(snapshotID, outputDir, identityPath string, backend storage.Backend) (string, error) {
	if isFileMode(backend, snapshotID) {
		return runFileRestore(snapshotID, identityPath, outputDir, backend)
	}
	r, err := repo.Open(backend, identityPath)
	if err != nil {
		return "", err
	}
	manifest, err := readManifest(backend, identityPath, snapshotID)
	if err != nil {
		return "", err
	}

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
