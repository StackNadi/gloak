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

	appcrypto "gloak/internal/crypto"
)

func TestUploadVerifyRestoreRoundTrip(t *testing.T) {
	pubKey, _, identityFile, err := appcrypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() returned error: %v", err)
	}
	identityPath := writeFlowIdentityFile(t, identityFile)

	sourcePath := filepath.Join(t.TempDir(), "secret.txt")
	wantPayload := []byte("important backup payload\nwith enough bytes to test restore")
	if err := os.WriteFile(sourcePath, wantPayload, 0600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	backend := newMemoryBackend()
	backupID := "7f91c6c7-7a0b-44aa-ae23-997b60e4e998"

	if err := runUpload(sourcePath, pubKey, backend, backupID); err != nil {
		t.Fatalf("runUpload() returned error: %v", err)
	}
	for _, path := range []string{backupID + "/chunk_00000", backupID + "/manifest.age", backupID + "/locator.json"} {
		if !backend.has(path) {
			t.Fatalf("uploaded path %q not found", path)
		}
	}

	if err := runVerify(backupID, identityPath, backend); err != nil {
		t.Fatalf("runVerify() returned error: %v", err)
	}

	outputDir := t.TempDir()
	if err := runRestore(backupID, identityPath, backend, outputDir); err != nil {
		t.Fatalf("runRestore() returned error: %v", err)
	}

	gotPayload, err := os.ReadFile(filepath.Join(outputDir, "secret.txt"))
	if err != nil {
		t.Fatalf("failed to read restored file: %v", err)
	}
	if !bytes.Equal(gotPayload, wantPayload) {
		t.Fatalf("restored payload = %q, want %q", gotPayload, wantPayload)
	}
}

func TestVerifyRejectsCorruptChunk(t *testing.T) {
	pubKey, _, identityFile, err := appcrypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() returned error: %v", err)
	}
	identityPath := writeFlowIdentityFile(t, identityFile)

	sourcePath := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(sourcePath, []byte("payload"), 0600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	backend := newMemoryBackend()
	backupID := "7f91c6c7-7a0b-44aa-ae23-997b60e4e998"
	if err := runUpload(sourcePath, pubKey, backend, backupID); err != nil {
		t.Fatalf("runUpload() returned error: %v", err)
	}

	backend.put(backupID+"/chunk_00000", []byte("tampered"))
	err = runVerify(backupID, identityPath, backend)
	if err == nil {
		t.Fatalf("runVerify() succeeded with corrupt chunk")
	}
	if !strings.Contains(err.Error(), "CHUNK CORRUPT") {
		t.Fatalf("runVerify() error = %q, want chunk corrupt", err)
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
	if state.UploadedChunks[0].Name != "chunk_00000" || state.UploadedChunks[0].Size == 0 || state.UploadedChunks[0].SHA256 == "" {
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

type memoryBackend struct {
	mu    sync.Mutex
	files map[string][]byte
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
	return &memoryBackend{files: make(map[string][]byte)}
}

func (m *memoryBackend) Upload(remotePath string, in io.Reader) error {
	data, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	m.put(remotePath, data)
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

func writeFlowIdentityFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "identity.txt")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write identity file: %v", err)
	}
	return path
}
