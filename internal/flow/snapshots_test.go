package flow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunSnapshotsListsBothKinds(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	filePath := writeTempFile(t, []byte("single"))
	if _, err := runBackup(filePath, ident, backend, "7f91c6c7-7a0b-44aa-ae23-997b60e4e998", nil); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "t.txt"), []byte("tree"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := runBackupPath(root, ident, backend, "8f91c6c7-7a0b-44aa-ae23-997b60e4e999", nil); err != nil {
		t.Fatal(err)
	}
	infos, err := runSnapshots(ident, backend)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 2 {
		t.Fatalf("len = %d, want 2", len(infos))
	}
	kinds := map[string]bool{}
	for _, info := range infos {
		kinds[info.Kind] = true
	}
	if !kinds["file"] || !kinds["tree"] {
		t.Fatalf("kinds = %v", kinds)
	}
}

func TestRunSnapshotsEmpty(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	infos, err := runSnapshots(ident, backend)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 0 {
		t.Fatalf("len = %d", len(infos))
	}
}
