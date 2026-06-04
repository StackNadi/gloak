package flow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gloak/internal/core"
)

const uploadStateVersion = "1.0"

type UploadState struct {
	Version        string           `json:"version"`
	BackupID       string           `json:"backup_id"`
	SourcePath     string           `json:"source_path"`
	OriginalName   string           `json:"original_name"`
	OriginalSize   int64            `json:"original_size"`
	Remote         string           `json:"remote"`
	Recipient      string           `json:"recipient"`
	ChunkSize      int64            `json:"chunk_size"`
	UploadedChunks []core.ChunkMeta `json:"uploaded_chunks"`
	CreatedAt      string           `json:"created_at"`
	UpdatedAt      string           `json:"updated_at"`
}

func NewUploadState(backupID string, sourcePath string, originalName string, originalSize int64, remote string, recipient string) UploadState {
	return newUploadStateWithChunkSize(backupID, sourcePath, originalName, originalSize, remote, recipient, core.ChunkSize)
}

func newUploadStateWithChunkSize(backupID string, sourcePath string, originalName string, originalSize int64, remote string, recipient string, chunkSize int64) UploadState {
	now := uploadStateTimestamp()
	return UploadState{
		Version:      uploadStateVersion,
		BackupID:     backupID,
		SourcePath:   sourcePath,
		OriginalName: originalName,
		OriginalSize: originalSize,
		Remote:       remote,
		Recipient:    recipient,
		ChunkSize:    chunkSize,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func uploadStatePath(stateDir string, backupID string) string {
	return filepath.Join(stateDir, backupID+".json")
}

func defaultUploadStateDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".gloak", "uploads")
}

func saveUploadState(stateDir string, state UploadState) error {
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return fmt.Errorf("failed to create upload state directory: %w", err)
	}

	state.UpdatedAt = uploadStateTimestamp()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode upload state: %w", err)
	}

	path := uploadStatePath(stateDir, state.BackupID)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write upload state: %w", err)
	}

	return nil
}

func loadUploadState(stateDir string, backupID string) (UploadState, error) {
	path := uploadStatePath(stateDir, backupID)
	data, err := os.ReadFile(path)
	if err != nil {
		return UploadState{}, fmt.Errorf("failed to read upload state: %w", err)
	}

	var state UploadState
	if err := json.Unmarshal(data, &state); err != nil {
		return UploadState{}, fmt.Errorf("failed to parse upload state: %w", err)
	}
	if state.BackupID != backupID {
		return UploadState{}, fmt.Errorf("upload state backup id mismatch: got %s, want %s", state.BackupID, backupID)
	}

	return state, nil
}

func deleteUploadState(stateDir string, backupID string) error {
	path := uploadStatePath(stateDir, backupID)
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("failed to delete upload state: %w", err)
	}

	return nil
}

func uploadStateTimestamp() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
