package flow

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"

	"gloak/internal/core"
	"gloak/internal/storage/memtest"
)

func testIdentityFile(t *testing.T) (recipient, identityPath string) {
	t.Helper()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	path := filepath.Join(t.TempDir(), "identity.txt")
	content := "# created: now\n# public key: " + id.Recipient().String() + "\n" + id.String() + "\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write identity: %v", err)
	}
	return id.Recipient().String(), path
}

func TestRunUploadWritesLocatorLast(t *testing.T) {
	recipient, _ := testIdentityFile(t)
	src := filepath.Join(t.TempDir(), "data.bin")
	if err := os.WriteFile(src, []byte("hello gloak"), 0600); err != nil {
		t.Fatal(err)
	}
	backend := memtest.NewMemoryBackend()

	uuid, err := RunUpload(src, []string{recipient}, backend)
	if err != nil {
		t.Fatalf("RunUpload() error: %v", err)
	}
	if uuid == "" {
		t.Fatal("RunUpload() returned empty uuid")
	}
	if !backend.Has(uuid + "/locator.json") {
		t.Fatalf("missing locator at %s/locator.json", uuid)
	}
	if !backend.Has(uuid + "/manifest.age") {
		t.Fatal("missing manifest.age")
	}
	if !backend.Has(uuid + "/chunk_00000") {
		t.Fatal("missing chunk_00000")
	}

	var loc fileLocator
	if err := json.Unmarshal(backend.Get(uuid+"/locator.json"), &loc); err != nil {
		t.Fatalf("locator json: %v", err)
	}
	if loc.Version != fileFormatVersion || loc.UUID != uuid {
		t.Fatalf("locator = %+v, want version %q uuid %q", loc, fileFormatVersion, uuid)
	}
	if backend.UploadCount(uuid+"/locator.json") != 1 {
		t.Fatal("locator uploaded more than once")
	}
}

func TestRunUploadRejectsEmptyRecipients(t *testing.T) {
	src := filepath.Join(t.TempDir(), "data.bin")
	if err := os.WriteFile(src, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := RunUpload(src, nil, memtest.NewMemoryBackend())
	if err == nil || !strings.Contains(err.Error(), "recipient") {
		t.Fatalf("err = %v, want recipient error", err)
	}
}

func TestFileChunkNameAndValidation(t *testing.T) {
	if got := fileChunkName(0); got != "chunk_00000" {
		t.Fatalf("fileChunkName(0) = %q", got)
	}
	if err := validateFileChunkName("chunk_00001"); err != nil {
		t.Fatalf("valid name rejected: %v", err)
	}
	for _, name := range []string{"chunk_100000", "chunk_000001"} {
		if err := validateFileChunkName(name); err != nil {
			t.Fatalf("validateFileChunkName(%q) rejected: %v", name, err)
		}
	}
	bad := []string{
		"chunk_0000",
		"chunk_00000x",
		"../evil",
		"chunk_00000/../x",
		"aaaa-bbbb-cccc-dddd-eeeeeeeeeeee/chunk_00000",
		"/etc/passwd",
	}
	for _, name := range bad {
		if err := validateFileChunkName(name); err == nil {
			t.Fatalf("validateFileChunkName(%q) succeeded, want error", name)
		}
	}
}

func TestRunUploadMultiChunk(t *testing.T) {
	recipient, _ := testIdentityFile(t)
	src := filepath.Join(t.TempDir(), "big.bin")
	// ChunkSize is 20MiB; write slightly over one chunk to force two chunks.
	data := bytes.Repeat([]byte("a"), int(core.ChunkSize)+10)
	if err := os.WriteFile(src, data, 0600); err != nil {
		t.Fatal(err)
	}
	backend := memtest.NewMemoryBackend()
	uuid, err := RunUpload(src, []string{recipient}, backend)
	if err != nil {
		t.Fatalf("RunUpload() error: %v", err)
	}
	if !backend.Has(uuid+"/chunk_00000") || !backend.Has(uuid+"/chunk_00001") {
		t.Fatal("expected two chunks")
	}
}
