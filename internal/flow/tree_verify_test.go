package flow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
