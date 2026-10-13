package flow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gloak/internal/crypto"
	"gloak/internal/repo"
	"gloak/internal/storage/memtest"
)

func repoFixture(t *testing.T) (string, *memtest.MemoryBackend, string) {
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
	// min=1 avg=8 max=64: tiny test payloads stay one chunk unless tests
	// craft multi-chunk files themselves.
	if _, _, err := repo.Init(backend, []string{pub}, 1, 8, 64, false); err != nil {
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
	ident, backend, _ := repoFixture(t)
	payload := []byte("0123456789abcdefghij")
	filePath := writeTempFile(t, payload)

	res, err := runBackup(filePath, ident, backend, "7f91c6c7-7a0b-44aa-ae23-997b60e4e998", nil)
	if err != nil {
		t.Fatalf("runBackup() error: %v", err)
	}
	if res.TotalChunks < 1 || res.UploadedChunks != res.TotalChunks || res.SkippedChunks != 0 {
		t.Fatalf("result = %+v", res)
	}
	if res.TotalPlainBytes != int64(len(payload)) {
		t.Fatalf("TotalPlainBytes = %d", res.TotalPlainBytes)
	}
	if !backend.Has(repo.SnapshotsPrefix + "/" + res.SnapshotID + ".age") {
		t.Fatal("snapshot file missing")
	}
	if got := backend.TotalUploads(repo.DataPrefix + "/"); got != res.UploadedChunks {
		t.Fatalf("data uploads = %d, want %d", got, res.UploadedChunks)
	}
	if !backend.Has("index/000001.age") {
		t.Fatal("index epoch missing")
	}
}

func TestBackupSkipsChunksAlreadyInIndex(t *testing.T) {
	ident, backend, _ := repoFixture(t)
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
	if res.UploadedChunks != 0 || res.SkippedChunks != res.TotalChunks {
		t.Fatalf("second result = %+v, want all skipped", res)
	}
	if backend.TotalUploads(repo.DataPrefix+"/") != dataUploadsBefore {
		t.Fatal("second backup re-uploaded data objects")
	}
	if !backend.Has(repo.SnapshotsPrefix + "/" + id2 + ".age") {
		t.Fatal("second snapshot missing")
	}
}

func TestBackupDedupsIdenticalContentAcrossFiles(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	payload := []byte("0123456789abcdefghij")
	fileA := writeTempFile(t, payload)
	fileB := filepath.Join(t.TempDir(), "copy.bin")
	if err := os.WriteFile(fileB, payload, 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := runBackup(fileA, ident, backend, "11111111-2222-4333-8444-555555555555", nil); err != nil {
		t.Fatalf("first runBackup() error: %v", err)
	}
	before := backend.TotalUploads(repo.DataPrefix + "/")

	res, err := runBackup(fileB, ident, backend, "11111111-2222-4333-8444-666666666666", nil)
	if err != nil {
		t.Fatalf("second runBackup() error: %v", err)
	}
	if res.UploadedChunks != 0 || res.SkippedChunks != res.TotalChunks {
		t.Fatalf("second result = %+v, want all skipped", res)
	}
	if backend.TotalUploads(repo.DataPrefix+"/") != before {
		t.Fatal("second backup re-uploaded data objects")
	}
}

func TestBackupFailsWithoutRepository(t *testing.T) {
	ident, _, _ := repoFixture(t)
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
	ident, backend, _ := repoFixture(t)
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
	ident, backend, _ := repoFixture(t)
	payload := []byte("0123456789abcdefghij")
	filePath := writeTempFile(t, payload)
	snapshotID := "99999999-8888-4777-8666-555555555555"
	if _, err := runBackup(filePath, ident, backend, snapshotID, nil); err != nil {
		t.Fatalf("runBackup() error: %v", err)
	}
	m, err := readManifest(backend, ident, snapshotID)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Chunks) == 0 {
		t.Fatal("no chunks in manifest")
	}
	if !backend.Has(m.Chunks[0].Name) {
		t.Fatalf("expected object at %s", m.Chunks[0].Name)
	}
}

func TestBackupCDCQualityAfterInsert(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	base := make([]byte, 200_000)
	for i := range base {
		base[i] = byte(i * 31)
	}
	fileA := writeTempFile(t, base)
	snapA := "aaaaaaaa-bbbb-4ccc-8ddd-000000000001"
	resA, err := runBackup(fileA, ident, backend, snapA, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resA.TotalChunks < 3 {
		t.Fatalf("TotalChunks = %d, want >= 3", resA.TotalChunks)
	}

	// Insert one byte near the start; most later chunk hashes should survive.
	shifted := make([]byte, 0, len(base)+1)
	shifted = append(shifted, base[:100]...)
	shifted = append(shifted, 0xAB)
	shifted = append(shifted, base[100:]...)
	fileB := writeTempFile(t, shifted)
	snapB := "aaaaaaaa-bbbb-4ccc-8ddd-000000000002"
	resB, err := runBackup(fileB, ident, backend, snapB, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resB.SkippedChunks == 0 {
		t.Fatal("expected some chunks reused after single-byte insert")
	}
}
