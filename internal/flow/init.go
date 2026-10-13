package flow

import (
	"github.com/pterm/pterm"

	"gloak/internal/repo"
	"gloak/internal/storage"
)

// RunInit creates a new gloak repository on remote and prints the outcome.
// Zero min/avg/max select the package defaults; sizes are in bytes. With force
// it overwrites an existing repository, which makes old snapshots unreadable.
// Validation, encryption, and storage errors from repo.Init are returned.
func RunInit(recipients []string, remote string, min, avg, max int64, force bool) error {
	if min == 0 {
		min = repo.DefaultChunkMin
	}
	if avg == 0 {
		avg = repo.DefaultChunkAvg
	}
	if max == 0 {
		max = repo.DefaultChunkMax
	}
	backend := storage.NewRcloneBackend(remote)
	spinner, _ := pterm.DefaultSpinner.Start("Initializing repository...")

	master, cfg, err := runInit(recipients, backend, min, avg, max, force)
	if err != nil {
		_ = spinner.Stop()
		return err
	}
	spinner.Success("Repository initialized")
	pterm.Printf("Location: %s\n", remote)
	pterm.Printf("Recipients: %v\n", cfg.Recipients)
	pterm.Printf("Chunk bounds: min=%d avg=%d max=%d bytes\n", cfg.ChunkMin, cfg.ChunkAvg, cfg.ChunkMax)
	pterm.Printf("Master key length: %d bytes (wrapped in %s)\n", len(master), repo.KeysPrefix+"/")
	return nil
}

// runInit delegates to repo.Init and returns its master key, config, and error.
// Chunk bounds are byte counts and are passed through without defaults.
func runInit(recipients []string, backend storage.Backend, min, avg, max int64, force bool) ([]byte, repo.Config, error) {
	return repo.Init(backend, recipients, min, avg, max, force)
}
