package flow

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gloak/internal/crypto"
	"gloak/internal/repo"
	"gloak/internal/storage/memtest"
)

func repoFixture(t *testing.T, chunkSize int64) (string, *memtest.MemoryBackend, string) {
	t.Helper()
	pub, _, fileContent, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error: %v", err)
	}
	identPath := filepath.Join(t.TempDir(), "identity.txt")
	if err := os.WriteFile(identPath, []byte(fileContent), 0600); err != nil {
		t.Fatalf("write identity: %v", err)
	}
	backend := memtest.NewMemoryBackend()
	if _, _, err := repo.Init(backend, []string{pub}, chunkSize, false); err != nil {
		t.Fatalf("repo.Init() error: %v", err)
	}
	return identPath, backend, pub
}

func writeTempFile(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "payload.bin")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func TestBackupWritesSnapshotAndDataObjects(t *testing.T) {
	ident, backend, _ := repoFixture(t, 8)
	payload := []byte("0123456789abcdefghij") // 3 chunks with size 8
	filePath := writeTempFile(t, payload)

	res, err := runBackup(filePath, ident, backend, "7f91c6c7-7a0b-44aa-ae23-997b60e4e998", nil)
	if err != nil {
		t.Fatalf("runBackup() error: %v", err)
	}
	if res.TotalChunks != 3 || res.UploadedChunks != 3 || res.SkippedChunks != 0 {
		t.Fatalf("result = %+v", res)
	}
	if !backend.Has(repo.SnapshotsPrefix + "/" + res.SnapshotID + ".age") {
		t.Fatal("snapshot file missing")
	}
	if got := backend.TotalUploads(repo.DataPrefix + "/"); got != 3 {
		t.Fatalf("data uploads = %d, want 3", got)
	}
	if !backend.Has("index/000001.age") {
		t.Fatal("index epoch missing")
	}
}

func TestBackupSkipsChunksAlreadyInIndex(t *testing.T) {
	ident, backend, _ := repoFixture(t, 8)
	payload := []byte("0123456789abcdefghij")
	filePath := writeTempFile(t, payload)

	id1 := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	if _, err := runBackup(filePath, ident, backend, id1, nil); err != nil {
		t.Fatalf("first runBackup() error: %v", err)
	}
	dataUploadsBefore := backend.TotalUploads(repo.DataPrefix + "/")

	id2 := "aaaaaaaa-bbbb-4ccc-8ddd-ffffffffffff"
	res, err := runBackup(filePath, ident, backend, id2, nil)
	if err != nil {
		t.Fatalf("second runBackup() error: %v", err)
	}
	if res.UploadedChunks != 0 || res.SkippedChunks != 3 {
		t.Fatalf("second result = %+v, want all skipped", res)
	}
	if backend.TotalUploads(repo.DataPrefix+"/") != dataUploadsBefore {
		t.Fatal("second backup re-uploaded data objects")
	}
	if !backend.Has(repo.SnapshotsPrefix + "/" + id2 + ".age") {
		t.Fatal("second snapshot missing")
	}
}

func TestBackupDedupsRepeatedChunksInSameFile(t *testing.T) {
	ident, backend, _ := repoFixture(t, 8)
	payload := []byte("012345670123456701234567") // same 8-byte chunk three times
	filePath := writeTempFile(t, payload)

	res, err := runBackup(filePath, ident, backend, "11111111-2222-4333-8444-555555555555", nil)
	if err != nil {
		t.Fatalf("runBackup() error: %v", err)
	}
	if res.TotalChunks != 3 {
		t.Fatalf("TotalChunks = %d", res.TotalChunks)
	}
	if res.UploadedChunks != 1 {
		t.Fatalf("UploadedChunks = %d, want 1 (content-addressed dedup)", res.UploadedChunks)
	}
	if got := backend.TotalUploads(repo.DataPrefix + "/"); got != 1 {
		t.Fatalf("data uploads = %d, want 1", got)
	}
}

func TestBackupFailsWithoutRepository(t *testing.T) {
	ident, _, _ := repoFixture(t, 8)
	filePath := writeTempFile(t, []byte("x"))
	_, err := runBackup(filePath, ident, memtest.NewMemoryBackend(), "7f91c6c7-7a0b-44aa-ae23-997b60e4e998", nil)
	if err == nil {
		t.Fatal("runBackup() succeeded without repository")
	}
	if !strings.Contains(err.Error(), "not a gloak repository") {
		t.Fatalf("error = %v, want not a gloak repository", err)
	}
}

func TestBackupRejectsInvalidSnapshotID(t *testing.T) {
	ident, backend, _ := repoFixture(t, 8)
	filePath := writeTempFile(t, []byte("x"))
	_, err := runBackup(filePath, ident, backend, "../escape", nil)
	if err == nil {
		t.Fatal("runBackup() succeeded with invalid snapshot id")
	}
	if !strings.Contains(err.Error(), "invalid snapshot id") {
		t.Fatalf("error = %v", err)
	}
}

func TestBackupObjectMatchesChunkHash(t *testing.T) {
	ident, backend, _ := repoFixture(t, 8)
	payload := []byte("0123456789abcdefghij")
	filePath := writeTempFile(t, payload)
	if _, err := runBackup(filePath, ident, backend, "99999999-8888-4777-8666-555555555555", nil); err != nil {
		t.Fatalf("runBackup() error: %v", err)
	}
	sum := sha256.Sum256(payload[:8])
	wantPath := repo.DataPath(hex.EncodeToString(sum[:]))
	if !backend.Has(wantPath) {
		t.Fatalf("expected object at %s", wantPath)
	}
}
