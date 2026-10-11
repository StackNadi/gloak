package flow

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/pterm/pterm"

	"gloak/internal/core"
	"gloak/internal/index"
	"gloak/internal/repo"
	"gloak/internal/storage"
)

// CheckResult is the structural consistency report for a repository.
type CheckResult struct {
	Snapshots   int
	Referenced  int
	DataObjects int
	Missing     []string
	Garbage     []string
}

// RunCheck opens the repository and prints a structural consistency report.
func RunCheck(identityPath, remote string) error {
	backend := storage.NewRcloneBackend(remote)
	spinner, _ := pterm.DefaultSpinner.Start("Checking repository...")
	res, err := runCheck(identityPath, backend)
	if err != nil {
		_ = spinner.Stop()
		return err
	}
	_ = spinner.Stop()
	pterm.Success.Println("Repository check passed")
	pterm.Printf("Snapshots: %d\n", res.Snapshots)
	pterm.Printf("Referenced chunks: %d\n", res.Referenced)
	pterm.Printf("Data objects: %d\n", res.DataObjects)
	if len(res.Garbage) > 0 {
		pterm.Warning.Printf("Garbage objects: %d (run prune in a later phase)\n", len(res.Garbage))
	}
	return nil
}

func runCheck(identityPath string, backend storage.Backend) (CheckResult, error) {
	var res CheckResult

	if _, err := repo.Open(backend, identityPath); err != nil {
		return res, err
	}
	ix, err := index.Load(backend, identityPath)
	if err != nil {
		return res, fmt.Errorf("failed to load index: %w", err)
	}

	snapRaw, err := backend.ListFiles([]string{repo.SnapshotsPrefix})
	if err != nil {
		// A fresh repo has no snapshots/ directory yet; treat as empty.
		snapRaw = []byte("[]")
	}
	snapEntries, err := storage.ParseList(snapRaw)
	if err != nil {
		return res, fmt.Errorf("failed to parse snapshot listing: %w", err)
	}

	referenced := make(map[string]bool)
	for _, entry := range snapEntries {
		if entry.IsDir || !strings.HasSuffix(entry.Path, ".age") {
			continue
		}
		id := strings.TrimSuffix(path.Base(entry.Path), ".age")
		kind, fileM, treeM, err := readAnyManifest(backend, identityPath, id)
		if err != nil {
			return res, fmt.Errorf("snapshot %s: %w", id, err)
		}
		res.Snapshots++
		if kind == core.KindTree {
			for _, te := range treeM.Entries {
				for _, meta := range te.Chunks {
					referenced[meta.PlainSHA256] = true
				}
			}
		} else {
			for _, meta := range fileM.Chunks {
				referenced[meta.PlainSHA256] = true
			}
		}
	}
	for _, h := range ix.AllHashes() {
		referenced[h] = true
	}
	res.Referenced = len(referenced)

	dataPaths, err := backend.ListRecursive([]string{repo.DataPrefix})
	if err != nil {
		return res, fmt.Errorf("failed to list data objects: %w", err)
	}
	present := make(map[string]bool, len(dataPaths))
	for _, p := range dataPaths {
		h := path.Base(p)
		if len(h) != 64 {
			continue
		}
		present[h] = true
	}
	res.DataObjects = len(present)

	for h := range referenced {
		if !present[h] {
			res.Missing = append(res.Missing, h)
		}
	}
	sort.Strings(res.Missing)
	for h := range present {
		if !referenced[h] {
			res.Garbage = append(res.Garbage, h)
		}
	}
	sort.Strings(res.Garbage)

	if len(res.Missing) > 0 {
		return res, fmt.Errorf("check failed: %d referenced chunk(s) missing from storage", len(res.Missing))
	}
	return res, nil
}
