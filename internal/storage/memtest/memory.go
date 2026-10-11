// Package memtest provides an in-memory storage.Backend for tests.
package memtest

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
)

type MemoryBackend struct {
	mu      sync.Mutex
	files   map[string][]byte
	uploads map[string]int
}

func NewMemoryBackend() *MemoryBackend {
	return &MemoryBackend{files: make(map[string][]byte), uploads: make(map[string]int)}
}

func (m *MemoryBackend) Upload(remotePath string, in io.Reader) error {
	data, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[remotePath] = append([]byte(nil), data...)
	m.uploads[remotePath]++
	return nil
}

func (m *MemoryBackend) Download(remotePath string, out io.Writer) error {
	m.mu.Lock()
	data, ok := m.files[remotePath]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("not found: %s", remotePath)
	}
	_, err := out.Write(data)
	return err
}

// ListFiles mimics rclone lsjson: Path is relative to the listed prefix,
// only direct children are returned, directories are marked IsDir.
func (m *MemoryBackend) ListFiles(prefix []string) ([]byte, error) {
	p := strings.Trim(strings.Join(prefix, "/"), "/")
	m.mu.Lock()
	defer m.mu.Unlock()

	children := map[string]bool{}
	for key := range m.files {
		var rel string
		switch {
		case p == "":
			rel = key
		case strings.HasPrefix(key, p+"/"):
			rel = strings.TrimPrefix(key, p+"/")
		default:
			continue
		}
		if idx := strings.Index(rel, "/"); idx >= 0 {
			children[rel[:idx]] = true
		} else {
			children[rel] = false
		}
	}

	names := make([]string, 0, len(children))
	for name := range children {
		names = append(names, name)
	}
	sort.Strings(names)

	entries := make([]map[string]any, 0, len(names))
	for _, name := range names {
		entries = append(entries, map[string]any{
			"Path":  name,
			"Name":  name,
			"IsDir": children[name],
			"Size":  0,
		})
	}
	return json.Marshal(entries)
}

func (m *MemoryBackend) Delete(remotePath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, remotePath)
	return nil
}

func (m *MemoryBackend) Has(path string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.files[path]
	return ok
}

func (m *MemoryBackend) Put(path string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[path] = append([]byte(nil), data...)
}

func (m *MemoryBackend) Get(path string) []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]byte(nil), m.files[path]...)
}

func (m *MemoryBackend) UploadCount(path string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.uploads[path]
}

func (m *MemoryBackend) TotalUploads(prefix string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	total := 0
	for path, n := range m.uploads {
		if strings.HasPrefix(path, prefix) {
			total += n
		}
	}
	return total
}

func (m *MemoryBackend) KeysWithPrefix(prefix string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var keys []string
	for key := range m.files {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}
