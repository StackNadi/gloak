package flow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gloak/internal/core"
)

func TestNewUploadState(t *testing.T) {
	state := NewUploadState("backup-id", "/tmp/secret.tar", "secret.tar", 123, "remote:path", "age1recipient")

	if state.Version != "1.0" {
		t.Fatalf("Version = %q, want 1.0", state.Version)
	}
	if state.BackupID != "backup-id" || state.SourcePath != "/tmp/secret.tar" || state.OriginalName != "secret.tar" {
		t.Fatalf("state identity fields not preserved: %+v", state)
	}
	if state.OriginalSize != 123 || state.Remote != "remote:path" || state.Recipient != "age1recipient" {
		t.Fatalf("state backup fields not preserved: %+v", state)
	}
	if state.ChunkSize != core.ChunkSize {
		t.Fatalf("ChunkSize = %d, want %d", state.ChunkSize, core.ChunkSize)
	}
	if state.CreatedAt == "" || state.UpdatedAt == "" {
		t.Fatalf("timestamps should be set: %+v", state)
	}
	if _, err := time.Parse(time.RFC3339Nano, state.CreatedAt); err != nil {
		t.Fatalf("CreatedAt is not RFC3339Nano: %v", err)
	}
}

func TestUploadStateSaveLoadRoundTrip(t *testing.T) {
	stateDir := t.TempDir()
	state := NewUploadState("backup-id", "/tmp/secret.tar", "secret.tar", 123, "remote:path", "age1recipient")
	state.UploadedChunks = []core.ChunkMeta{{Index: 0, Name: "chunk_00000", Size: 10, SHA256: "abc"}}

	if err := saveUploadState(stateDir, state); err != nil {
		t.Fatalf("saveUploadState() returned error: %v", err)
	}

	path := uploadStatePath(stateDir, "backup-id")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("state file was not written: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("state file permissions = %v, want 0600", info.Mode().Perm())
	}

	got, err := loadUploadState(stateDir, "backup-id")
	if err != nil {
		t.Fatalf("loadUploadState() returned error: %v", err)
	}
	if got.BackupID != state.BackupID || got.SourcePath != state.SourcePath || got.Remote != state.Remote {
		t.Fatalf("loaded state identity fields = %+v, want %+v", got, state)
	}
	if len(got.UploadedChunks) != 1 || got.UploadedChunks[0] != state.UploadedChunks[0] {
		t.Fatalf("loaded chunks = %+v, want %+v", got.UploadedChunks, state.UploadedChunks)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, got.CreatedAt)
	if err != nil {
		t.Fatalf("loaded CreatedAt is not RFC3339Nano: %v", err)
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, got.UpdatedAt)
	if err != nil {
		t.Fatalf("loaded UpdatedAt is not RFC3339Nano: %v", err)
	}
	if updatedAt.Before(createdAt) {
		t.Fatalf("UpdatedAt %s is before CreatedAt %s", got.UpdatedAt, got.CreatedAt)
	}
}

func TestUploadStatePathUsesBackupID(t *testing.T) {
	stateDir := t.TempDir()
	got := uploadStatePath(stateDir, "backup-id")
	want := filepath.Join(stateDir, "backup-id.json")
	if got != want {
		t.Fatalf("uploadStatePath() = %q, want %q", got, want)
	}
}

func TestLoadUploadStateRejectsInvalidJSON(t *testing.T) {
	stateDir := t.TempDir()
	path := uploadStatePath(stateDir, "backup-id")
	if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatalf("failed to write invalid state: %v", err)
	}

	_, err := loadUploadState(stateDir, "backup-id")
	if err == nil {
		t.Fatalf("loadUploadState() succeeded with invalid JSON")
	}
	if !strings.Contains(err.Error(), "failed to parse upload state") {
		t.Fatalf("loadUploadState() error = %q, want parse error", err)
	}
}

func TestLoadUploadStateRejectsBackupIDMismatch(t *testing.T) {
	stateDir := t.TempDir()
	state := NewUploadState("other-id", "/tmp/secret.tar", "secret.tar", 123, "remote:path", "age1recipient")
	if err := saveUploadState(stateDir, state); err != nil {
		t.Fatalf("saveUploadState() returned error: %v", err)
	}

	if err := os.Rename(uploadStatePath(stateDir, "other-id"), uploadStatePath(stateDir, "backup-id")); err != nil {
		t.Fatalf("failed to rename state file: %v", err)
	}

	_, err := loadUploadState(stateDir, "backup-id")
	if err == nil {
		t.Fatalf("loadUploadState() succeeded with mismatched backup id")
	}
	if !strings.Contains(err.Error(), "upload state backup id mismatch") {
		t.Fatalf("loadUploadState() error = %q, want backup id mismatch", err)
	}
}

func TestDeleteUploadState(t *testing.T) {
	stateDir := t.TempDir()
	state := NewUploadState("backup-id", "/tmp/secret.tar", "secret.tar", 123, "remote:path", "age1recipient")
	if err := saveUploadState(stateDir, state); err != nil {
		t.Fatalf("saveUploadState() returned error: %v", err)
	}

	if err := deleteUploadState(stateDir, "backup-id"); err != nil {
		t.Fatalf("deleteUploadState() returned error: %v", err)
	}
	if _, err := os.Stat(uploadStatePath(stateDir, "backup-id")); !os.IsNotExist(err) {
		t.Fatalf("state file still exists or stat failed unexpectedly: %v", err)
	}
}
