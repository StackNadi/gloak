package memtest

import (
	"bytes"
	"testing"
)

func TestListRecursiveReturnsNestedFiles(t *testing.T) {
	m := NewMemoryBackend()
	_ = m.Upload("data/ab/abhash", bytes.NewReader([]byte("x")))
	_ = m.Upload("data/cd/cdhash", bytes.NewReader([]byte("y")))
	_ = m.Upload("snapshots/s1.age", bytes.NewReader([]byte("z")))

	got, err := m.ListRecursive([]string{"data"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"data/ab/abhash", "data/cd/cdhash"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
