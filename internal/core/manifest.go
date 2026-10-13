package core

import (
	"github.com/google/uuid"
)

const (
	KindFile = "file"
	KindTree = "tree"
)

type Manifest struct {
	Version       string      `json:"version"`
	UUID          string      `json:"uuid"`
	OriginalName  string      `json:"original_name"`
	OriginalSize  int64       `json:"original_size"`
	CreatedAt     string      `json:"created_at"`
	Recipient     string      `json:"recipient"`
	PayloadSHA256 string      `json:"payload_sha256"`
	Chunks        []ChunkMeta `json:"chunks"`
}

type TreeEntry struct {
	RelPath string      `json:"rel_path"`
	Type    string      `json:"type"`
	Mode    uint32      `json:"mode"`
	Mtime   string      `json:"mtime"`
	Size    int64       `json:"size"`
	Chunks  []ChunkMeta `json:"chunks"`
}

type TreeManifest struct {
	Version   string      `json:"version"`
	UUID      string      `json:"uuid"`
	Kind      string      `json:"kind"`
	CreatedAt string      `json:"created_at"`
	Hostname  string      `json:"hostname"`
	Paths     []string    `json:"paths"`
	Entries   []TreeEntry `json:"entries"`
}

// FileCount counts entries whose type is "file", including empty files.
func (m *TreeManifest) FileCount() int {
	n := 0
	for _, e := range m.Entries {
		if e.Type == "file" {
			n++
		}
	}
	return n
}

func GenerateUUID() string {
	return uuid.New().String()
}
