package flow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gloak/internal/core"
	appcrypto "gloak/internal/crypto"
	"gloak/internal/repo"
	"gloak/internal/storage"
)

func TestBackupVerifyRestoreRoundTrip(t *testing.T) {
	ident, backend, _ := repoFixture(t, 8)
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
	restoredPath, err := runRestore(snapshotID, outDir, ident, backend)
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
	ident, backend, _ := repoFixture(t, 8)
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
	ident, backend, pub := repoFixture(t, 8)
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

	if _, err := runRestore(snapshotID, t.TempDir(), ident, backend); err == nil {
		t.Fatal("runRestore() succeeded with escaping chunk path")
	} else if !strings.Contains(err.Error(), "escapes data prefix") {
		t.Fatalf("error = %v, want escapes data prefix", err)
	}
}

func TestRunUploadWithStateDeletesStateAfterSuccess(t *testing.T) {
	pubKey, _, _, err := appcrypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() returned error: %v", err)
	}

	sourcePath := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(sourcePath, []byte("payload"), 0600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	backend := newMemoryBackend()
	stateDir := t.TempDir()
	backupID := "7f91c6c7-7a0b-44aa-ae23-997b60e4e998"
	if err := runUploadWithState(sourcePath, pubKey, backend, backupID, "memory:backups", stateDir); err != nil {
		t.Fatalf("runUploadWithState() returned error: %v", err)
	}

	if _, err := os.Stat(uploadStatePath(stateDir, backupID)); !os.IsNotExist(err) {
		t.Fatalf("upload state should be deleted after success, stat error: %v", err)
	}
}

func TestRunUploadWithStateKeepsUploadedChunksAfterFailure(t *testing.T) {
	pubKey, _, _, err := appcrypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() returned error: %v", err)
	}

	sourcePath := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(sourcePath, []byte("payload"), 0600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	backupID := "7f91c6c7-7a0b-44aa-ae23-997b60e4e998"
	backend := &failUploadBackend{
		memoryBackend: newMemoryBackend(),
		failPath:      backupID + "/manifest.age",
	}
	stateDir := t.TempDir()
	err = runUploadWithState(sourcePath, pubKey, backend, backupID, "memory:backups", stateDir)
	if err == nil {
		t.Fatalf("runUploadWithState() succeeded despite manifest upload failure")
	}
	if !strings.Contains(err.Error(), "failed to upload manifest.age") {
		t.Fatalf("runUploadWithState() error = %q, want manifest upload failure", err)
	}

	state, err := loadUploadState(stateDir, backupID)
	if err != nil {
		t.Fatalf("loadUploadState() returned error: %v", err)
	}
	if state.Remote != "memory:backups" || state.Recipient != pubKey {
		t.Fatalf("state remote/recipient = %q/%q, want memory:backups/%q", state.Remote, state.Recipient, pubKey)
	}
	absSourcePath, err := filepath.Abs(sourcePath)
	if err != nil {
		t.Fatalf("failed to resolve source path: %v", err)
	}
	if state.SourcePath != absSourcePath {
		t.Fatalf("state SourcePath = %q, want %q", state.SourcePath, absSourcePath)
	}
	if len(state.UploadedChunks) != 1 {
		t.Fatalf("uploaded chunks in state = %d, want 1", len(state.UploadedChunks))
	}
	if state.UploadedChunks[0].Name != "chunk_00000" || state.UploadedChunks[0].PlainSize == 0 || state.UploadedChunks[0].PlainSHA256 == "" || state.UploadedChunks[0].EncryptedSize == 0 || state.UploadedChunks[0].EncryptedSHA256 == "" {
		t.Fatalf("uploaded chunk metadata not recorded correctly: %+v", state.UploadedChunks[0])
	}
}

func TestRunUploadWithStateStopsAtMidUploadFailure(t *testing.T) {
	pubKey, _, _, err := appcrypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() returned error: %v", err)
	}

	sourcePath := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(sourcePath, []byte("payload large enough for several encrypted chunks"), 0600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	backupID := "7f91c6c7-7a0b-44aa-ae23-997b60e4e998"
	backend := &failUploadBackend{
		memoryBackend: newMemoryBackend(),
		failPath:      backupID + "/chunk_00001",
	}
	stateDir := t.TempDir()
	err = runUploadWithStateAndChunkSize(sourcePath, pubKey, backend, backupID, "memory:backups", stateDir, 8)
	if err == nil {
		t.Fatalf("runUploadWithStateAndChunkSize() succeeded despite chunk upload failure")
	}
	if !strings.Contains(err.Error(), "chunk 1 failed to process") {
		t.Fatalf("runUploadWithStateAndChunkSize() error = %q, want chunk 1 failure", err)
	}

	state, err := loadUploadState(stateDir, backupID)
	if err != nil {
		t.Fatalf("loadUploadState() returned error: %v", err)
	}
	if state.ChunkSize != 8 {
		t.Fatalf("state ChunkSize = %d, want 8", state.ChunkSize)
	}
	if len(state.UploadedChunks) != 1 {
		t.Fatalf("uploaded chunks in state = %d, want 1", len(state.UploadedChunks))
	}
	if state.UploadedChunks[0].Name != "chunk_00000" {
		t.Fatalf("recorded chunk = %q, want chunk_00000", state.UploadedChunks[0].Name)
	}

	if !backend.has(backupID + "/chunk_00000") {
		t.Fatalf("chunk_00000 should exist after first chunk upload")
	}
	for _, path := range []string{backupID + "/chunk_00001", backupID + "/manifest.age", backupID + "/locator.json"} {
		if backend.has(path) {
			t.Fatalf("%s should not exist after mid-upload failure", path)
		}
	}
}

