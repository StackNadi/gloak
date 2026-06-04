package flow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gloak/internal/core"
)

func TestRunResumeValidationRejectsMissingState(t *testing.T) {
	_, err := runResumeValidation("7f91c6c7-7a0b-44aa-ae23-997b60e4e998", t.TempDir())
	if err == nil {
		t.Fatalf("runResumeValidation() succeeded with missing state")
	}
	if !strings.Contains(err.Error(), "failed to read upload state") {
		t.Fatalf("runResumeValidation() error = %q, want missing state error", err)
	}
}

func TestRunResumeValidationRejectsMissingSourceFile(t *testing.T) {
	stateDir := t.TempDir()
	backupID := "7f91c6c7-7a0b-44aa-ae23-997b60e4e998"
	state := NewUploadState(backupID, filepath.Join(t.TempDir(), "missing.txt"), "missing.txt", 7, "memory:backups", "age1recipient")
	if err := saveUploadState(stateDir, state); err != nil {
		t.Fatalf("saveUploadState() returned error: %v", err)
	}

	_, err := runResumeValidation(backupID, stateDir)
	if err == nil {
		t.Fatalf("runResumeValidation() succeeded with missing source file")
	}
	if !strings.Contains(err.Error(), "failed to stat source file from upload state") {
		t.Fatalf("runResumeValidation() error = %q, want source stat error", err)
	}
}

func TestRunResumeValidationRejectsChangedSourceSize(t *testing.T) {
	stateDir := t.TempDir()
	backupID := "7f91c6c7-7a0b-44aa-ae23-997b60e4e998"
	sourcePath := writeResumeSource(t, []byte("current payload"))
	state := NewUploadState(backupID, sourcePath, filepath.Base(sourcePath), 5, "memory:backups", "age1recipient")
	if err := saveUploadState(stateDir, state); err != nil {
		t.Fatalf("saveUploadState() returned error: %v", err)
	}

	_, err := runResumeValidation(backupID, stateDir)
	if err == nil {
		t.Fatalf("runResumeValidation() succeeded with changed source size")
	}
	if !strings.Contains(err.Error(), "source file size changed") {
		t.Fatalf("runResumeValidation() error = %q, want source size error", err)
	}
}

func TestRunResumeValidationRejectsEmptyRecipient(t *testing.T) {
	stateDir := t.TempDir()
	backupID := "7f91c6c7-7a0b-44aa-ae23-997b60e4e998"
	sourcePath := writeResumeSource(t, []byte("payload"))
	state := NewUploadState(backupID, sourcePath, filepath.Base(sourcePath), 7, "memory:backups", " ")
	if err := saveUploadState(stateDir, state); err != nil {
		t.Fatalf("saveUploadState() returned error: %v", err)
	}

	_, err := runResumeValidation(backupID, stateDir)
	if err == nil {
		t.Fatalf("runResumeValidation() succeeded with empty recipient")
	}
	if !strings.Contains(err.Error(), "upload state recipient is empty") {
		t.Fatalf("runResumeValidation() error = %q, want empty recipient error", err)
	}
}

func TestRunResumeValidationAcceptsValidState(t *testing.T) {
	stateDir := t.TempDir()
	backupID := "7f91c6c7-7a0b-44aa-ae23-997b60e4e998"
	sourcePath := writeResumeSource(t, []byte("payload"))
	state := NewUploadState(backupID, sourcePath, filepath.Base(sourcePath), 7, "memory:backups", "age1recipient")
	state.UploadedChunks = []core.ChunkMeta{{Index: 0, Name: "chunk_00000", PlainSize: 7, PlainSHA256: hashBytes([]byte("payload")), EncryptedSize: 12, EncryptedSHA256: "encrypted"}}
	if err := saveUploadState(stateDir, state); err != nil {
		t.Fatalf("saveUploadState() returned error: %v", err)
	}

	got, err := runResumeValidation(backupID, stateDir)
	if err != nil {
		t.Fatalf("runResumeValidation() returned error: %v", err)
	}
	if got.BackupID != backupID || got.SourcePath != sourcePath || got.Remote != "memory:backups" {
		t.Fatalf("validated state = %+v, want backup/source/remote preserved", got)
	}
	if len(got.UploadedChunks) != 1 || got.UploadedChunks[0].Name != "chunk_00000" {
		t.Fatalf("validated uploaded chunks = %+v, want chunk_00000", got.UploadedChunks)
	}
}

func writeResumeSource(t *testing.T, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.txt")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}
	return path
}
