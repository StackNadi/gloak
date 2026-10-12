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

	"gloak/internal/crypto"
	"gloak/internal/storage"
)

// isFileMode reports whether backupID/locator.json can be downloaded.
// It does not parse the locator, and any download error yields false.
func isFileMode(backend storage.Backend, backupID string) bool {
	var buf bytes.Buffer
	return backend.Download(backupID+"/locator.json", &buf) == nil
}

// readFileManifest loads a standalone backup's locator and encrypted manifest.
// It checks both UUIDs, the manifest version, and that OriginalName equals its
// filepath.Base. Locator download failures become an incomplete-or-missing
// backup error; JSON, manifest download, identity, and decryption errors are returned.
func readFileManifest(backend storage.Backend, identityPath, backupID string) (fileManifest, error) {
	var loc fileLocator
	var locBuf bytes.Buffer
	if err := backend.Download(backupID+"/locator.json", &locBuf); err != nil {
		return fileManifest{}, fmt.Errorf("backup %s incomplete or missing: no locator.json", backupID)
	}
	if err := json.Unmarshal(locBuf.Bytes(), &loc); err != nil {
		return fileManifest{}, fmt.Errorf("invalid locator: %w", err)
	}
	if loc.UUID != backupID {
		return fileManifest{}, fmt.Errorf("locator uuid mismatch: got %s want %s", loc.UUID, backupID)
	}

	var manBuf bytes.Buffer
	if err := backend.Download(backupID+"/manifest.age", &manBuf); err != nil {
		return fileManifest{}, fmt.Errorf("failed to download manifest: %w", err)
	}
	reader, err := crypto.DecryptReader(identityPath, &manBuf)
	if err != nil {
		return fileManifest{}, fmt.Errorf("failed to decrypt manifest: %w", err)
	}
	plain, err := io.ReadAll(reader)
	if err != nil {
		return fileManifest{}, fmt.Errorf("failed to read manifest: %w", err)
	}
	var m fileManifest
	if err := json.Unmarshal(plain, &m); err != nil {
		return fileManifest{}, fmt.Errorf("failed to parse manifest: %w", err)
	}
	if m.UUID != backupID {
		return fileManifest{}, fmt.Errorf("manifest uuid mismatch: got %s want %s", m.UUID, backupID)
	}
	if m.Version != fileFormatVersion {
		return fileManifest{}, fmt.Errorf("unsupported manifest version: %s", m.Version)
	}
	if filepath.Base(m.OriginalName) != m.OriginalName {
		return fileManifest{}, fmt.Errorf("invalid original filename in manifest: %s", m.OriginalName)
	}
	return m, nil
}

// decodeFileMasterKey decodes the manifest's hex key, returning an error if it
// is malformed or does not contain exactly 32 bytes.
func decodeFileMasterKey(m fileManifest) ([]byte, error) {
	key, err := hex.DecodeString(m.MasterKey)
	if err != nil || len(key) != masterKeySize {
		return nil, fmt.Errorf("invalid master key in manifest")
	}
	return key, nil
}

// runFileRestore restores a standalone backup under outputDir and returns the
// output path. It creates parent directories and truncates an existing output
// file. Manifest, key, chunk path, download, decryption, checksum, and filesystem
// errors are returned; a failure can leave a partial file. File close errors
// are ignored.
func runFileRestore(backupID, identityPath, outputDir string, backend storage.Backend) (string, error) {
	m, err := readFileManifest(backend, identityPath, backupID)
	if err != nil {
		return "", err
	}
	masterKey, err := decodeFileMasterKey(m)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}
	outputPath := filepath.Join(outputDir, m.OriginalName)
	outFile, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return "", fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	payloadHasher := sha256.New()
	for _, meta := range m.Chunks {
		if err := validateFileChunkName(meta.Name); err != nil {
			return "", fmt.Errorf("invalid chunk name in manifest: %w", err)
		}
		var buf bytes.Buffer
		if err := backend.Download(backupID+"/"+meta.Name, &buf); err != nil {
			return "", fmt.Errorf("CHUNK CORRUPT %d (%s): missing object: %w", meta.Index, meta.Name, err)
		}
		plain, err := crypto.OpenChunk(masterKey, meta.PlainSHA256, buf.Bytes())
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

	if hex.EncodeToString(payloadHasher.Sum(nil)) != m.PayloadSHA256 {
		return "", fmt.Errorf("fatal: overall payload hash does not match manifest")
	}
	return outputPath, nil
}

// runFileVerify reads a standalone backup without writing restored files.
// It verifies encrypted and plaintext chunk sizes and hashes, authentication,
// and the overall payload hash, returning manifest, key, path, download,
// decryption, or integrity errors.
func runFileVerify(backupID, identityPath string, backend storage.Backend) error {
	m, err := readFileManifest(backend, identityPath, backupID)
	if err != nil {
		return err
	}
	masterKey, err := decodeFileMasterKey(m)
	if err != nil {
		return err
	}

	payloadHasher := sha256.New()
	for _, meta := range m.Chunks {
		if err := validateFileChunkName(meta.Name); err != nil {
			return fmt.Errorf("invalid chunk name in manifest: %w", err)
		}
		var buf bytes.Buffer
		if err := backend.Download(backupID+"/"+meta.Name, &buf); err != nil {
			return fmt.Errorf("CHUNK CORRUPT %d (%s): missing object: %w", meta.Index, meta.Name, err)
		}
		sealed := buf.Bytes()
		if int64(len(sealed)) != meta.EncryptedSize {
			return fmt.Errorf("CHUNK CORRUPT %d (%s): encrypted size mismatch", meta.Index, meta.Name)
		}
		if hashBytes(sealed) != meta.EncryptedSHA256 {
			return fmt.Errorf("CHUNK CORRUPT %d (%s): encrypted sha256 mismatch", meta.Index, meta.Name)
		}
		plain, err := crypto.OpenChunk(masterKey, meta.PlainSHA256, sealed)
		if err != nil {
			return fmt.Errorf("CHUNK CORRUPT %d (%s): %w", meta.Index, meta.Name, err)
		}
		if int64(len(plain)) != meta.PlainSize || hashBytes(plain) != meta.PlainSHA256 {
			return fmt.Errorf("CHUNK CORRUPT %d (%s): plaintext mismatch", meta.Index, meta.Name)
		}
		payloadHasher.Write(plain)
	}
	if hex.EncodeToString(payloadHasher.Sum(nil)) != m.PayloadSHA256 {
		return fmt.Errorf("FATAL: overall payload hash differs from manifest")
	}
	return nil
}
