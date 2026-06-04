package flow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"

	"github.com/pterm/pterm"

	"gloak/internal/core"
	"gloak/internal/crypto"
	"gloak/internal/storage"
)

func RunVerify(backupID string, identityPath string, remote string) error {
	return runVerify(backupID, identityPath, storage.NewRcloneBackend(remote))
}

func runVerify(backupID string, identityPath string, backend storage.Backend) error {
	pterm.Info.Printf("Starting verification for UUID: %s\n", backupID)

	var manifestBuf bytes.Buffer
	err := backend.Download(fmt.Sprintf("%s/manifest.age", backupID), &manifestBuf)
	if err != nil {
		return fmt.Errorf("failed to download manifest.age: %w", err)
	}

	pterm.Info.Println("Decrypting manifest...")
	manifestReader, err := crypto.DecryptReader(identityPath, &manifestBuf)
	if err != nil {
		return fmt.Errorf("failed to decrypt manifest (invalid key or corrupted file): %w", err)
	}

	manifestBytes, err := io.ReadAll(manifestReader)
	if err != nil {
		return fmt.Errorf("failed to read manifest contents: %w", err)
	}

	var manifest core.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return fmt.Errorf("failed to parse manifest JSON: %w", err)
	}

	pterm.Success.Printf("Manifest valid! Verifying %d chunk...\n", len(manifest.Chunks))

	p, _ := pterm.DefaultProgressbar.
		WithTotal(len(manifest.Chunks)).
		WithTitle("Verifying Chunks...").
		WithRemoveWhenDone(false).
		Start()

	payloadHasher := sha256.New()

	for _, chunk := range manifest.Chunks {
		var encryptedChunk bytes.Buffer
		err := backend.Download(fmt.Sprintf("%s/%s", backupID, chunk.Name), &encryptedChunk)
		if err != nil {
			p.Stop()
			return fmt.Errorf("failed to download chunk %s: %w", chunk.Name, err)
		}

		encryptedBytes := encryptedChunk.Bytes()
		if int64(len(encryptedBytes)) != chunk.EncryptedSize {
			p.Stop()
			return fmt.Errorf("CHUNK CORRUPT: Size %s mismatch!", chunk.Name)
		}
		if hashBytes(encryptedBytes) != chunk.EncryptedSHA256 {
			p.Stop()
			return fmt.Errorf("CHUNK CORRUPT: Hash %s mismatch!", chunk.Name)
		}

		plainReader, err := crypto.DecryptReader(identityPath, bytes.NewReader(encryptedBytes))
		if err != nil {
			p.Stop()
			return fmt.Errorf("failed to decrypt chunk %s: %w", chunk.Name, err)
		}
		plainBytes, err := io.ReadAll(plainReader)
		if err != nil {
			p.Stop()
			return fmt.Errorf("failed to read decrypted chunk %s: %w", chunk.Name, err)
		}
		if int64(len(plainBytes)) != chunk.PlainSize {
			p.Stop()
			return fmt.Errorf("CHUNK CORRUPT: Plain size %s mismatch!", chunk.Name)
		}
		if hashBytes(plainBytes) != chunk.PlainSHA256 {
			p.Stop()
			return fmt.Errorf("CHUNK CORRUPT: Plain hash %s mismatch!", chunk.Name)
		}
		payloadHasher.Write(plainBytes)

		p.Add(1)
	}

	p.Stop()

	if hex.EncodeToString(payloadHasher.Sum(nil)) != manifest.PayloadSHA256 {
		return fmt.Errorf("FATAL: Overall payload hash differs from manifest! Data has been modified")
	}

	pterm.Success.Println("VERIFIED! All chunks are healthy and checksums match 100%.")
	return nil
}
