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

func TestRunTreeRestoreKeepsUnsafeRootNameInsideOutput(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	// Base(Clean("/")) is "/", which is not a safe single path segment,
	// so the restore root falls back to the snapshot UUID.
	m := core.TreeManifest{
		Version:   "2.0",
		UUID:      "aaaaaaaa-bbbb-4ccc-8ddd-000000000031",
		Kind:      core.KindTree,
		CreatedAt: "2026-10-12T00:00:00Z",
		Hostname:  "test",
		Paths:     []string{"/"},
		Entries:   nil,
	}
	seedTreeSnapshot(t, backend, ident, m)

	out := t.TempDir()
	got, err := runRestore(m.UUID, out, ident, backend, "")
	if err != nil {
		t.Fatalf("runRestore() error: %v", err)
	}
	want := filepath.Join(out, m.UUID)
	if got != want {
		t.Fatalf("restore root = %q, want %q", got, want)
	}
}

func TestRunTreeRestoreRejectsUnsafeRootAndUUID(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	// The path id stays a valid UUID while the manifest content is
	// hostile: root name and UUID both escape the output directory.
	validID := "aaaaaaaa-bbbb-4ccc-8ddd-000000000032"
	m := core.TreeManifest{
		Version:   "2.0",
		UUID:      "../evil",
		Kind:      core.KindTree,
		CreatedAt: "2026-10-12T00:00:00Z",
		Hostname:  "test",
		Paths:     []string{".."},
		Entries:   nil,
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	r, err := repo.Open(backend, ident)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := appcrypto.EncryptMulti(r.Config.Recipients, raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Upload("snapshots/"+validID+".age", bytes.NewReader(sealed)); err != nil {
		t.Fatal(err)
	}

	if _, err := runRestore(validID, t.TempDir(), ident, backend, ""); err == nil {
		t.Fatal("runRestore() accepted a hostile root name and uuid")
	} else if !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("error = %v, want it to reject the unsafe root", err)
	}
}

func TestRunTreeRestoreFilterMatchingNothingFails(t *testing.T) {
	_, backend, ident, snap := treeFixture(t)
	_, err := runRestore(snap, t.TempDir(), ident, backend, "does/not/exist")
	if err == nil {
		t.Fatal("runRestore() succeeded with a filter that matches nothing")
	}
	if !strings.Contains(err.Error(), "matches nothing") {
		t.Fatalf("error = %v, want it to report the unmatched filter", err)
	}
}

func TestRunTreeRestoreAppliesDirModesAfterChildren(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	if err := os.MkdirAll(locked, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "f.txt"), []byte("inside"), 0444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0555); err != nil {
		t.Fatal(err)
	}
	// Restore write access so t.TempDir can clean up after the test.
	t.Cleanup(func() { _ = os.Chmod(locked, 0755) })
	snap := "aaaaaaaa-bbbb-4ccc-8ddd-000000000033"
	if _, err := runBackupPath(root, ident, backend, snap, nil); err != nil {
		t.Fatal(err)
	}

	out := t.TempDir()
	got, err := runRestore(snap, out, ident, backend, "")
	if err != nil {
		t.Fatalf("runRestore() error: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(got, "locked"), 0755) })
	fi, err := os.Stat(filepath.Join(got, "locked"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0555 {
		t.Fatalf("dir mode = %o, want 555", fi.Mode().Perm())
	}
	fi, err = os.Stat(filepath.Join(got, "locked", "f.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0444 {
		t.Fatalf("file mode = %o, want 444", fi.Mode().Perm())
	}
	data, err := os.ReadFile(filepath.Join(got, "locked", "f.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "inside" {
		t.Fatalf("content = %q", data)
	}

	// Restoring again over the read-only tree must succeed and keep the
	// recorded modes.
	if _, err := runRestore(snap, out, ident, backend, ""); err != nil {
		t.Fatalf("re-restore over read-only tree: %v", err)
	}
	fi, err = os.Stat(filepath.Join(got, "locked"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0555 {
		t.Fatalf("dir mode after re-restore = %o, want 555", fi.Mode().Perm())
	}
	fi, err = os.Stat(filepath.Join(got, "locked", "f.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0444 {
		t.Fatalf("file mode after re-restore = %o, want 444", fi.Mode().Perm())
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
