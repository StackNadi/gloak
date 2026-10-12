package flow

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"gloak/internal/core"
	"gloak/internal/index"
	"gloak/internal/repo"
	"gloak/internal/storage"
)

// PruneResult summarizes what prune would do or did.
type PruneResult struct {
	KeptSnapshots    []string
	DeadSnapshots    []string
	LiveChunks       int
	GarbageChunks    int
	DeletedSnapshots int
	DeletedObjects   int
	CompactedEpochs  bool
}

// RunPrune opens the repository and runs the prune flow. Without apply,
// nothing is deleted; the result describes the plan only.
func RunPrune(identityPath, remote string, policy RetentionPolicy, apply bool) (PruneResult, error) {
	backend := storage.NewRcloneBackend(remote)
	return runPrune(identityPath, backend, policy, apply)
}

func runPrune(identityPath string, backend storage.Backend, policy RetentionPolicy, apply bool) (PruneResult, error) {
	var res PruneResult

	if policy.KeepLast < 0 || policy.KeepDaily < 0 || policy.KeepWeekly < 0 || policy.KeepMonthly < 0 {
		return res, fmt.Errorf("retention counts must be non-negative")
	}
	if policy.isEmpty() {
		return res, fmt.Errorf("refusing to prune with no retention rules (pass at least one --keep-* flag)")
	}

	if _, err := repo.Open(backend, identityPath); err != nil {
		return res, err
	}
	if apply {
		// Advisory exclusive lock: no backup (shared) may run while
		// prune deletes, and no other prune may overlap.
		release, err := acquireRepoLock(backend, "prune", false)
		if err != nil {
			return res, err
		}
		defer release()
	}
	ix, err := index.Load(backend, identityPath)
	if err != nil {
		return res, fmt.Errorf("failed to load index: %w", err)
	}

	// List snapshots.
	snapRaw, err := backend.ListFiles([]string{repo.SnapshotsPrefix})
	if err != nil {
		if !storage.IsMissingPath(err) {
			return res, fmt.Errorf("failed to list snapshots: %w", err)
		}
		// A fresh repo has no snapshots/ directory yet.
		snapRaw = []byte("[]")
	}
	snapEntries, err := storage.ParseList(snapRaw)
	if err != nil {
		return res, fmt.Errorf("failed to parse snapshot listing: %w", err)
	}

	var snaps []snapshotMeta
	for _, entry := range snapEntries {
		if entry.IsDir || !strings.HasSuffix(entry.Path, ".age") {
			continue
		}
		id := strings.TrimSuffix(path.Base(entry.Path), ".age")
		if _, err := uuid.Parse(id); err != nil {
			continue
		}
		kind, fileM, treeM, err := readAnyManifest(backend, identityPath, id)
		if err != nil {
			// Undecryptable snapshot is an error; do not prune around it.
			return res, fmt.Errorf("snapshot %s: %w", id, err)
		}
		var created time.Time
		if kind == core.KindTree {
			created, _ = time.Parse(time.RFC3339, treeM.CreatedAt)
		} else {
			created, _ = time.Parse(time.RFC3339, fileM.CreatedAt)
		}
		snaps = append(snaps, snapshotMeta{UUID: id, CreatedAt: created})
	}

	keep := selectKeepSnapshots(snaps, policy)
	for _, s := range snaps {
		if keep[s.UUID] {
			res.KeptSnapshots = append(res.KeptSnapshots, s.UUID)
		} else {
			res.DeadSnapshots = append(res.DeadSnapshots, s.UUID)
		}
	}
	sort.Strings(res.KeptSnapshots)
	sort.Strings(res.DeadSnapshots)

	// Mark live chunks from kept snapshots.
	live := make(map[string]bool)
	for _, s := range snaps {
		if !keep[s.UUID] {
			continue
		}
		kind, fileM, treeM, err := readAnyManifest(backend, identityPath, s.UUID)
		if err != nil {
			return res, fmt.Errorf("snapshot %s: %w", s.UUID, err)
		}
		if kind == core.KindTree {
			for _, te := range treeM.Entries {
				for _, meta := range te.Chunks {
					live[meta.PlainSHA256] = true
				}
			}
		} else {
			for _, meta := range fileM.Chunks {
				live[meta.PlainSHA256] = true
			}
		}
	}
	res.LiveChunks = len(live)

	// Garbage = index entries not live, plus data objects whose hash is
	// not live.
	allHashes := ix.AllHashes()
	var garbage []string
	for _, h := range allHashes {
		if !live[h] {
			garbage = append(garbage, h)
		}
	}
	// Also scan data/ for objects not in the live set (orphans).
	dataPaths, err := backend.ListRecursive([]string{repo.DataPrefix})
	if err != nil {
		// A fresh repo has no data/ directory yet; anything else is a
		// real listing failure and must not be pruned around.
		if !storage.IsMissingPath(err) {
			return res, fmt.Errorf("failed to list data objects: %w", err)
		}
		dataPaths = nil
	}
	seenGarbage := make(map[string]bool, len(garbage))
	for _, h := range garbage {
		seenGarbage[h] = true
	}
	for _, p := range dataPaths {
		h := path.Base(p)
		if len(h) != 64 || live[h] || seenGarbage[h] {
			continue
		}
		garbage = append(garbage, h)
		seenGarbage[h] = true
	}
	sort.Strings(garbage)
	res.GarbageChunks = len(garbage)

	if !apply {
		return res, nil
	}

	// Compact the index first: the new epoch drops hashes no kept
	// snapshot references. If this fails, nothing has been deleted yet.
	if err := compactIndex(backend, identityPath, ix, live); err != nil {
		return res, err
	}
	res.CompactedEpochs = true

	// Delete dead snapshots. Their exclusive chunks are already gone from
	// the index but the data objects are still present, so an interrupted
	// prune leaves the repository consistent.
	for _, id := range res.DeadSnapshots {
		sp, err := repo.SnapshotPath(id)
		if err != nil {
			return res, err
		}
		if err := backend.Delete(sp); err != nil {
			return res, fmt.Errorf("failed to delete snapshot %s: %w", id, err)
		}
		res.DeletedSnapshots++
	}

	// Delete garbage data objects last, once nothing references them.
	for _, h := range garbage {
		dp := repo.DataPath(h)
		if err := backend.Delete(dp); err != nil {
			return res, fmt.Errorf("failed to delete data object %s: %w", h, err)
		}
		res.DeletedObjects++
	}

	return res, nil
}

