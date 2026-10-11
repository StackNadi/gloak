package flow

import (
	"github.com/pterm/pterm"

	"gloak/internal/core"
	"gloak/internal/repo"
	"gloak/internal/storage"
)

const defaultChunkSize = core.ChunkSize

// RunInit creates a new gloak repository on remote and prints the outcome.
// With force it overwrites an existing repository, which makes old snapshots
// unreadable.
func RunInit(recipients []string, remote string, force bool) error {
	backend := storage.NewRcloneBackend(remote)
	spinner, _ := pterm.DefaultSpinner.Start("Initializing repository...")

	master, cfg, err := runInit(recipients, backend, defaultChunkSize, force)
	if err != nil {
		_ = spinner.Stop()
		return err
	}
	_ = spinner.Stop()
	pterm.Success.Println("Repository initialized")
	pterm.Printf("Location: %s\n", remote)
	pterm.Printf("Recipients: %v\n", cfg.Recipients)
	pterm.Printf("Chunk size: %d bytes\n", cfg.ChunkSize)
	pterm.Printf("Master key length: %d bytes (wrapped in %s)\n", len(master), repo.KeysPrefix+"/")
	return nil
}

func runInit(recipients []string, backend storage.Backend, chunkSize int64, force bool) ([]byte, repo.Config, error) {
	return repo.Init(backend, recipients, chunkSize, force)
}
