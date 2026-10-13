package flow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gloak/internal/core"
)

func TestRunVerifyTreeSnapshot(t *testing.T) {
	_, backend, ident, snap := treeFixture(t)
	if err := runVerify(snap, ident, backend); err != nil {
		t.Fatalf("runVerify() error: %v", err)
	}
}

func TestRunVerifyTreeCorruptChunk(t *testing.T) {
	_, backend, ident, snap := treeFixture(t)
	keys := backend.KeysWithPrefix("data/")
	if len(keys) == 0 {
		t.Fatal("no data")
	}
	blob := backend.Get(keys[0])
	blob[len(blob)/2] ^= 0xff
	backend.Put(keys[0], blob)
	err := runVerify(snap, ident, backend)
	if err == nil {
		t.Fatal("runVerify() succeeded on corrupt chunk")
	}
	if !strings.Contains(err.Error(), "CHUNK CORRUPT") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunVerifyTreeRejectsUnknownEntryType(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	m := core.TreeManifest{
		Version:   "2.0",
		UUID:      "aaaaaaaa-bbbb-4ccc-8ddd-000000000040",
		Kind:      core.KindTree,
		CreatedAt: "2026-10-12T00:00:00Z",
		Hostname:  "test",
		Paths:     []string{"/tmp/x"},
		Entries: []core.TreeEntry{
			{RelPath: "weird", Type: "socket", Mode: 0644, Mtime: "2026-10-12T00:00:00Z"},
		},
	}
	seedTreeSnapshot(t, backend, ident, m)

	err := runVerify(m.UUID, ident, backend)
	if err == nil || !strings.Contains(err.Error(), "unknown tree entry type") {
		t.Fatalf("err = %v, want unknown entry type rejection", err)
	}
}

func TestRunVerifyTreeRejectsUnsafeDirPath(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	m := core.TreeManifest{
		Version:   "2.0",
		UUID:      "aaaaaaaa-bbbb-4ccc-8ddd-000000000041",
		Kind:      core.KindTree,
		CreatedAt: "2026-10-12T00:00:00Z",
		Hostname:  "test",
		Paths:     []string{"/tmp/x"},
		Entries: []core.TreeEntry{
			{RelPath: "../escape", Type: "dir", Mode: 0755, Mtime: "2026-10-12T00:00:00Z"},
		},
	}
	seedTreeSnapshot(t, backend, ident, m)

	err := runVerify(m.UUID, ident, backend)
	if err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("err = %v, want unsafe path rejection", err)
	}
}

func TestRunCheckWithTreeAndFileSnapshots(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	filePath := writeTempFile(t, []byte("single-file-payload"))
	if _, err := runBackup(filePath, ident, backend, "7f91c6c7-7a0b-44aa-ae23-997b60e4e999", nil); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "t.txt"), []byte("tree-payload"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := runBackupPath(root, ident, backend, "8f91c6c7-7a0b-44aa-ae23-997b60e4e999", nil); err != nil {
		t.Fatal(err)
	}
	res, err := runCheck(ident, backend)
	if err != nil {
		t.Fatalf("runCheck() error: %v", err)
	}
	if res.Snapshots != 2 {
		t.Fatalf("Snapshots = %d, want 2", res.Snapshots)
	}
	if len(res.Missing) != 0 {
		t.Fatalf("Missing = %v", res.Missing)
	}
}
