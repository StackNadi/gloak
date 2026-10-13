package flow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunTreeBackupNestedDirs(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub", "empty"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "b.txt"), []byte("beta-beta-beta"), 0644); err != nil {
		t.Fatal(err)
	}

	snap := "aaaaaaaa-bbbb-4ccc-8ddd-000000000010"
	res, err := runBackupPath(root, ident, backend, snap, nil)
	if err != nil {
		t.Fatalf("runBackupPath() error: %v", err)
	}
	if res.TotalChunks < 2 {
		t.Fatalf("TotalChunks = %d, want >= 2", res.TotalChunks)
	}
	m, err := readTreeManifest(backend, ident, snap)
	if err != nil {
		t.Fatal(err)
	}
	if m.Kind != "tree" {
		t.Fatalf("Kind = %q", m.Kind)
	}
	if m.FileCount() != 2 {
		t.Fatalf("FileCount = %d", m.FileCount())
	}
	var paths []string
	for _, e := range m.Entries {
		paths = append(paths, e.RelPath)
	}
	if paths[0] != "a.txt" {
		t.Fatalf("first entry = %q, want a.txt", paths[0])
	}
	foundEmpty := false
	for _, e := range m.Entries {
		if e.Type == "dir" && strings.HasSuffix(e.RelPath, "empty") {
			foundEmpty = true
		}
	}
	if !foundEmpty {
		t.Fatalf("empty dir missing, entries = %v", paths)
	}
}

func TestRunTreeBackupSkipsSymlinks(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "real.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.txt", filepath.Join(root, "link")); err != nil {
		t.Skip("symlink not supported")
	}
	snap := "aaaaaaaa-bbbb-4ccc-8ddd-000000000011"
	if _, err := runBackupPath(root, ident, backend, snap, nil); err != nil {
		t.Fatal(err)
	}
	m, err := readTreeManifest(backend, ident, snap)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range m.Entries {
		if e.RelPath == "link" {
			t.Fatal("symlink recorded")
		}
	}
}

func TestRunBackupPathDispatchesFile(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	filePath := writeTempFile(t, []byte("single"))
	snap := "aaaaaaaa-bbbb-4ccc-8ddd-000000000012"
	if _, err := runBackupPath(filePath, ident, backend, snap, nil); err != nil {
		t.Fatal(err)
	}
	m, err := readManifest(backend, ident, snap)
	if err != nil {
		t.Fatal(err)
	}
	if m.OriginalName != "payload.bin" {
		t.Fatalf("OriginalName = %q", m.OriginalName)
	}
}

func TestRunTreeBackupFollowsSymlinkedRoot(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	real := t.TempDir()
	if err := os.WriteFile(filepath.Join(real, "a.txt"), []byte("alpha"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlink not supported")
	}

	snap := "aaaaaaaa-bbbb-4ccc-8ddd-000000000013"
	res, err := runBackupPath(link, ident, backend, snap, nil)
	if err != nil {
		t.Fatalf("runBackupPath(symlink) error: %v", err)
	}
	if res.TotalChunks < 1 {
		t.Fatalf("TotalChunks = %d, want >= 1", res.TotalChunks)
	}
	m, err := readTreeManifest(backend, ident, snap)
	if err != nil {
		t.Fatal(err)
	}
	if m.FileCount() != 1 {
		t.Fatalf("FileCount = %d, want 1 (walk of a symlinked root must not be empty)", m.FileCount())
	}
	found := false
	for _, e := range m.Entries {
		if e.RelPath == "a.txt" && e.Type == "file" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a.txt missing from entries: %+v", m.Entries)
	}
}