func TestRunResumeCompletesInterruptedUpload(t *testing.T) {
	pubKey, _, _, err := appcrypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() returned error: %v", err)
	}

	sourcePath := filepath.Join(t.TempDir(), "secret.txt")
	wantPayload := []byte("payload large enough for several encrypted chunks")
	if err := os.WriteFile(sourcePath, wantPayload, 0600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	backupID := "7f91c6c7-7a0b-44aa-ae23-997b60e4e998"
	memory := newMemoryBackend()
	failing := &failUploadBackend{
		memoryBackend: memory,
		failPath:      backupID + "/chunk_00001",
	}
	stateDir := t.TempDir()
	if err := runUploadWithStateAndChunkSize(sourcePath, pubKey, failing, backupID, "memory:backups", stateDir, 8); err == nil {
		t.Fatalf("runUploadWithStateAndChunkSize() succeeded despite chunk upload failure")
	}

	if err := runResume(backupID, func(string) storage.Backend { return memory }, stateDir); err != nil {
		t.Fatalf("runResume() returned error: %v", err)
	}
	if _, err := os.Stat(uploadStatePath(stateDir, backupID)); !os.IsNotExist(err) {
		t.Fatalf("upload state should be deleted after resume, stat error: %v", err)
	}
	if memory.uploadCount(backupID+"/chunk_00000") != 1 {
		t.Fatalf("chunk_00000 upload count = %d, want 1", memory.uploadCount(backupID+"/chunk_00000"))
	}
	if memory.uploadCount(backupID+"/chunk_00001") != 1 {
		t.Fatalf("chunk_00001 upload count = %d, want 1", memory.uploadCount(backupID+"/chunk_00001"))
	}
}

func TestRunResumeRejectsStateChunkPlainHashMismatch(t *testing.T) {
	pubKey, _, _, err := appcrypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() returned error: %v", err)
	}

	sourcePath := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(sourcePath, []byte("payload large enough for several encrypted chunks"), 0600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	backupID := "7f91c6c7-7a0b-44aa-ae23-997b60e4e998"
	memory := newMemoryBackend()
	failing := &failUploadBackend{
		memoryBackend: memory,
		failPath:      backupID + "/chunk_00001",
	}
	stateDir := t.TempDir()
	if err := runUploadWithStateAndChunkSize(sourcePath, pubKey, failing, backupID, "memory:backups", stateDir, 8); err == nil {
		t.Fatalf("runUploadWithStateAndChunkSize() succeeded despite chunk upload failure")
	}

	state, err := loadUploadState(stateDir, backupID)
	if err != nil {
		t.Fatalf("loadUploadState() returned error: %v", err)
	}
	state.UploadedChunks[0].PlainSHA256 = "bad-hash"
	if err := saveUploadState(stateDir, state); err != nil {
		t.Fatalf("saveUploadState() returned error: %v", err)
	}

	err = runResume(backupID, func(string) storage.Backend { return memory }, stateDir)
	if err == nil {
		t.Fatalf("runResume() succeeded with mismatched state chunk hash")
	}
	if !strings.Contains(err.Error(), "does not match source file") {
		t.Fatalf("runResume() error = %q, want source mismatch error", err)
	}
}

type memoryBackend struct {
	mu      sync.Mutex
	files   map[string][]byte
	uploads map[string]int
}

type failUploadBackend struct {
	*memoryBackend
	failPath string
}

func (f *failUploadBackend) Upload(remotePath string, in io.Reader) error {
	if remotePath == f.failPath {
		_, _ = io.Copy(io.Discard, in)
		return fmt.Errorf("forced upload failure for %s", remotePath)
	}
	return f.memoryBackend.Upload(remotePath, in)
}

func newMemoryBackend() *memoryBackend {
	return &memoryBackend{files: make(map[string][]byte), uploads: make(map[string]int)}
}

func (m *memoryBackend) Upload(remotePath string, in io.Reader) error {
	data, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	m.put(remotePath, data)
	m.mu.Lock()
	m.uploads[remotePath]++
	m.mu.Unlock()
	return nil
}

func (m *memoryBackend) Download(remotePath string, out io.Writer) error {
	m.mu.Lock()
	data, ok := m.files[remotePath]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("not found: %s", remotePath)
	}
	_, err := out.Write(data)
	return err
}

func (m *memoryBackend) ListFiles(prefix []string) ([]byte, error) {
	return json.Marshal([]struct{}{})
}

func (m *memoryBackend) Delete(remotePath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, remotePath)
	return nil
}

func (m *memoryBackend) has(remotePath string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.files[remotePath]
	return ok
}

func (m *memoryBackend) put(remotePath string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[remotePath] = append([]byte(nil), data...)
}

func (m *memoryBackend) uploadCount(remotePath string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.uploads[remotePath]
}