// compactIndex rewrites the index as a single new epoch containing only the
// live hashes, then deletes prior epochs.
func compactIndex(backend storage.Backend, identityPath string, ix *index.Index, live map[string]bool) error {
	r, err := repo.Open(backend, identityPath)
	if err != nil {
		return err
	}

	// List current epochs before compacting so we can delete the old ones.
	oldRaw, err := backend.ListFiles([]string{"index"})
	if err != nil {
		if !storage.IsMissingPath(err) {
			return fmt.Errorf("failed to list index epochs: %w", err)
		}
		oldRaw = []byte("[]")
	}
	oldEntries, err := storage.ParseList(oldRaw)
	if err != nil {
		return err
	}
	var oldEpochs []string
	for _, e := range oldEntries {
		if !e.IsDir && strings.HasSuffix(e.Path, ".age") {
			oldEpochs = append(oldEpochs, e.Path)
		}
	}

	// Write one new epoch with live entries only. The epoch counter
	// continues past existing names so the compacted file is new.
	if _, err := ix.Compact(backend, r.Config.Recipients, live); err != nil {
		return fmt.Errorf("failed to write compacted index: %w", err)
	}

	// Delete every epoch that existed before compaction. A failure here
	// leaves the old epochs in place, which Load merges harmlessly.
	for _, name := range oldEpochs {
		if err := backend.Delete("index/" + name); err != nil {
			return fmt.Errorf("failed to delete old index epoch %s: %w", name, err)
		}
	}
	return nil
}
