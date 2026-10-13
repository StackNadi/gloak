package flow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"gloak/internal/core"
	"gloak/internal/storage"
)

// Repository locks coordinate separate gloak processes through files under
// locks/ in the repository. Backups take a shared lock (several backups may
// run at once); prune takes an exclusive lock. The check-then-create
// sequence is not atomic on every backend, so the lock is advisory: it
// stops accidental overlap between backup and prune, not a determined race.

const lockPrefix = "locks"

// lockStaleAfter is how long a lock file may exist before other operations
// ignore it. A run that holds its lock longer than this can be overlapped.
const lockStaleAfter = 24 * time.Hour

type repoLock struct {
	Op        string `json:"op"`
	Hostname  string `json:"hostname,omitempty"`
	PID       int    `json:"pid,omitempty"`
	CreatedAt string `json:"created_at"`
}

// acquireRepoLock takes a lock on the repository. A shared acquire (backup)
// fails only while a live non-backup lock exists; an exclusive acquire
// (prune) fails while any live lock exists. Stale locks from processes that
// never released them are removed. The returned function releases the lock
// and is safe to call once.
func acquireRepoLock(backend storage.Backend, op string, shared bool) (func(), error) {
	raw, err := backend.ListFiles([]string{lockPrefix})
	if err != nil {
		if !storage.IsMissingPath(err) {
			return nil, fmt.Errorf("failed to list repository locks: %w", err)
		}
		raw = []byte("[]")
	}
	entries, err := storage.ParseList(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to parse lock listing: %w", err)
	}

	now := time.Now().UTC()
	for _, entry := range entries {
		if entry.IsDir {
			continue
		}
		name := lockPrefix + "/" + entry.Path
		var buf bytes.Buffer
		if err := backend.Download(name, &buf); err != nil {
			return nil, fmt.Errorf("failed to read lock %s: %w", name, err)
		}
		var held repoLock
		if err := json.Unmarshal(buf.Bytes(), &held); err != nil {
			return nil, fmt.Errorf("unreadable lock file %s", name)
		}
		created, err := time.Parse(time.RFC3339, held.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("unreadable lock file %s", name)
		}
		if now.Sub(created) < lockStaleAfter {
			if !shared || held.Op != "backup" {
				return nil, fmt.Errorf("repository is busy: %s lock held by %s since %s",
					held.Op, held.Hostname, held.CreatedAt)
			}
			continue
		}
		// The lock is stale; its holder never released it. Clearing it
		// may race with another process doing the same, which is harmless.
		_ = backend.Delete(name)
	}

	host, _ := os.Hostname()
	payload, err := json.Marshal(repoLock{
		Op:        op,
		Hostname:  host,
		PID:       os.Getpid(),
		CreatedAt: now.Format(time.RFC3339),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal lock: %w", err)
	}
	name := fmt.Sprintf("%s/%s-%s.json", lockPrefix, op, core.GenerateUUID())
	if err := backend.Upload(name, bytes.NewReader(payload)); err != nil {
		return nil, fmt.Errorf("failed to create %s lock: %w", op, err)
	}
	return func() { _ = backend.Delete(name) }, nil
}
