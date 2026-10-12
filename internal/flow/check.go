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
// It checks object presence without reading chunk contents. Missing referenced
// objects and repository or manifest errors fail the check; garbage alone does not.
func RunCheck(identityPath, remote string) error {
	backend := storage.NewRcloneBackend(remote)
	spinner, _ := pterm.DefaultSpinner.Start("Checking repository...")
	res, err := runCheck(identityPath, backend)
	if err != nil {
		_ = spinner.Stop()
		return err
	}
	spinner.Success("Repository check passed")
	pterm.Printf("Snapshots: %d\n", res.Snapshots)
	pterm.Printf("Referenced chunks: %d\n", res.Referenced)
	pterm.Printf("Data objects: %d\n", res.DataObjects)
	if len(res.Garbage) > 0 {
		pterm.Warning.Printf("Garbage objects: %d (run prune in a later phase)\n", len(res.Garbage))
	}
	return nil
}

// runCheck compares hashes referenced by snapshots or the index with data
// object names, returning sorted missing and garbage hashes. It does not verify
// chunk contents. Snapshot listing failures are treated as an empty listing;
// repository, index, manifest, and data listing errors are returned. Missing
// objects return an error with the report; garbage alone is not an error.
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
		// A fresh repo has no snapshots/ directory yet; only a confirmed
		// missing path may be treated as empty, other failures are real.
		if !storage.IsMissingPath(err) {
			return res, fmt.Errorf("failed to list snapshots: %w", err)
		}
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
		if !storage.IsMissingPath(err) {
			return res, fmt.Errorf("failed to list data objects: %w", err)
		}
		dataPaths = nil
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
