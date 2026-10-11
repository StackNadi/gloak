package index

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"gloak/internal/crypto"
	"gloak/internal/storage/memtest"
)

func identityFixture(t *testing.T) (string, string) {
	t.Helper()
	pub, _, fileContent, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error: %v", err)
	}
	path := filepath.Join(t.TempDir(), "identity.txt")
	if err := os.WriteFile(path, []byte(fileContent), 0600); err != nil {
		t.Fatalf("write identity: %v", err)
	}
	return pub, path
}

func entryFixture(hash string) Entry {
	return Entry{
		PlainSHA256:     hash,
		PlainSize:       100,
		EncryptedSize:   120,
		EncryptedSHA256: hash + "-enc",
	}
}

func TestAddHasGet(t *testing.T) {
	ix := New()
	ix.Add(entryFixture("aaa"))
	if !ix.Has("aaa") {
		t.Fatal("Has() = false after Add")
	}
	if e, ok := ix.Get("aaa"); !ok || e.PlainSize != 100 {
		t.Fatalf("Get() = %+v, %v", e, ok)
	}
	if ix.Has("bbb") {
		t.Fatal("Has() = true for unknown hash")
	}
}

func TestSaveAndLoadAcrossEpochs(t *testing.T) {
	pub, ident := identityFixture(t)
	backend := memtest.NewMemoryBackend()

	ix := New()
	ix.Add(entryFixture("aaa"))
	ix.Add(entryFixture("bbb"))
	n, err := ix.Save(backend, []string{pub})
	if err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	if n != 2 {
		t.Fatalf("Save() = %d, want 2", n)
	}

	ix.Add(entryFixture("ccc"))
	if n, err := ix.Save(backend, []string{pub}); err != nil || n != 1 {
		t.Fatalf("second Save() = %d, %v, want 1, nil", n, err)
	}

	loaded, err := Load(backend, ident)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	for _, hash := range []string{"aaa", "bbb", "ccc"} {
		if !loaded.Has(hash) {
			t.Fatalf("Load() missing %s", hash)
		}
	}
}

func TestSaveSkipsEmptyEpoch(t *testing.T) {
	pub, _ := identityFixture(t)
	backend := memtest.NewMemoryBackend()
	ix := New()
	ix.Add(entryFixture("aaa"))
	if _, err := ix.Save(backend, []string{pub}); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	before := len(backend.KeysWithPrefix("index/"))
	n, err := ix.Save(backend, []string{pub})
	if err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	if n != 0 || len(backend.KeysWithPrefix("index/")) != before {
		t.Fatalf("empty Save() wrote files: n=%d files=%d", n, len(backend.KeysWithPrefix("index/")))
	}
}

func TestLoadSkipsUnreadableEpoch(t *testing.T) {
	pub, ident := identityFixture(t)
	backend := memtest.NewMemoryBackend()
	ix := New()
	ix.Add(entryFixture("aaa"))
	if _, err := ix.Save(backend, []string{pub}); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	// Corrupt one epoch file with garbage that is not valid age ciphertext.
	backend.Put("index/000002.age", []byte("garbage-not-age"))
	// Add a second valid epoch so we can confirm the merge continues.
	ix.Add(entryFixture("bbb"))
	if _, err := ix.Save(backend, []string{pub}); err != nil {
		t.Fatalf("second Save() error: %v", err)
	}

	loaded, err := Load(backend, ident)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if !loaded.Has("aaa") || !loaded.Has("bbb") {
		t.Fatalf("Load() lost entries: %+v", loaded.all)
	}
}

func TestLoadOnEmptyBackendIsClean(t *testing.T) {
	_, ident := identityFixture(t)
	loaded, err := Load(memtest.NewMemoryBackend(), ident)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(loaded.all) != 0 {
		t.Fatalf("Load() on empty backend = %+v", loaded.all)
	}
}

func TestSaveOutputIsEncryptedJSON(t *testing.T) {
	pub, ident := identityFixture(t)
	backend := memtest.NewMemoryBackend()
	ix := New()
	ix.Add(entryFixture("aaa"))
	if _, err := ix.Save(backend, []string{pub}); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	var buf bytes.Buffer
	if err := backend.Download("index/000001.age", &buf); err != nil {
		t.Fatalf("Download() error: %v", err)
	}
	reader, err := crypto.DecryptReader(ident, &buf)
	if err != nil {
		t.Fatalf("index file is not age-encrypted with identity: %v", err)
	}
	plain, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if !bytes.Contains(plain, []byte("aaa")) {
		t.Fatalf("decrypted index missing entry hash: %s", plain)
	}
}
