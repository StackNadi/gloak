package core

import (
	"github.com/google/uuid"
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

func GenerateUUID() string {
	return uuid.New().String()
}
