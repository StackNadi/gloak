package index

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gloak/internal/crypto"
	"gloak/internal/storage"
	"gloak/internal/storage/memtest"
)

// listErrorBackend fails every listing with err while staying usable for
// anything else, so Load error handling can be exercised.
type listErrorBackend struct {
	storage.Backend
	err error
}

func (b *listErrorBackend) ListFiles(prefix []string) ([]byte, error) {
	return nil, b.err
}

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

func TestAllHashesMergesPendingAndAll(t *testing.T) {
	ix := New()
	ix.Add(entryFixture("bbb"))
	ix.Add(entryFixture("aaa"))
	got := ix.AllHashes()
	want := []string{"aaa", "bbb"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("AllHashes() = %v, want %v", got, want)
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
	ix.Add(entryFixture("bbb"))
	if _, err := ix.Save(backend, []string{pub}); err != nil {
		t.Fatalf("second Save() error: %v", err)
	}
	// Corrupt the third epoch with garbage that is not valid age ciphertext,
	// after both valid epochs are already written.
	backend.Put("index/000003.age", []byte("garbage-not-age"))

	loaded, err := Load(backend, ident)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if !loaded.Has("aaa") || !loaded.Has("bbb") {
		t.Fatalf("Load() lost entries: %+v", loaded.all)
	}
}

func TestLoadRestoresEpochCounter(t *testing.T) {
	pub, ident := identityFixture(t)
	backend := memtest.NewMemoryBackend()

	ix := New()
	ix.Add(entryFixture("aaa"))
	if _, err := ix.Save(backend, []string{pub}); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	ix.Add(entryFixture("bbb"))
	if _, err := ix.Save(backend, []string{pub}); err != nil {
		t.Fatalf("second Save() error: %v", err)
	}

	loaded, err := Load(backend, ident)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	loaded.Add(entryFixture("ccc"))
	if _, err := loaded.Save(backend, []string{pub}); err != nil {
		t.Fatalf("Save() after Load error: %v", err)
	}

	keys := backend.KeysWithPrefix("index/")
	want := []string{"index/000001.age", "index/000002.age", "index/000003.age"}
	if len(keys) != len(want) {
		t.Fatalf("index files = %v, want %v", keys, want)
	}
	for i, k := range want {
		if keys[i] != k {
			t.Fatalf("index files = %v, want %v", keys, want)
		}
	}

	reloaded, err := Load(backend, ident)
	if err != nil {
		t.Fatalf("Load() after save error: %v", err)
	}
	for _, hash := range []string{"aaa", "bbb", "ccc"} {
		if !reloaded.Has(hash) {
			t.Fatalf("reloaded index missing %s", hash)
		}
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

func TestLoadTreatsMissingPathAsEmpty(t *testing.T) {
	_, ident := identityFixture(t)
	// rclone exits 3 when the remote path is missing; that is a fresh repo.
	exitErr := exec.Command("sh", "-c", "exit 3").Run()
	backend := &listErrorBackend{
		Backend: memtest.NewMemoryBackend(),
		err:     fmt.Errorf("failed to list index: %w", exitErr),
	}
	loaded, err := Load(backend, ident)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(loaded.AllHashes()) != 0 {
		t.Fatalf("Load() on missing index = %v, want empty", loaded.AllHashes())
	}
}

func TestLoadPropagatesListingErrors(t *testing.T) {
	_, ident := identityFixture(t)
	backend := &listErrorBackend{
		Backend: memtest.NewMemoryBackend(),
		err:     errors.New("remote exploded"),
	}
	if _, err := Load(backend, ident); err == nil {
		t.Fatal("Load() succeeded on a real listing error")
	} else if !strings.Contains(err.Error(), "remote exploded") {
		t.Fatalf("Load() error = %v, want it to wrap the listing failure", err)
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
