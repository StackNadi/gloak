package flow

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gloak/internal/core"
	appcrypto "gloak/internal/crypto"
	"gloak/internal/repo"
	"gloak/internal/storage/memtest"
)

func treeFixture(t *testing.T) (string, *memtest.MemoryBackend, string, string) {
	t.Helper()
	ident, backend, _ := repoFixture(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub", "empty"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "b.txt"), []byte("beta"), 0644); err != nil {
		t.Fatal(err)
	}
	snap := "aaaaaaaa-bbbb-4ccc-8ddd-000000000020"
	if _, err := runBackupPath(root, ident, backend, snap, nil); err != nil {
		t.Fatal(err)
	}
	return root, backend, ident, snap
}

func TestRunTreeRestoreFull(t *testing.T) {
	_, backend, ident, snap := treeFixture(t)
	out := t.TempDir()
	got, err := runRestore(snap, out, ident, backend, "")
	if err != nil {
		t.Fatalf("runRestore() error: %v", err)
	}
	if base := filepath.Base(got); base == "" {
		t.Fatal("empty restore root")
	}
	a, err := os.ReadFile(filepath.Join(got, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != "alpha" {
		t.Fatalf("a.txt = %q", a)
	}
	b, err := os.ReadFile(filepath.Join(got, "sub", "b.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "beta" {
		t.Fatalf("b.txt = %q", b)
	}
	if fi, err := os.Stat(filepath.Join(got, "sub", "empty")); err != nil || !fi.IsDir() {
		t.Fatal("empty dir not restored")
	}
}

func TestRunTreeRestorePathFilter(t *testing.T) {
	_, backend, ident, snap := treeFixture(t)
	out := t.TempDir()
	got, err := runRestore(snap, out, ident, backend, "sub")
	if err != nil {
		t.Fatalf("runRestore() error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(got, "a.txt")); err == nil {
		t.Fatal("a.txt restored despite filter")
	}
	b, err := os.ReadFile(filepath.Join(got, "sub", "b.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "beta" {
		t.Fatalf("b.txt = %q", b)
	}
}

func TestRunTreeRestoreRejectsUnsafeRelPath(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	m := core.TreeManifest{
		Version:   "2.0",
		UUID:      "aaaaaaaa-bbbb-4ccc-8ddd-000000000030",
		Kind:      core.KindTree,
		CreatedAt: "2026-10-12T00:00:00Z",
		Hostname:  "test",
		Paths:     []string{"/tmp/x"},
		Entries: []core.TreeEntry{
			{RelPath: "../escape", Type: "file", Mode: 0644, Mtime: "2026-10-12T00:00:00Z", Size: 0},
		},
	}
	seedTreeSnapshot(t, backend, ident, m)

	_, err := runRestore(m.UUID, t.TempDir(), ident, backend, "")
	if err == nil {
		t.Fatal("runRestore() accepted traversal RelPath")
	}
	if !strings.Contains(err.Error(), "unsafe") && !strings.Contains(err.Error(), "..") {
		t.Fatalf("error = %v", err)
	}
}

func seedTreeSnapshot(t *testing.T, backend *memtest.MemoryBackend, ident string, m core.TreeManifest) {
	t.Helper()
	r, err := repo.Open(backend, ident)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := appcrypto.EncryptMulti(r.Config.Recipients, raw)
	if err != nil {
		t.Fatal(err)
	}
	p, err := repo.SnapshotPath(m.UUID)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Upload(p, bytes.NewReader(sealed)); err != nil {
		t.Fatal(err)
	}
}
