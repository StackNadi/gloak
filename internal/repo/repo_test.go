package repo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gloak/internal/crypto"
	"gloak/internal/storage/memtest"
)

func testRecipient(t *testing.T) (string, string) {
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

func TestInitWritesKeysAndConfig(t *testing.T) {
	pubA, _ := testRecipient(t)
	pubB, _ := testRecipient(t)
	backend := memtest.NewMemoryBackend()

	master, cfg, err := Init(backend, []string{pubA, pubB}, 4096, false)
	if err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	if len(master) != 32 {
		t.Fatalf("master key length = %d, want 32", len(master))
	}
	if cfg.Version != FormatVersion || cfg.ChunkSize != 4096 {
		t.Fatalf("cfg = %+v", cfg)
	}
	if !backend.Has(ConfigPath) {
		t.Fatal("repo.json not uploaded")
	}
	for _, pub := range []string{pubA, pubB} {
		path := KeysPathFor(pub)
		if !backend.Has(path) {
			t.Fatalf("key file %s not uploaded", path)
		}
	}
}

func TestOpenWithEachRecipientIdentity(t *testing.T) {
	pubA, identA := testRecipient(t)
	pubB, identB := testRecipient(t)
	backend := memtest.NewMemoryBackend()
	master, _, err := Init(backend, []string{pubA, pubB}, 4096, false)
	if err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	for _, identPath := range []string{identA, identB} {
		r, err := Open(backend, identPath)
		if err != nil {
			t.Fatalf("Open(%s) error: %v", identPath, err)
		}
		if string(r.MasterKey) != string(master) {
			t.Fatalf("Open(%s) master key mismatch", identPath)
		}
	}
}

func TestOpenRejectsWrongIdentity(t *testing.T) {
	pubA, _ := testRecipient(t)
	_, identOther := testRecipient(t)
	backend := memtest.NewMemoryBackend()
	if _, _, err := Init(backend, []string{pubA}, 4096, false); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	_, err := Open(backend, identOther)
	if err == nil {
		t.Fatal("Open() succeeded with unrelated identity")
	}
	if !strings.Contains(err.Error(), "cannot open") {
		t.Fatalf("Open() error = %v, want cannot open", err)
	}
}

func TestOpenRejectsMissingConfig(t *testing.T) {
	_, ident := testRecipient(t)
	_, err := Open(memtest.NewMemoryBackend(), ident)
	if err == nil {
		t.Fatal("Open() succeeded on empty backend")
	}
	if !strings.Contains(err.Error(), ConfigPath) {
		t.Fatalf("Open() error = %v, want repo.json mention", err)
	}
}

func TestDataPathUsesHashSharding(t *testing.T) {
	got := DataPath(strings.Repeat("ab", 32))
	want := "data/ab/" + strings.Repeat("ab", 32)
	if got != want {
		t.Fatalf("DataPath() = %q, want %q", got, want)
	}
}

func TestDataPathRejectsShortHash(t *testing.T) {
	for _, in := range []string{"a", ""} {
		got := DataPath(in)
		if got != "data/invalid" {
			t.Fatalf("DataPath(%q) = %q, want %q", in, got, "data/invalid")
		}
	}
}

func TestSnapshotPathRejectsInvalidUUID(t *testing.T) {
	if _, err := SnapshotPath("../escape"); err == nil {
		t.Fatal("SnapshotPath() accepted invalid uuid")
	}
	got, err := SnapshotPath("7f91c6c7-7a0b-44aa-ae23-997b60e4e998")
	if err != nil {
		t.Fatalf("SnapshotPath() error: %v", err)
	}
	if got != "snapshots/7f91c6c7-7a0b-44aa-ae23-997b60e4e998.age" {
		t.Fatalf("SnapshotPath() = %q", got)
	}
}
