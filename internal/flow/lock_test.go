package flow

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"gloak/internal/storage/memtest"
)

func putLock(t *testing.T, backend *memtest.MemoryBackend, name string, lk repoLock) {
	t.Helper()
	raw, err := json.Marshal(lk)
	if err != nil {
		t.Fatal(err)
	}
	backend.Put("locks/"+name, raw)
}

func freshLock(op string) repoLock {
	return repoLock{
		Op:        op,
		Hostname:  "test-host",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

func TestAcquireRepoLockSharedAllowsOtherBackups(t *testing.T) {
	backend := memtest.NewMemoryBackend()
	release1, err := acquireRepoLock(backend, "backup", true)
	if err != nil {
		t.Fatalf("first shared acquire: %v", err)
	}
	release2, err := acquireRepoLock(backend, "backup", true)
	if err != nil {
		t.Fatalf("second shared acquire: %v", err)
	}
	release2()
	release1()
	if got := len(backend.KeysWithPrefix("locks/")); got != 0 {
		t.Fatalf("locks left behind: %d", got)
	}
}

func TestAcquireRepoLockExclusiveBlocksShared(t *testing.T) {
	backend := memtest.NewMemoryBackend()
	release, err := acquireRepoLock(backend, "prune", false)
	if err != nil {
		t.Fatalf("exclusive acquire: %v", err)
	}
	defer release()

	if _, err := acquireRepoLock(backend, "backup", true); err == nil {
		t.Fatal("shared acquire succeeded while an exclusive lock is held")
	} else if !strings.Contains(err.Error(), "busy") {
		t.Fatalf("error = %v, want it to report a busy repository", err)
	}
}

func TestAcquireRepoLockSharedBlocksExclusive(t *testing.T) {
	backend := memtest.NewMemoryBackend()
	release, err := acquireRepoLock(backend, "backup", true)
	if err != nil {
		t.Fatalf("shared acquire: %v", err)
	}
	defer release()

	if _, err := acquireRepoLock(backend, "prune", false); err == nil {
		t.Fatal("exclusive acquire succeeded while a backup lock is held")
	}
}

func TestAcquireRepoLockBreaksStaleLock(t *testing.T) {
	backend := memtest.NewMemoryBackend()
	stale := freshLock("prune")
	stale.CreatedAt = time.Now().UTC().Add(-lockStaleAfter - time.Hour).Format(time.RFC3339)
	putLock(t, backend, "prune-stale.json", stale)

	release, err := acquireRepoLock(backend, "backup", true)
	if err != nil {
		t.Fatalf("shared acquire with stale lock present: %v", err)
	}
	release()
	keys := backend.KeysWithPrefix("locks/")
	if len(keys) != 0 {
		t.Fatalf("stale lock not cleaned up: %v", keys)
	}
}

func TestRunPruneApplyBlockedByBackupLock(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	putLock(t, backend, "backup-live.json", freshLock("backup"))

	_, err := runPrune(ident, backend, RetentionPolicy{KeepLast: 1}, true)
	if err == nil {
		t.Fatal("runPrune(apply) succeeded while a backup lock is held")
	}
	if !strings.Contains(err.Error(), "busy") {
		t.Fatalf("error = %v, want it to report a busy repository", err)
	}
}

func TestRunBackupPathBlockedByPruneLock(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	putLock(t, backend, "prune-live.json", freshLock("prune"))

	filePath := writeTempFile(t, []byte("locked-out"))
	_, err := runBackupPath(filePath, ident, backend, "7f91c6c7-7a0b-44aa-ae23-997b60e4e997", nil)
	if err == nil {
		t.Fatal("runBackupPath() succeeded while a prune lock is held")
	}
	if !strings.Contains(err.Error(), "busy") {
		t.Fatalf("error = %v, want it to report a busy repository", err)
	}
}

func TestRunPruneDryRunIgnoresLocks(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	putLock(t, backend, "prune-live.json", freshLock("prune"))

	if _, err := runPrune(ident, backend, RetentionPolicy{KeepLast: 1}, false); err != nil {
		t.Fatalf("dry-run runPrune() error: %v", err)
	}
}

func TestAcquireRepoLockRejectsUnreadableLock(t *testing.T) {
	backend := memtest.NewMemoryBackend()
	backend.Put("locks/broken.json", []byte("{not json"))
	_, err := acquireRepoLock(backend, "backup", true)
	if err == nil {
		t.Fatal("acquireRepoLock() succeeded on an unreadable lock file")
	}
	if !strings.Contains(fmt.Sprint(err), "unreadable") {
		t.Fatalf("error = %v, want it to mention the unreadable lock", err)
	}
}
