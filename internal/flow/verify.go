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
	pterm.Info.Printf("Starting verification for UUID: %s\n", backupID)

	backend := storage.NewRcloneBackend(remote)

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
		chunkHasher := sha256.New()

		multiWriter := io.MultiWriter(chunkHasher, payloadHasher)

		err := backend.Download(fmt.Sprintf("%s/%s", backupID, chunk.Name), multiWriter)
		if err != nil {
			p.Stop()
			return fmt.Errorf("failed to download chunk %s: %w", chunk.Name, err)
		}

		if hex.EncodeToString(chunkHasher.Sum(nil)) != chunk.SHA256 {
			p.Stop()
			return fmt.Errorf("CHUNK CORRUPT: Hash %s mismatch!", chunk.Name)
		}

		p.Add(1)
	}

	p.Stop()

	if hex.EncodeToString(payloadHasher.Sum(nil)) != manifest.PayloadEncryptedHash {
		return fmt.Errorf("FATAL: Overall payload hash differs from manifest! Data has been modified")
	}

	pterm.Success.Println("VERIFIED! All chunks are healthy and checksums match 100%.")
	return nil
}
