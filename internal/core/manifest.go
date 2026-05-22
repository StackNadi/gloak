package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"gloak/internal/crypto"
)

type Manifest struct {
	Version              string      `json:"version"`
	UUID                 string      `json:"uuid"`
	OriginalName         string      `json:"original_name"`
	OriginalSize         int64       `json:"original_size"`
	CreatedAt            string      `json:"created_at"`
	Recipient            string      `json:"recipient"`
	PayloadEncryptedHash string      `json:"payload_encrypted_hash"`
	Chunks               []ChunkMeta `json:"chunks"`
}

type Locator struct {
	Version string `json:"version"`
	UUID    string `json:"uuid"`
}

func GenerateUUID() string {
	return uuid.New().String()
}

func CreateManifest(id string, filename string, size int64, recipient string, payloadHash string, chunks []ChunkMeta) Manifest {
	return Manifest{
		Version:              "1.0",
		UUID:                 id,
		OriginalName:         filename,
		OriginalSize:         size,
		CreatedAt:            time.Now().UTC().Format(time.RFC3339),
		Recipient:            recipient,
		PayloadEncryptedHash: payloadHash,
		Chunks:               chunks,
	}
}

func CreateLocator(id string) Locator {
	return Locator{
		Version: "1.0",
		UUID:    id,
	}
}

func EncryptManifest(pubKey string, manifest Manifest) ([]byte, error) {
	jsonData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal manifest json: %w", err)
	}

	var buf bytes.Buffer

	encWriter, err := crypto.EncryptWriter(pubKey, &buf)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare age writer: %w", err)
	}

	_, err = encWriter.Write(jsonData)
	if err != nil {
		return nil, fmt.Errorf("failed to write JSON to encryptor: %w", err)
	}
	encWriter.Close()

	return buf.Bytes(), nil
}

func SerializeLocator(locator Locator) ([]byte, error) {
	return json.MarshalIndent(locator, "", "  ")
}
