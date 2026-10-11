package flow

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"gloak/internal/crypto"
	"gloak/internal/storage/memtest"
)

func resealFileManifest(t *testing.T, backend *memtest.MemoryBackend, identityPath, uuid string, m fileManifest) {
	t.Helper()
	data, err := os.ReadFile(identityPath)
	if err != nil {
		t.Fatal(err)
	}
	var recipient string
	for _, line := range bytes.Split(data, []byte("\n")) {
		if bytes.HasPrefix(bytes.TrimSpace(line), []byte("# public key:")) {
			recipient = string(bytes.TrimSpace(bytes.TrimPrefix(bytes.TrimSpace(line), []byte("# public key:"))))
			break
		}
	}
	if recipient == "" {
		t.Fatal("no public key in identity file")
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := crypto.EncryptMulti([]string{recipient}, raw)
	if err != nil {
		t.Fatal(err)
	}
	backend.Put(uuid+"/manifest.age", sealed)
}

func TestRunRestoreFileModeRoundTrip(t *testing.T) {
	recipient, identityPath := testIdentityFile(t)
	src := filepath.Join(t.TempDir(), "secret.txt")
	want := []byte("restore me please\n")
	if err := os.WriteFile(src, want, 0600); err != nil {
		t.Fatal(err)
	}
	backend := memtest.NewMemoryBackend()
	backupID, err := RunUpload(src, []string{recipient}, backend)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	outDir := t.TempDir()
	gotPath, err := runRestore(backupID, outDir, identityPath, backend, "")
	if err != nil {
		t.Fatalf("runRestore() error: %v", err)
	}
	got, err := os.ReadFile(gotPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("restored %q, want %q", got, want)
	}
}

func TestRunRestoreFileModeEmptyFile(t *testing.T) {
	recipient, identityPath := testIdentityFile(t)
	src := filepath.Join(t.TempDir(), "empty.bin")
	if err := os.WriteFile(src, nil, 0600); err != nil {
		t.Fatal(err)
	}
	backend := memtest.NewMemoryBackend()
	backupID, err := RunUpload(src, []string{recipient}, backend)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	outDir := t.TempDir()
	gotPath, err := runRestore(backupID, outDir, identityPath, backend, "")
	if err != nil {
		t.Fatalf("runRestore() error: %v", err)
	}
	got, err := os.ReadFile(gotPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("restored %d bytes, want empty", len(got))
	}
}

func TestRunRestoreFileModeEitherIdentity(t *testing.T) {
	r1, id1 := testIdentityFile(t)
	r2, id2 := testIdentityFile(t)
	src := filepath.Join(t.TempDir(), "m.bin")
	if err := os.WriteFile(src, []byte("two keys"), 0600); err != nil {
		t.Fatal(err)
	}
	backend := memtest.NewMemoryBackend()
	backupID, err := RunUpload(src, []string{r1, r2}, backend)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{id1, id2} {
		out := t.TempDir()
		if _, err := runRestore(backupID, out, id, backend, ""); err != nil {
			t.Fatalf("restore with %s: %v", id, err)
		}
	}
}

func TestRunRestoreFileModeMissingLocator(t *testing.T) {
	recipient, identityPath := testIdentityFile(t)
	src := filepath.Join(t.TempDir(), "a.bin")
	if err := os.WriteFile(src, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	backend := memtest.NewMemoryBackend()
	backupID, err := RunUpload(src, []string{recipient}, backend)
	if err != nil {
		t.Fatal(err)
	}
	backend.Delete(backupID + "/locator.json")

	// Without locator, isFileMode is false, so restore falls through to
	// repository Open. Either "incomplete" or a repository error is fine;
	// we only assert it fails.
	if _, err := runRestore(backupID, t.TempDir(), identityPath, backend, ""); err == nil {
		t.Fatal("expected error for missing locator")
	}
}

func TestRunRestoreFileModeRejectsTraversalManifest(t *testing.T) {
	recipient, identityPath := testIdentityFile(t)
	src := filepath.Join(t.TempDir(), "a.bin")
	if err := os.WriteFile(src, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	backend := memtest.NewMemoryBackend()
	backupID, err := RunUpload(src, []string{recipient}, backend)
	if err != nil {
		t.Fatal(err)
	}
	m, err := readFileManifest(backend, identityPath, backupID)
	if err != nil {
		t.Fatal(err)
	}
	m.Chunks[0].Name = "../evil"
	resealFileManifest(t, backend, identityPath, backupID, m)

	_, err = runRestore(backupID, t.TempDir(), identityPath, backend, "")
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("invalid chunk")) {
		t.Fatalf("err = %v, want invalid chunk", err)
	}
}

func TestRunRestoreFileModeCorruptChunk(t *testing.T) {
	recipient, identityPath := testIdentityFile(t)
	src := filepath.Join(t.TempDir(), "a.bin")
	if err := os.WriteFile(src, []byte("abcdefgh"), 0600); err != nil {
		t.Fatal(err)
	}
	backend := memtest.NewMemoryBackend()
	backupID, err := RunUpload(src, []string{recipient}, backend)
	if err != nil {
		t.Fatal(err)
	}
	raw := backend.Get(backupID + "/chunk_00000")
	raw[len(raw)/2] ^= 0xff
	backend.Put(backupID+"/chunk_00000", raw)

	_, err = runRestore(backupID, t.TempDir(), identityPath, backend, "")
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("CHUNK CORRUPT")) {
		t.Fatalf("err = %v, want CHUNK CORRUPT", err)
	}
}

func TestRunVerifyFileModeOK(t *testing.T) {
	recipient, identityPath := testIdentityFile(t)
	src := filepath.Join(t.TempDir(), "v.bin")
	if err := os.WriteFile(src, []byte("verify me"), 0600); err != nil {
		t.Fatal(err)
	}
	backend := memtest.NewMemoryBackend()
	backupID, err := RunUpload(src, []string{recipient}, backend)
	if err != nil {
		t.Fatal(err)
	}
	if err := runVerify(backupID, identityPath, backend); err != nil {
		t.Fatalf("runVerify() error: %v", err)
	}
}

func TestRunVerifyFileModeCorrupt(t *testing.T) {
	recipient, identityPath := testIdentityFile(t)
	src := filepath.Join(t.TempDir(), "v.bin")
	if err := os.WriteFile(src, []byte("verify me"), 0600); err != nil {
		t.Fatal(err)
	}
	backend := memtest.NewMemoryBackend()
	backupID, err := RunUpload(src, []string{recipient}, backend)
	if err != nil {
		t.Fatal(err)
	}
	raw := backend.Get(backupID + "/chunk_00000")
	raw[0] ^= 0xff
	backend.Put(backupID+"/chunk_00000", raw)

	err = runVerify(backupID, identityPath, backend)
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("CHUNK CORRUPT")) {
		t.Fatalf("err = %v, want CHUNK CORRUPT", err)
	}
}

func TestRunVerifyFileModeMissingLocator(t *testing.T) {
	recipient, identityPath := testIdentityFile(t)
	src := filepath.Join(t.TempDir(), "v.bin")
	if err := os.WriteFile(src, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	backend := memtest.NewMemoryBackend()
	backupID, err := RunUpload(src, []string{recipient}, backend)
	if err != nil {
		t.Fatal(err)
	}
	backend.Delete(backupID + "/locator.json")

	// Without locator, falls through to repository verify; must fail.
	if err := runVerify(backupID, identityPath, backend); err == nil {
		t.Fatal("expected error for missing locator")
	}
}
