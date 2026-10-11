package flow

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"gloak/internal/core"
	appcrypto "gloak/internal/crypto"
	"gloak/internal/repo"
)

func TestBackupVerifyRestoreRoundTrip(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	payload := []byte("0123456789abcdefghij")
	filePath := writeTempFile(t, payload)
	snapshotID := "7f91c6c7-7a0b-44aa-ae23-997b60e4e998"

	if _, err := runBackup(filePath, ident, backend, snapshotID, nil); err != nil {
		t.Fatalf("runBackup() error: %v", err)
	}
	if err := runVerify(snapshotID, ident, backend); err != nil {
		t.Fatalf("runVerify() error: %v", err)
	}

	outDir := t.TempDir()
	restoredPath, err := runRestore(snapshotID, outDir, ident, backend, "")
	if err != nil {
		t.Fatalf("runRestore() error: %v", err)
	}
	got, err := os.ReadFile(restoredPath)
	if err != nil {
		t.Fatalf("read restored: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("restored = %q, want %q", got, payload)
	}
}

func TestVerifyRejectsCorruptChunk(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	payload := []byte("0123456789abcdefghij")
	filePath := writeTempFile(t, payload)
	snapshotID := "7f91c6c7-7a0b-44aa-ae23-997b60e4e998"
	if _, err := runBackup(filePath, ident, backend, snapshotID, nil); err != nil {
		t.Fatalf("runBackup() error: %v", err)
	}

	// Corrupt the first data object in place.
	var dataKey string
	for _, key := range backend.KeysWithPrefix(repo.DataPrefix + "/") {
		dataKey = key
		break
	}
	if dataKey == "" {
		t.Fatal("no data objects to corrupt")
	}
	blob := backend.Get(dataKey)
	blob[len(blob)-1] ^= 0xff
	backend.Put(dataKey, blob)

	err := runVerify(snapshotID, ident, backend)
	if err == nil {
		t.Fatal("runVerify() succeeded on corrupt chunk")
	}
	if !strings.Contains(err.Error(), "CHUNK CORRUPT") {
		t.Fatalf("error = %v, want CHUNK CORRUPT", err)
	}
}

func TestRestoreRejectsEscapingChunkPath(t *testing.T) {
	ident, backend, pub := repoFixture(t)
	snapshotID := "7f91c6c7-7a0b-44aa-ae23-997b60e4e998"

	manifest := core.Manifest{
		Version:       repo.FormatVersion,
		UUID:          snapshotID,
		OriginalName:  "payload.bin",
		OriginalSize:  4,
		CreatedAt:     "2026-01-01T00:00:00Z",
		Recipient:     pub,
		PayloadSHA256: hashBytes([]byte("data")),
		Chunks: []core.ChunkMeta{{
			Index:       0,
			Name:        "../evil",
			PlainSize:   4,
			PlainSHA256: hashBytes([]byte("data")),
		}},
	}
	plain, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	sealed, err := appcrypto.EncryptMulti([]string{pub}, plain)
	if err != nil {
		t.Fatalf("EncryptMulti() error: %v", err)
	}
	snapshotPath, err := repo.SnapshotPath(snapshotID)
	if err != nil {
		t.Fatalf("SnapshotPath() error: %v", err)
	}
	backend.Put(snapshotPath, sealed)

	if _, err := runRestore(snapshotID, t.TempDir(), ident, backend, ""); err == nil {
		t.Fatal("runRestore() succeeded with escaping chunk path")
	} else if !strings.Contains(err.Error(), "escapes data prefix") {
		t.Fatalf("error = %v, want escapes data prefix", err)
	}
}
