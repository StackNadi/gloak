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
// nothing is deleted; the result describes the plan only. It returns an error
// for an all-zero policy or a failed repository operation. An apply error may
// follow partial deletions, reflected in the returned counts.
func RunPrune(identityPath, remote string, policy RetentionPolicy, apply bool) (PruneResult, error) {
	backend := storage.NewRcloneBackend(remote)
	return runPrune(identityPath, backend, policy, apply)
}

// runPrune plans retention and garbage collection, deleting dead snapshots and
// unreferenced chunks and compacting the index only when apply is true.
// An all-zero policy or unreadable snapshot returns an error. Data listing
// failures are treated as no additional objects; invalid snapshot timestamps
// use the zero time. Other repository, manifest, and deletion errors return
// the results accumulated so far, without undoing earlier deletions.
func runPrune(identityPath string, backend storage.Backend, policy RetentionPolicy, apply bool) (PruneResult, error) {
	var res PruneResult

	if policy.isEmpty() {
		return res, fmt.Errorf("refusing to prune with no retention rules (pass at least one --keep-* flag)")
	}

	if _, err := repo.Open(backend, identityPath); err != nil {
		return res, err
	}
	ix, err := index.Load(backend, identityPath)
	if err != nil {
		return res, fmt.Errorf("failed to load index: %w", err)
	}

	// List snapshots.
	snapRaw, err := backend.ListFiles([]string{repo.SnapshotsPrefix})
	if err != nil {
		return res, fmt.Errorf("failed to list snapshots: %w", err)
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
		// No data/ is fine.
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

	// Delete dead snapshots.
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

	// Delete garbage data objects.
	for _, h := range garbage {
		dp := repo.DataPath(h)
		if err := backend.Delete(dp); err != nil {
			return res, fmt.Errorf("failed to delete data object %s: %w", h, err)
		}
		res.DeletedObjects++
	}

	// Compact index: write one epoch with live entries only, delete old epochs.
	if err := compactIndex(backend, identityPath, ix, live); err != nil {
		return res, err
	}
	res.CompactedEpochs = true

	return res, nil
}

// compactIndex writes a new epoch for known live hashes, if any, then attempts
// to delete prior epochs. Listing failures are treated as an empty listing and
// old-epoch deletion errors are ignored. Repository opening, listing parsing,
// and compaction errors are returned before deleting old epochs.
func compactIndex(backend storage.Backend, identityPath string, ix *index.Index, live map[string]bool) error {
	r, err := repo.Open(backend, identityPath)
	if err != nil {
		return err
	}

	// List current epochs before compacting so we can delete the old ones.
	oldRaw, err := backend.ListFiles([]string{"index"})
	if err != nil {
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

	// Delete every epoch that existed before compaction.
	for _, name := range oldEpochs {
		_ = backend.Delete("index/" + name)
	}
	return nil
}
