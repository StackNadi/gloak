package core

import "testing"

func TestFileCount(t *testing.T) {
	m := TreeManifest{Entries: []TreeEntry{
		{Type: "dir"},
		{Type: "file"},
		{Type: "file"},
	}}
	if got := m.FileCount(); got != 2 {
		t.Fatalf("FileCount() = %d, want 2", got)
	}
}
