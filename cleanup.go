package main

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/pterm/pterm"

	"gloak/internal/storage"
)

type CleanupCmd struct {
	Remote string `help:"Rclone remote URI (e.g., keraaaki:backup_folder)." required:"" short:"R"`
	Yes    bool   `help:"Skip confirmation prompt." short:"y"`
}

type lsjsonEntry struct {
	Path    string `json:"Path"`
	Name    string `json:"Name"`
	IsDir   bool   `json:"IsDir"`
	Size    int64  `json:"Size"`
	ModTime string `json:"ModTime"`
}

func (c *CleanupCmd) Run() error {
	pterm.Info.Println("Scanning remote for backup directories...")

	backend := storage.NewRcloneBackend(c.Remote)

	raw, err := backend.ListFiles(nil)
	if err != nil {
		return fmt.Errorf("failed to list remote: %w", err)
	}

	var entries []lsjsonEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return fmt.Errorf("failed to parse lsjson output: %w", err)
	}

	// Collect top-level directories (potential backup folders)
	var dirs []lsjsonEntry
	for _, e := range entries {
		if e.IsDir {
			dirs = append(dirs, e)
		}
	}

	if len(dirs) == 0 {
		pterm.Success.Println("No backup directories found. Nothing to clean up.")
		return nil
	}

	pterm.Info.Printf("Found %d backup directories. Checking for manifest.age...\n", len(dirs))

	// Identify orphan directories (missing manifest.age)
	var orphans []string
	for _, d := range dirs {
		filesInDir, err := backend.ListFiles([]string{d.Path})
		if err != nil {
			pterm.Warning.Printf("  Could not list %s: %v — skipping\n", d.Path, err)
			continue
		}

		var innerEntries []lsjsonEntry
		if err := json.Unmarshal(filesInDir, &innerEntries); err != nil {
			pterm.Warning.Printf("  Could not parse listing for %s: %v — skipping\n", d.Path, err)
			continue
		}

		hasManifest := false
		for _, ie := range innerEntries {
			if !ie.IsDir && ie.Name == "manifest.age" {
				hasManifest = true
				break
			}
		}

		if !hasManifest {
			orphans = append(orphans, d.Path)
		}
	}

	if len(orphans) == 0 {
		pterm.Success.Println("All backup directories have a manifest.age. Nothing to clean up.")
		return nil
	}

	sort.Strings(orphans)

	pterm.Warning.Printf("Found %d orphan directorie(s) missing manifest.age:\n", len(orphans))
	for _, o := range orphans {
		pterm.DefaultBasicText.Printf("  - %s\n", o)
	}

	if !c.Yes {
		fmt.Println()
		result, _ := pterm.DefaultInteractiveConfirm.
			WithDefaultText("Delete these directories?").
			Show()
		if !result {
			pterm.Info.Println("Aborted. No directories deleted.")
			return nil
		}
	}

	return batchDelete(backend, orphans)
}

func batchDelete(backend *storage.RcloneBackend, orphans []string) error {
	// Batch delete in chunks of up to 500
	const batchSize = 500
	deleted := 0
	var deleteErrors []string

	for i := 0; i < len(orphans); i += batchSize {
		end := i + batchSize
		if end > len(orphans) {
			end = len(orphans)
		}
		batch := orphans[i:end]

		for _, path := range batch {
			if err := backend.Delete(path); err != nil {
				deleteErrors = append(deleteErrors, fmt.Sprintf("%s: %v", path, err))
				continue
			}
			deleted++
			pterm.Success.Printf("  Deleted: %s\n", path)
		}
	}

	fmt.Println()
	pterm.Success.Printf("Cleanup complete: %d of %d orphan directorie(s) deleted.\n", deleted, len(orphans))

	if len(deleteErrors) > 0 {
		pterm.Warning.Printf("%d deletion(s) failed:\n", len(deleteErrors))
		for _, de := range deleteErrors {
			pterm.Error.Printf("  %s\n", de)
		}
		return fmt.Errorf("%d deletion(s) failed", len(deleteErrors))
	}

	return nil
}
