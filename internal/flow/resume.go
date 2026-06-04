package flow

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/pterm/pterm"

	"gloak/internal/core"
	"gloak/internal/storage"
)

func RunResumeValidation(backupID string) (UploadState, error) {
	return runResumeValidation(backupID, defaultUploadStateDir())
}

func RunResume(backupID string) error {
	return runResume(backupID, func(remote string) storage.Backend {
		return storage.NewRcloneBackend(remote)
	}, defaultUploadStateDir())
}

func runResume(backupID string, newBackend func(string) storage.Backend, stateDir string) error {
	state, err := runResumeValidation(backupID, stateDir)
	if err != nil {
		return err
	}

	file, err := os.Open(state.SourcePath)
	if err != nil {
		return fmt.Errorf("failed to open source file from upload state: %w", err)
	}
	defer file.Close()

	pterm.Success.Printf("Found upload state: %s\n", state.BackupID)
	pterm.DefaultBasicText.Printf("Source file     : %s\n", state.SourcePath)
	pterm.DefaultBasicText.Printf("Destination     : %s\n", state.Remote)
	pterm.DefaultBasicText.Printf("Uploaded chunks : %d\n", len(state.UploadedChunks))

	backend := newBackend(state.Remote)
	p, _ := pterm.DefaultProgressbar.
		WithTotal(int(state.OriginalSize)).
		WithTitle("Resuming upload...").
		WithRemoveWhenDone(false).
		Start()

	chunksMeta, payloadHash, err := uploadPlaintextChunks(file, state.Recipient, backend, backupID, &state, stateDir, state.ChunkSize, func(chunk core.ChunkMeta) {
		pterm.Info.Printf("Skipping %s\n", chunk.Name)
	}, func(n int) {
		p.Add(n)
	})
	if err != nil {
		p.Stop()
		return fmt.Errorf("resume upload failed: %w", err)
	}
	p.Stop()

	pterm.Info.Println("Building and encrypting manifest...")
	manifest := core.CreateManifest(state.BackupID, state.OriginalName, state.OriginalSize, state.Recipient, payloadHash, chunksMeta)
	encryptedManifestBytes, err := core.EncryptManifest(state.Recipient, manifest)
	if err != nil {
		return fmt.Errorf("failed to encrypt manifest: %w", err)
	}
	if err := backend.Upload(fmt.Sprintf("%s/manifest.age", state.BackupID), bytes.NewReader(encryptedManifestBytes)); err != nil {
		return fmt.Errorf("failed to upload manifest.age: %w", err)
	}

	locator := core.CreateLocator(state.BackupID)
	locatorBytes, _ := core.SerializeLocator(locator)
	if err := backend.Upload(fmt.Sprintf("%s/locator.json", state.BackupID), bytes.NewReader(locatorBytes)); err != nil {
		return fmt.Errorf("failed to upload locator.json: %w", err)
	}
	if err := deleteUploadState(stateDir, state.BackupID); err != nil {
		return fmt.Errorf("resume completed but failed to delete upload state: %w", err)
	}

	pterm.Success.Println("Resume completed successfully!")
	return nil
}

func runResumeValidation(backupID string, stateDir string) (UploadState, error) {
	state, err := loadUploadState(stateDir, backupID)
	if err != nil {
		return UploadState{}, err
	}

	info, err := os.Stat(state.SourcePath)
	if err != nil {
		return UploadState{}, fmt.Errorf("failed to stat source file from upload state: %w", err)
	}
	if info.IsDir() {
		return UploadState{}, fmt.Errorf("source path from upload state is a directory: %s", state.SourcePath)
	}
	if info.Size() != state.OriginalSize {
		return UploadState{}, fmt.Errorf("source file size changed: got %d, want %d", info.Size(), state.OriginalSize)
	}
	if strings.TrimSpace(state.Recipient) == "" {
		return UploadState{}, fmt.Errorf("upload state recipient is empty")
	}
	if state.Remote == "" {
		return UploadState{}, fmt.Errorf("upload state remote is empty")
	}
	if state.ChunkSize <= 0 {
		return UploadState{}, fmt.Errorf("upload state chunk size must be positive")
	}

	return state, nil
}
