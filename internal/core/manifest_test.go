package core

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	appcrypto "gloak/internal/crypto"
)

func TestCreateManifestAndLocatorVersions(t *testing.T) {
	chunks := []ChunkMeta{{Index: 0, Name: "chunk_00000", Size: 10, SHA256: "abc"}}
	manifest := CreateManifest("backup-id", "secret.tar", 123, "age1example", "payload-hash", chunks)

	if manifest.Version != "1.0" {
		t.Fatalf("manifest version = %q, want 1.0", manifest.Version)
	}
	if manifest.UUID != "backup-id" || manifest.OriginalName != "secret.tar" || manifest.OriginalSize != 123 {
		t.Fatalf("manifest fields not preserved: %+v", manifest)
	}
	if len(manifest.Chunks) != 1 || manifest.Chunks[0] != chunks[0] {
		t.Fatalf("manifest chunks = %+v, want %+v", manifest.Chunks, chunks)
	}

	locator := CreateLocator("backup-id")
	if locator.Version != "1.0" || locator.UUID != "backup-id" {
		t.Fatalf("locator = %+v, want version 1.0 and uuid backup-id", locator)
	}
}

func TestSerializeLocator(t *testing.T) {
	data, err := SerializeLocator(CreateLocator("backup-id"))
	if err != nil {
		t.Fatalf("SerializeLocator() returned error: %v", err)
	}

	var locator Locator
	if err := json.Unmarshal(data, &locator); err != nil {
		t.Fatalf("serialized locator is not valid JSON: %v", err)
	}
	if locator.Version != "1.0" || locator.UUID != "backup-id" {
		t.Fatalf("locator = %+v, want version 1.0 and uuid backup-id", locator)
	}
}

func TestEncryptManifestCanBeDecrypted(t *testing.T) {
	pubKey, _, identityFile, err := appcrypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() returned error: %v", err)
	}
	identityPath := filepath.Join(t.TempDir(), "identity.txt")
	if err := os.WriteFile(identityPath, []byte(identityFile), 0600); err != nil {
		t.Fatalf("failed to write identity file: %v", err)
	}

	manifest := CreateManifest("backup-id", "secret.tar", 123, pubKey, "payload-hash", nil)
	encrypted, err := EncryptManifest(pubKey, manifest)
	if err != nil {
		t.Fatalf("EncryptManifest() returned error: %v", err)
	}
	if bytes.Contains(encrypted, []byte("secret.tar")) {
		t.Fatalf("encrypted manifest contains plaintext filename")
	}

	r, err := appcrypto.DecryptReader(identityPath, bytes.NewReader(encrypted))
	if err != nil {
		t.Fatalf("DecryptReader() returned error: %v", err)
	}
	plain, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("failed to read decrypted manifest: %v", err)
	}

	var got Manifest
	if err := json.Unmarshal(plain, &got); err != nil {
		t.Fatalf("decrypted manifest is not valid JSON: %v", err)
	}
	if got.UUID != manifest.UUID || got.OriginalName != manifest.OriginalName || got.PayloadEncryptedHash != manifest.PayloadEncryptedHash {
		t.Fatalf("decrypted manifest = %+v, want %+v", got, manifest)
	}
}
