package flow

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"gloak/internal/repo"
	"gloak/internal/storage"
)

// listFailBackend wraps a backend and fails listings for selected prefixes,
// so error handling in check, snapshots, and prune can be exercised.
type listFailBackend struct {
	storage.Backend
	filesErr map[string]error
	recurErr map[string]error
}

func (b *listFailBackend) ListFiles(prefix []string) ([]byte, error) {
	if err, ok := b.filesErr[strings.Join(prefix, "/")]; ok {
		return nil, err
	}
	return b.Backend.ListFiles(prefix)
}

func (b *listFailBackend) ListRecursive(prefix []string) ([]string, error) {
	if err, ok := b.recurErr[strings.Join(prefix, "/")]; ok {
		return nil, err
	}
	return b.Backend.ListRecursive(prefix)
}

// missingPathErr builds the wrapped exit-code-3 error rclone produces for a
// directory that does not exist.
func missingPathErr(t *testing.T) error {
	t.Helper()
	err := exec.Command("sh", "-c", "exit 3").Run()
	if err == nil {
		t.Fatal("sh exited 0, want exit 3")
	}
	return fmt.Errorf("failed to list via rclone lsjson: %w", err)
}

func TestRunCheckTreatsMissingSnapshotsDirAsEmpty(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	wrapped := &listFailBackend{
		Backend:  backend,
		filesErr: map[string]error{repo.SnapshotsPrefix: missingPathErr(t)},
	}
	if _, err := runCheck(ident, wrapped); err != nil {
		t.Fatalf("runCheck() on fresh repo error: %v", err)
	}
}

func TestRunCheckPropagatesSnapshotListError(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	wrapped := &listFailBackend{
		Backend:  backend,
		filesErr: map[string]error{repo.SnapshotsPrefix: fmt.Errorf("remote exploded")},
	}
	_, err := runCheck(ident, wrapped)
	if err == nil {
		t.Fatal("runCheck() succeeded despite a listing failure")
	}
	if !strings.Contains(err.Error(), "remote exploded") {
		t.Fatalf("runCheck() error = %v, want it to wrap the listing failure", err)
	}
}

func TestRunCheckPropagatesDataListError(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	wrapped := &listFailBackend{
		Backend:  backend,
		recurErr: map[string]error{repo.DataPrefix: fmt.Errorf("remote exploded")},
	}
	_, err := runCheck(ident, wrapped)
	if err == nil {
		t.Fatal("runCheck() succeeded despite a data listing failure")
	}
	if !strings.Contains(err.Error(), "remote exploded") {
		t.Fatalf("runCheck() error = %v, want it to wrap the listing failure", err)
	}
}

func TestRunSnapshotsPropagatesListError(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	wrapped := &listFailBackend{
		Backend:  backend,
		filesErr: map[string]error{repo.SnapshotsPrefix: fmt.Errorf("remote exploded")},
	}
	_, err := runSnapshots(ident, wrapped)
	if err == nil {
		t.Fatal("runSnapshots() succeeded despite a listing failure")
	}
	if !strings.Contains(err.Error(), "remote exploded") {
		t.Fatalf("runSnapshots() error = %v, want it to wrap the listing failure", err)
	}
}

func TestRunSnapshotsTreatsMissingDirAsEmpty(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	wrapped := &listFailBackend{
		Backend:  backend,
		filesErr: map[string]error{repo.SnapshotsPrefix: missingPathErr(t)},
	}
	infos, err := runSnapshots(ident, wrapped)
	if err != nil {
		t.Fatalf("runSnapshots() on fresh repo error: %v", err)
	}
	if len(infos) != 0 {
		t.Fatalf("runSnapshots() = %v, want empty", infos)
	}
}

func TestRunPrunePropagatesSnapshotListError(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	wrapped := &listFailBackend{
		Backend:  backend,
		filesErr: map[string]error{repo.SnapshotsPrefix: fmt.Errorf("remote exploded")},
	}
	_, err := runPrune(ident, wrapped, RetentionPolicy{KeepLast: 1}, false)
	if err == nil {
		t.Fatal("runPrune() succeeded despite a listing failure")
	}
	if !strings.Contains(err.Error(), "remote exploded") {
		t.Fatalf("runPrune() error = %v, want it to wrap the listing failure", err)
	}
}

func TestRunPrunePropagatesDataListError(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	wrapped := &listFailBackend{
		Backend:  backend,
		recurErr: map[string]error{repo.DataPrefix: fmt.Errorf("remote exploded")},
	}
	_, err := runPrune(ident, wrapped, RetentionPolicy{KeepLast: 1}, false)
	if err == nil {
		t.Fatal("runPrune() succeeded despite a data listing failure")
	}
	if !strings.Contains(err.Error(), "remote exploded") {
		t.Fatalf("runPrune() error = %v, want it to wrap the listing failure", err)
	}
}
