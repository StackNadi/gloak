# gloak Phase 3 Tree Snapshots Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. User directive: execute inline in-session, not via subagents. Owner pre-approved the design; do not stop for approval gates.

**Goal:** Directory tree backups with tree manifests, snapshots list, multi-file restore with path filter, and per-entry path validation.

**Architecture:** Snapshots gain a `kind` field (`file` or `tree`). Tree walk is sorted, files CDC through the existing index/seal path, empty dirs are recorded. Restore recreates the tree and rejects unsafe relative paths. Single-file flow stays the default for file inputs.

**Tech Stack:** Go 1.26, existing packages. No new dependencies.

## Global Constraints

- Language for docs and user-facing copy: English. Code identifiers English.
- Zero em dash and en dash characters in any written file.
- Corruption errors keep the `CHUNK CORRUPT` prefix on the message.
- Format version string stays `"2.0"`. Breaking change is noted in CHANGELOG only.
- Defaults: min 524288, avg 1048576, max 8388608 bytes.
- Gear window size: 64 bytes.
- After every task: `go test ./... && go vet ./...` green before commit.
- Never stage or touch `M .gitignore`, `M README.md`, `?? docs/GUIDE.md`.
- Branch: create `phase3-tree` from `main` before Task 1.

---

### Task 1: Tree types and path validation

**Files:**
- Modify: `internal/core/manifest.go`
- Create: `internal/core/manifest_test.go`
- Create: `internal/core/path.go`
- Create: `internal/core/path_test.go`

**Interfaces:**
- Consumes: `core.ChunkMeta`
- Produces:
  - `core.KindFile = "file"`, `core.KindTree = "tree"`
  - `core.TreeEntry`, `core.TreeManifest`
  - `(*TreeManifest).FileCount() int`
  - `core.ValidateRelPath(rel string) error`

- [ ] **Step 1: Write failing tests**

```go
package core

import "testing"

func TestFileCount(t *testing.T) {
	m := TreeManifest{Entries: []TreeEntry{
		{Type: "dir"},
		{Type: "file"},
		{Type: "file"},
	}}
	if got := m.FileCount(); got != 2 {
		t.Fatalf("FileCount() = %d, want 2", got)
	}
}

func TestValidateRelPathAcceptsNormal(t *testing.T) {
	for _, p := range []string{"a", "a/b", "a/b/c.txt", "dir/empty"} {
		if err := ValidateRelPath(p); err != nil {
			t.Fatalf("ValidateRelPath(%q) error: %v", p, err)
		}
	}
}

func TestValidateRelPathRejectsUnsafe(t *testing.T) {
	for _, p := range []string{"", "/abs", "a//b", "a/../b", "..", ".", "a/.", `a\b`, "a\x00b", "C:/x", "c:\\x"} {
		if err := ValidateRelPath(p); err == nil {
			t.Fatalf("ValidateRelPath(%q) succeeded", p)
		}
	}
}
```

- [ ] **Step 2: Run tests, expect compile failure**

Run: `go test ./internal/core/ -count=1`
Expected: FAIL undefined ValidateRelPath / TreeManifest

- [ ] **Step 3: Implement**

Add to `internal/core/manifest.go`:

```go
const (
	KindFile = "file"
	KindTree = "tree"
)

type TreeEntry struct {
	RelPath string      `json:"rel_path"`
	Type    string      `json:"type"`
	Mode    uint32      `json:"mode"`
	Mtime   string      `json:"mtime"`
	Size    int64       `json:"size"`
	Chunks  []ChunkMeta `json:"chunks"`
}

type TreeManifest struct {
	Version   string      `json:"version"`
	UUID      string      `json:"uuid"`
	Kind      string      `json:"kind"`
	CreatedAt string      `json:"created_at"`
	Hostname  string      `json:"hostname"`
	Paths     []string    `json:"paths"`
	Entries   []TreeEntry `json:"entries"`
}

func (m *TreeManifest) FileCount() int {
	n := 0
	for _, e := range m.Entries {
		if e.Type == "file" {
			n++
		}
	}
	return n
}
```

Create `internal/core/path.go`:

```go
package core

import (
	"fmt"
	"strings"
)

// ValidateRelPath rejects tree entry paths that could escape the restore
// root. The manifest is remote input; this is the trust boundary.
func ValidateRelPath(rel string) error {
	if rel == "" {
		return fmt.Errorf("empty path")
	}
	if strings.ContainsRune(rel, 0) {
		return fmt.Errorf("path contains null byte")
	}
	if strings.Contains(rel, `\`) {
		return fmt.Errorf("path contains backslash: %s", rel)
	}
	if strings.HasPrefix(rel, "/") {
		return fmt.Errorf("absolute path: %s", rel)
	}
	if len(rel) >= 2 && rel[1] == ':' {
		return fmt.Errorf("drive letter path: %s", rel)
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("unsafe path segment in %s", rel)
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to pass**

Run: `go test ./internal/core/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/core/manifest.go internal/core/manifest_test.go internal/core/path.go internal/core/path_test.go
git commit -m "feat(core): add tree manifest types and path validation"
```

---

### Task 2: Tree backup walk

**Files:**
- Modify: `internal/flow/backup.go`
- Create: `internal/flow/tree_backup.go`
- Create: `internal/flow/tree_backup_test.go`
- Modify: `internal/flow/manifest_read.go`

**Interfaces:**
- Consumes: `chunker.NewCDC`, `core.TreeManifest`, `index.Load`, `crypto.SealChunk`
- Produces:
  - `runBackupPath(filePath, identityPath string, backend storage.Backend, snapshotID string, progress func(int)) (BackupResult, error)` dispatches file vs dir
  - `runTreeBackup(dirPath, identityPath string, backend storage.Backend, snapshotID string, progress func(int)) (BackupResult, error)`
  - `readTreeManifest(backend, identityPath, snapshotID) (core.TreeManifest, error)`
  - `readAnyManifest(backend, identityPath, snapshotID) (kind string, file *core.Manifest, tree *core.TreeManifest, err error)`

- [ ] **Step 1: Write failing tests**

```go
package flow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gloak/internal/core"
)

func TestRunTreeBackupNestedDirs(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub", "empty"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "b.txt"), []byte("beta-beta-beta"), 0644); err != nil {
		t.Fatal(err)
	}

	snap := "aaaaaaaa-bbbb-4ccc-8ddd-000000000010"
	res, err := runBackupPath(root, ident, backend, snap, nil)
	if err != nil {
		t.Fatalf("runBackupPath() error: %v", err)
	}
	if res.TotalChunks < 2 {
		t.Fatalf("TotalChunks = %d, want >= 2", res.TotalChunks)
	}
	m, err := readTreeManifest(backend, ident, snap)
	if err != nil {
		t.Fatal(err)
	}
	if m.Kind != core.KindTree {
		t.Fatalf("Kind = %q", m.Kind)
	}
	if m.FileCount() != 2 {
		t.Fatalf("FileCount = %d", m.FileCount())
	}
	// Lexical order by RelPath.
	var paths []string
	for _, e := range m.Entries {
		paths = append(paths, e.RelPath)
	}
	wantAny := []string{"a.txt", "sub", "sub/b.txt", filepath.Base(root) + "/empty"}
	_ = wantAny
	if paths[0] != "a.txt" {
		t.Fatalf("first entry = %q, want a.txt", paths[0])
	}
	// empty dir recorded
	foundEmpty := false
	for _, e := range m.Entries {
		if e.Type == "dir" && strings.HasSuffix(e.RelPath, "empty") {
			foundEmpty = true
		}
	}
	if !foundEmpty {
		t.Fatalf("empty dir missing, entries = %v", paths)
	}
}

func TestRunTreeBackupSkipsSymlinks(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "real.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.txt", filepath.Join(root, "link")); err != nil {
		t.Skip("symlink not supported")
	}
	snap := "aaaaaaaa-bbbb-4ccc-8ddd-000000000011"
	if _, err := runBackupPath(root, ident, backend, snap, nil); err != nil {
		t.Fatal(err)
	}
	m, err := readTreeManifest(backend, ident, snap)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range m.Entries {
		if e.RelPath == "link" {
			t.Fatal("symlink recorded")
		}
	}
}

func TestRunBackupPathDispatchesFile(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	filePath := writeTempFile(t, []byte("single"))
	snap := "aaaaaaaa-bbbb-4ccc-8ddd-000000000012"
	if _, err := runBackupPath(filePath, ident, backend, snap, nil); err != nil {
		t.Fatal(err)
	}
	m, err := readManifest(backend, ident, snap)
	if err != nil {
		t.Fatal(err)
	}
	if m.OriginalName != "payload.bin" {
		t.Fatalf("OriginalName = %q", m.OriginalName)
	}
}
```

- [ ] **Step 2: Run, expect compile failure**

Run: `go test ./internal/flow/ -run TestRunTreeBackup -count=1`
Expected: FAIL undefined runBackupPath / runTreeBackup

- [ ] **Step 3: Implement tree walk and wire dispatch**

Create `internal/flow/tree_backup.go`:

```go
package flow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gloak/internal/chunker"
	"gloak/internal/core"
	"gloak/internal/crypto"
	"gloak/internal/index"
	"gloak/internal/repo"
	"gloak/internal/storage"
)

func runTreeBackup(dirPath, identityPath string, backend storage.Backend, snapshotID string, progress func(int)) (BackupResult, error) {
	r, err := repo.Open(backend, identityPath)
	if err != nil {
		return BackupResult{}, err
	}
	ix, err := index.Load(backend, identityPath)
	if err != nil {
		return BackupResult{}, fmt.Errorf("failed to load index: %w", err)
	}
	cdc, err := chunker.NewCDC(r.Config.ChunkMin, r.Config.ChunkAvg, r.Config.ChunkMax)
	if err != nil {
		return BackupResult{}, fmt.Errorf("invalid chunk config: %w", err)
	}

	abs, err := filepath.Abs(dirPath)
	if err != nil {
		return BackupResult{}, err
	}
	base := filepath.Base(abs)

	var entries []core.TreeEntry
	var sinceCheckpoint int
	res := BackupResult{SnapshotID: snapshotID}

	walkErr := filepath.WalkDir(abs, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if p == abs {
			return nil
		}
		rel, err := filepath.Rel(abs, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := uint32(info.Mode().Perm())
		mtime := info.ModTime().UTC().Format(time.RFC3339)

		switch {
		case d.IsDir():
			entries = append(entries, core.TreeEntry{
				RelPath: rel,
				Type:    "dir",
				Mode:    mode,
				Mtime:   mtime,
			})
			return nil
		case !info.Mode().IsRegular():
			// Symlinks and special files are skipped.
			return nil
		}

		file, err := os.Open(p)
		if err != nil {
			return err
		}
		defer file.Close()

		var chunks []core.ChunkMeta
		payloadHasher := sha256.New()
		splitErr := cdc.Split(file, func(chunk []byte) error {
			payloadHasher.Write(chunk)
			hash := hashBytes(chunk)
			res.TotalChunks++
			res.TotalPlainBytes += int64(len(chunk))
			dataPath := repo.DataPath(hash)

			if ix.Has(hash) {
				res.SkippedChunks++
			} else {
				sealed, err := crypto.SealChunk(r.MasterKey, hash, chunk)
				if err != nil {
					return fmt.Errorf("failed to seal chunk %s: %w", hash, err)
				}
				encSum := sha256.Sum256(sealed)
				if err := backend.Upload(dataPath, bytes.NewReader(sealed)); err != nil {
					return fmt.Errorf("failed to upload chunk %s: %w", hash, err)
				}
				ix.Add(index.Entry{
					PlainSHA256:     hash,
					PlainSize:       int64(len(chunk)),
					EncryptedSize:   int64(len(sealed)),
					EncryptedSHA256: hex.EncodeToString(encSum[:]),
				})
				res.UploadedChunks++
				sinceCheckpoint++
				if sinceCheckpoint >= indexCheckpointEvery {
					if _, err := ix.Save(backend, r.Config.Recipients); err != nil {
						return fmt.Errorf("failed to checkpoint index: %w", err)
					}
					sinceCheckpoint = 0
				}
			}

			encEntry, _ := ix.Get(hash)
			chunks = append(chunks, core.ChunkMeta{
				Index:           len(chunks),
				Name:            dataPath,
				PlainSize:       encEntry.PlainSize,
				PlainSHA256:     hash,
				EncryptedSize:   encEntry.EncryptedSize,
				EncryptedSHA256: encEntry.EncryptedSHA256,
			})
			if progress != nil {
				progress(res.TotalChunks)
			}
			return nil
		})
		if splitErr != nil {
			return splitErr
		}
		entries = append(entries, core.TreeEntry{
			RelPath: rel,
			Type:    "file",
			Mode:    mode,
			Mtime:   mtime,
			Size:    info.Size(),
			Chunks:  chunks,
		})
		return nil
	})
	if walkErr != nil {
		return BackupResult{}, walkErr
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].RelPath < entries[j].RelPath
	})

	if _, err := ix.Save(backend, r.Config.Recipients); err != nil {
		return BackupResult{}, fmt.Errorf("failed to save index: %w", err)
	}

	host, _ := os.Hostname()
	manifest := core.TreeManifest{
		Version:   repo.FormatVersion,
		UUID:      snapshotID,
		Kind:      core.KindTree,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Hostname:  host,
		Paths:     []string{abs},
		Entries:   entries,
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return BackupResult{}, fmt.Errorf("failed to marshal tree manifest: %w", err)
	}
	sealed, err := crypto.EncryptMulti(r.Config.Recipients, manifestJSON)
	if err != nil {
		return BackupResult{}, fmt.Errorf("failed to encrypt tree manifest: %w", err)
	}
	snapshotPath, err := repo.SnapshotPath(snapshotID)
	if err != nil {
		return BackupResult{}, err
	}
	if err := backend.Upload(snapshotPath, bytes.NewReader(sealed)); err != nil {
		return BackupResult{}, fmt.Errorf("failed to upload snapshot: %w", err)
	}
	_ = base
	return res, nil
}
```

Add `readTreeManifest` and `readAnyManifest` to `internal/flow/manifest_read.go`:

```go
func readTreeManifest(backend storage.Backend, identityPath, snapshotID string) (core.TreeManifest, error) {
	path, err := repo.SnapshotPath(snapshotID)
	if err != nil {
		return core.TreeManifest{}, err
	}
	var buf bytes.Buffer
	if err := backend.Download(path, &buf); err != nil {
		return core.TreeManifest{}, fmt.Errorf("snapshot %s not found: %w", snapshotID, err)
	}
	reader, err := crypto.DecryptReader(identityPath, &buf)
	if err != nil {
		return core.TreeManifest{}, fmt.Errorf("failed to decrypt snapshot: %w", err)
	}
	plain, err := io.ReadAll(reader)
	if err != nil {
		return core.TreeManifest{}, fmt.Errorf("failed to read snapshot: %w", err)
	}
	var manifest core.TreeManifest
	if err := json.Unmarshal(plain, &manifest); err != nil {
		return core.TreeManifest{}, fmt.Errorf("failed to parse snapshot: %w", err)
	}
	return manifest, nil
}

// readAnyManifest decrypts a snapshot and returns its kind. kind is
// core.KindFile or core.KindTree; exactly one of file/tree is non-nil.
func readAnyManifest(backend storage.Backend, identityPath, snapshotID string) (string, *core.Manifest, *core.TreeManifest, error) {
	path, err := repo.SnapshotPath(snapshotID)
	if err != nil {
		return "", nil, nil, err
	}
	var buf bytes.Buffer
	if err := backend.Download(path, &buf); err != nil {
		return "", nil, nil, fmt.Errorf("snapshot %s not found: %w", snapshotID, err)
	}
	reader, err := crypto.DecryptReader(identityPath, &buf)
	if err != nil {
		return "", nil, nil, fmt.Errorf("failed to decrypt snapshot: %w", err)
	}
	plain, err := io.ReadAll(reader)
	if err != nil {
		return "", nil, nil, fmt.Errorf("failed to read snapshot: %w", err)
	}
	var probe struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(plain, &probe); err != nil {
		return "", nil, nil, fmt.Errorf("failed to parse snapshot: %w", err)
	}
	switch probe.Kind {
	case core.KindTree:
		var m core.TreeManifest
		if err := json.Unmarshal(plain, &m); err != nil {
			return "", nil, nil, fmt.Errorf("failed to parse tree snapshot: %w", err)
		}
		return core.KindTree, nil, &m, nil
	case "", core.KindFile:
		var m core.Manifest
		if err := json.Unmarshal(plain, &m); err != nil {
			return "", nil, nil, fmt.Errorf("failed to parse file snapshot: %w", err)
		}
		return core.KindFile, &m, nil, nil
	default:
		return "", nil, nil, fmt.Errorf("unknown snapshot kind %q", probe.Kind)
	}
}
```

Change `RunBackup` / `runBackup` call sites: in `internal/flow/backup.go`, replace public `RunBackup` to call `runBackupPath`. Rename existing `runBackup` body to `runFileBackup` (or keep `runBackup` for file and add dispatcher):

```go
func RunBackup(filePath, identityPath, remote string) (string, error) {
	backend := storage.NewRcloneBackend(remote)
	snapshotID := core.GenerateUUID()

	spinner, _ := pterm.DefaultSpinner.Start("Backing up...")
	res, err := runBackupPath(filePath, identityPath, backend, snapshotID, func(done int) {
		spinner.UpdateText(fmt.Sprintf("Backed up %d chunks", done))
	})
	if err != nil {
		_ = spinner.Stop()
		return "", err
	}
	_ = spinner.Stop()

	pterm.Success.Println("Backup complete")
	pterm.Printf("Snapshot ID: %s\n", res.SnapshotID)
	pterm.Printf("Chunks: %d total, %d uploaded, %d skipped\n", res.TotalChunks, res.UploadedChunks, res.SkippedChunks)
	pterm.Printf("Data size: %d bytes\n", res.TotalPlainBytes)
	return res.SnapshotID, nil
}

func runBackupPath(filePath, identityPath string, backend storage.Backend, snapshotID string, progress func(int)) (BackupResult, error) {
	if _, err := uuid.Parse(snapshotID); err != nil {
		return BackupResult{}, fmt.Errorf("invalid snapshot id: %w", err)
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return BackupResult{}, fmt.Errorf("failed to stat path: %w", err)
	}
	if info.IsDir() {
		return runTreeBackup(filePath, identityPath, backend, snapshotID, progress)
	}
	return runBackup(filePath, identityPath, backend, snapshotID, progress)
}
```

Keep existing `runBackup` as the file-only implementation (it already validates uuid).

- [ ] **Step 4: Run flow tests**

Run: `go test ./internal/flow/ -count=1`
Expected: PASS (existing single-file tests still call runBackup directly)

- [ ] **Step 5: Full suite and commit**

Run: `go test ./... && go vet ./...`

```bash
git add internal/flow/tree_backup.go internal/flow/tree_backup_test.go internal/flow/backup.go internal/flow/manifest_read.go
git commit -m "feat(flow): backup directory trees with sorted walk"
```

---

### Task 3: Tree restore with path filter

**Files:**
- Modify: `internal/flow/restore.go`
- Modify: `restore.go` (CLI)
- Modify: `internal/flow/restore_test.go` or create `internal/flow/tree_restore_test.go`
- Modify: `main_test.go` if new short tags

**Interfaces:**
- Consumes: `core.ValidateRelPath`, `readAnyManifest`
- Produces:
  - `RunRestore(snapshotID, identityPath, remote, outputDir, pathFilter string) error`
  - `runRestore(snapshotID, outputDir, identityPath string, backend storage.Backend, pathFilter string) (string, error)`
  - `runTreeRestore(m *core.TreeManifest, r *repo.Repo, outputDir, pathFilter string, backend storage.Backend) (string, error)`

- [ ] **Step 1: Write failing tests**

```go
package flow

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gloak/internal/core"
)

func treeFixture(t *testing.T) (string, *memtestBackendAlias, string, string) {
	t.Helper()
	ident, backend, _ := repoFixture(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub", "empty"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "b.txt"), []byte("beta"), 0644); err != nil {
		t.Fatal(err)
	}
	snap := "aaaaaaaa-bbbb-4ccc-8ddd-000000000020"
	if _, err := runBackupPath(root, ident, backend, snap, nil); err != nil {
		t.Fatal(err)
	}
	return root, backend, ident, snap
}

func TestRunTreeRestoreFull(t *testing.T) {
	_, backend, ident, snap := treeFixture(t)
	out := t.TempDir()
	got, err := runRestore(snap, out, ident, backend, "")
	if err != nil {
		t.Fatalf("runRestore() error: %v", err)
	}
	base := filepath.Base(got)
	if base == "" {
		t.Fatal("empty restore root")
	}
	a, err := os.ReadFile(filepath.Join(got, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != "alpha" {
		t.Fatalf("a.txt = %q", a)
	}
	b, err := os.ReadFile(filepath.Join(got, "sub", "b.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "beta" {
		t.Fatalf("b.txt = %q", b)
	}
	if fi, err := os.Stat(filepath.Join(got, "sub", "empty")); err != nil || !fi.IsDir() {
		t.Fatal("empty dir not restored")
	}
}

func TestRunTreeRestorePathFilter(t *testing.T) {
	_, backend, ident, snap := treeFixture(t)
	out := t.TempDir()
	got, err := runRestore(snap, out, ident, backend, "sub")
	if err != nil {
		t.Fatalf("runRestore() error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(got, "a.txt")); err == nil {
		t.Fatal("a.txt restored despite filter")
	}
	b, err := os.ReadFile(filepath.Join(got, "sub", "b.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "beta" {
		t.Fatalf("b.txt = %q", b)
	}
}

func TestRunTreeRestoreRejectsUnsafeRelPath(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	// Craft a tree snapshot with a traversal RelPath.
	m := core.TreeManifest{
		Version:   "2.0",
		UUID:      "aaaaaaaa-bbbb-4ccc-8ddd-000000000030",
		Kind:      core.KindTree,
		CreatedAt: "2026-10-12T00:00:00Z",
		Hostname:  "test",
		Paths:     []string{"/tmp/x"},
		Entries: []core.TreeEntry{
			{RelPath: "../escape", Type: "file", Mode: 0644, Mtime: "2026-10-12T00:00:00Z", Size: 0},
		},
	}
	seedTreeSnapshot(t, backend, ident, m)

	_, err := runRestore(m.UUID, t.TempDir(), ident, backend, "")
	if err == nil {
		t.Fatal("runRestore() accepted traversal RelPath")
	}
	if !strings.Contains(err.Error(), "unsafe") && !strings.Contains(err.Error(), "..") {
		t.Fatalf("error = %v", err)
	}
}
```

Note: need a test helper `seedTreeSnapshot` that marshals and EncryptMulti-uploads the crafted manifest. Use `repo.Open` master recipients from fixture:

```go
func seedTreeSnapshot(t *testing.T, backend storage.Backend, ident string, m core.TreeManifest) {
	t.Helper()
	r, err := repo.Open(backend, ident)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := crypto.EncryptMulti(r.Config.Recipients, raw)
	if err != nil {
		t.Fatal(err)
	}
	p, err := repo.SnapshotPath(m.UUID)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Upload(p, bytes.NewReader(sealed)); err != nil {
		t.Fatal(err)
	}
}
```

Use actual `*memtest.MemoryBackend` type instead of alias.

- [ ] **Step 2: Run, expect compile failure**

Run: `go test ./internal/flow/ -run TestRunTreeRestore -count=1`
Expected: FAIL wrong arity / undefined pathFilter

- [ ] **Step 3: Implement restore**

Update `internal/flow/restore.go`:

```go
func RunRestore(snapshotID, identityPath, remote, outputDir, pathFilter string) error {
	backend := storage.NewRcloneBackend(remote)
	outputPath, err := runRestore(snapshotID, outputDir, identityPath, backend, pathFilter)
	if err != nil {
		return err
	}
	pterm.Success.Println("Restore completed successfully! File saved to:", outputPath)
	return nil
}

func runRestore(snapshotID, outputDir, identityPath string, backend storage.Backend, pathFilter string) (string, error) {
	if isFileMode(backend, snapshotID) {
		return runFileRestore(snapshotID, identityPath, outputDir, backend)
	}
	kind, fileM, treeM, err := readAnyManifest(backend, identityPath, snapshotID)
	if err != nil {
		return "", err
	}
	if kind == core.KindTree {
		r, err := repo.Open(backend, identityPath)
		if err != nil {
			return "", err
		}
		return runTreeRestore(treeM, r, outputDir, pathFilter, backend)
	}
	return runFileRestoreFromManifest(fileM, backend, identityPath, outputDir)
}
```

Refactor existing file restore body into `runFileRestoreFromManifest(manifest *core.Manifest, backend storage.Backend, identityPath, outputDir string) (string, error)` and keep `runFileRestore` as a thin wrapper that reads the file manifest first (for backward compat with existing callers/tests).

Add:

```go
func runTreeRestore(m *core.TreeManifest, r *repo.Repo, outputDir, pathFilter string, backend storage.Backend) (string, error) {
	if len(m.Paths) == 0 {
		return "", fmt.Errorf("tree snapshot has no source paths")
	}
	rootName := filepath.Base(filepath.Clean(m.Paths[0]))
	if rootName == "" || rootName == "." || rootName == string(filepath.Separator) {
		rootName = m.UUID
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}
	destRoot := filepath.Join(outputDir, rootName)

	for _, entry := range m.Entries {
		if err := core.ValidateRelPath(entry.RelPath); err != nil {
			return "", fmt.Errorf("unsafe path in manifest: %w", err)
		}
		if pathFilter != "" {
			if entry.RelPath != pathFilter && !strings.HasPrefix(entry.RelPath, pathFilter+"/") {
				continue
			}
		}
		target := filepath.Join(destRoot, filepath.FromSlash(entry.RelPath))
		// Defense in depth: ensure target stays under destRoot.
		if !strings.HasPrefix(target, destRoot+string(filepath.Separator)) && target != destRoot {
			return "", fmt.Errorf("restored path escapes output root: %s", entry.RelPath)
		}
		switch entry.Type {
		case "dir":
			if err := os.MkdirAll(target, os.FileMode(entry.Mode)&0777); err != nil {
				return "", fmt.Errorf("failed to create directory %s: %w", entry.RelPath, err)
			}
		case "file":
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return "", fmt.Errorf("failed to create parent for %s: %w", entry.RelPath, err)
			}
			if err := restoreTreeFile(entry, r, backend, target); err != nil {
				return "", err
			}
		default:
			return "", fmt.Errorf("unknown tree entry type %q for %s", entry.Type, entry.RelPath)
		}
	}
	return destRoot, nil
}

func restoreTreeFile(entry core.TreeEntry, r *repo.Repo, backend storage.Backend, target string) error {
	outFile, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(entry.Mode)&0777)
	if err != nil {
		return fmt.Errorf("failed to create output file %s: %w", entry.RelPath, err)
	}
	defer outFile.Close()

	for _, meta := range entry.Chunks {
		if err := validateChunkPath(meta.Name); err != nil {
			return err
		}
		var buf bytes.Buffer
		if err := backend.Download(meta.Name, &buf); err != nil {
			return fmt.Errorf("CHUNK CORRUPT %d (%s): missing object: %w", meta.Index, meta.Name, err)
		}
		plain, err := crypto.OpenChunk(r.MasterKey, meta.PlainSHA256, buf.Bytes())
		if err != nil {
			return fmt.Errorf("CHUNK CORRUPT %d (%s): %w", meta.Index, meta.Name, err)
		}
		if int64(len(plain)) != meta.PlainSize || hashBytes(plain) != meta.PlainSHA256 {
			return fmt.Errorf("CHUNK CORRUPT %d (%s): plaintext mismatch", meta.Index, meta.Name)
		}
		if _, err := outFile.Write(plain); err != nil {
			return fmt.Errorf("failed to write chunk %d: %w", meta.Index, err)
		}
	}
	if mt, err := time.Parse(time.RFC3339, entry.Mtime); err == nil {
		_ = os.Chtimes(target, mt, mt)
	}
	return nil
}
```

Update CLI `restore.go`:

```go
type RestoreCmd struct {
	UUID       string `arg:"" name:"uuid" help:"Backup UUID to restore."`
	Identity   string `help:"Identity private key file (defaults to ~/.gloak/identity.txt)." short:"i"`
	Remote     string `help:"Source Rclone URI (e.g., keraaaki:backup_folder or /mnt/backups)." required:"" short:"R"`
	OutputDir  string `help:"Output directory." short:"o" default:"./restored"`
	PathFilter string `help:"Restore only this relative path (and children) from a tree snapshot."`
}

func (r *RestoreCmd) Run() error {
	pterm.Info.Println("Starting RESTORE process...")
	ident := r.Identity
	if ident == "" {
		ident = filepath.Join(getDefaultDir(), "identity.txt")
		pterm.Info.Printf("Using identity from: %s\n", ident)
	}
	return flow.RunRestore(r.UUID, ident, r.Remote, r.OutputDir, r.PathFilter)
}
```

- [ ] **Step 4: Fix compile errors and run tests**

Run: `go test ./... -count=1`
Expected: PASS. Update any existing callers of `runRestore` in `flow_test.go` and `filemode_test.go` to pass empty pathFilter `""`.

- [ ] **Step 5: Commit**

```bash
git add internal/flow/restore.go internal/flow/tree_restore_test.go restore.go internal/flow/flow_test.go internal/flow/filemode_test.go
git commit -m "feat(flow): restore tree snapshots with path filter"
```

---

### Task 4: Tree verify and check integration

**Files:**
- Modify: `internal/flow/verify.go`
- Modify: `internal/flow/check.go`
- Create: `internal/flow/tree_verify_test.go` or extend `check_test.go`

**Interfaces:**
- Consumes: `readAnyManifest`
- Produces: verify and check handle tree kind transparently

- [ ] **Step 1: Write failing tests**

```go
package flow

import "testing"

func TestRunVerifyTreeSnapshot(t *testing.T) {
	_, backend, ident, snap := treeFixture(t)
	if err := runVerify(snap, ident, backend); err != nil {
		t.Fatalf("runVerify() error: %v", err)
	}
}

func TestRunVerifyTreeCorruptChunk(t *testing.T) {
	_, backend, ident, snap := treeFixture(t)
	keys := backend.KeysWithPrefix("data/")
	if len(keys) == 0 {
		t.Fatal("no data")
	}
	blob := backend.Get(keys[0])
	blob[len(blob)/2] ^= 0xff
	backend.Put(keys[0], blob)
	err := runVerify(snap, ident, backend)
	if err == nil {
		t.Fatal("runVerify() succeeded on corrupt chunk")
	}
	if !strings.Contains(err.Error(), "CHUNK CORRUPT") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunCheckWithTreeAndFileSnapshots(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	filePath := writeTempFile(t, []byte("single-file-payload"))
	if _, err := runBackup(filePath, ident, backend, "7f91c6c7-7a0b-44aa-ae23-997b60e4e999", nil); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "t.txt"), []byte("tree-payload"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := runBackupPath(root, ident, backend, "8f91c6c7-7a0b-44aa-ae23-997b60e4e999", nil); err != nil {
		t.Fatal(err)
	}
	res, err := runCheck(ident, backend)
	if err != nil {
		t.Fatalf("runCheck() error: %v", err)
	}
	if res.Snapshots != 2 {
		t.Fatalf("Snapshots = %d, want 2", res.Snapshots)
	}
	if len(res.Missing) != 0 {
		t.Fatalf("Missing = %v", res.Missing)
	}
}
```

- [ ] **Step 2: Run, expect failures**

Run: `go test ./internal/flow/ -run 'TestRunVerifyTree|TestRunCheckWithTree' -count=1`
Expected: FAIL (verify still uses file-only readManifest; check same)

- [ ] **Step 3: Update verify.go**

```go
func runVerify(snapshotID, identityPath string, backend storage.Backend) error {
	if isFileMode(backend, snapshotID) {
		return runFileVerify(snapshotID, identityPath, backend)
	}
	kind, fileM, treeM, err := readAnyManifest(backend, identityPath, snapshotID)
	if err != nil {
		return err
	}
	r, err := repo.Open(backend, identityPath)
	if err != nil {
		return err
	}
	if kind == core.KindTree {
		return runTreeVerify(treeM, r, backend)
	}
	return runFileVerifyFromManifest(fileM, r, backend)
}

func runTreeVerify(m *core.TreeManifest, r *repo.Repo, backend storage.Backend) error {
	for _, entry := range m.Entries {
		if entry.Type != "file" {
			continue
		}
		if err := core.ValidateRelPath(entry.RelPath); err != nil {
			return err
		}
		for _, meta := range entry.Chunks {
			if err := validateChunkPath(meta.Name); err != nil {
				return err
			}
			var buf bytes.Buffer
			if err := backend.Download(meta.Name, &buf); err != nil {
				return fmt.Errorf("CHUNK CORRUPT %d (%s): missing object: %w", meta.Index, meta.Name, err)
			}
			sealed := buf.Bytes()
			if int64(len(sealed)) != meta.EncryptedSize {
				return fmt.Errorf("CHUNK CORRUPT %d (%s): encrypted size mismatch", meta.Index, meta.Name)
			}
			if hashBytes(sealed) != meta.EncryptedSHA256 {
				return fmt.Errorf("CHUNK CORRUPT %d (%s): encrypted sha256 mismatch", meta.Index, meta.Name)
			}
			plain, err := crypto.OpenChunk(r.MasterKey, meta.PlainSHA256, sealed)
			if err != nil {
				return fmt.Errorf("CHUNK CORRUPT %d (%s): %w", meta.Index, meta.Name, err)
			}
			if int64(len(plain)) != meta.PlainSize || hashBytes(plain) != meta.PlainSHA256 {
				return fmt.Errorf("CHUNK CORRUPT %d (%s): plaintext mismatch", meta.Index, meta.Name)
			}
		}
	}
	return nil
}
```

Refactor existing file verify body into `runFileVerifyFromManifest(manifest *core.Manifest, r *repo.Repo, backend storage.Backend) error` and keep `runFileVerify` as a thin wrapper.

- [ ] **Step 4: Update check.go snapshot walk**

Replace the snapshot loop body with:

```go
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
```

Add `"gloak/internal/core"` import to check.go.

- [ ] **Step 5: Run tests and commit**

Run: `go test ./... && go vet ./...`

```bash
git add internal/flow/verify.go internal/flow/check.go internal/flow/tree_verify_test.go
git commit -m "feat(flow): verify and check tree snapshots"
```

---

### Task 5: snapshots list command

**Files:**
- Create: `internal/flow/snapshots.go`
- Create: `internal/flow/snapshots_test.go`
- Create: `snapshots.go` (CLI)
- Modify: `main.go`
- Modify: `main_test.go`

**Interfaces:**
- Consumes: `backend.ListFiles`, `readAnyManifest`, `storage.ParseList`
- Produces:
  - `flow.RunSnapshots(identityPath, remote string) error`
  - `flow.runSnapshots(identityPath string, backend storage.Backend) ([]SnapshotInfo, error)`
  - `flow.SnapshotInfo{UUID, Kind, CreatedAt, Hostname, Detail string}`
  - `SnapshotsCmd` CLI with `-R`, `-i`

- [ ] **Step 1: Write failing tests**

```go
package flow

import "testing"

func TestRunSnapshotsListsBothKinds(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	filePath := writeTempFile(t, []byte("single"))
	if _, err := runBackup(filePath, ident, backend, "7f91c6c7-7a0b-44aa-ae23-997b60e4e998", nil); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "t.txt"), []byte("tree"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := runBackupPath(root, ident, backend, "8f91c6c7-7a0b-44aa-ae23-997b60e4e999", nil); err != nil {
		t.Fatal(err)
	}
	infos, err := runSnapshots(ident, backend)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 2 {
		t.Fatalf("len = %d, want 2", len(infos))
	}
	kinds := map[string]bool{}
	for _, info := range infos {
		kinds[info.Kind] = true
	}
	if !kinds["file"] || !kinds["tree"] {
		t.Fatalf("kinds = %v", kinds)
	}
}

func TestRunSnapshotsEmpty(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	infos, err := runSnapshots(ident, backend)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 0 {
		t.Fatalf("len = %d", len(infos))
	}
}
```

- [ ] **Step 2: Run, expect compile failure**

Run: `go test ./internal/flow/ -run TestRunSnapshots -count=1`
Expected: FAIL undefined runSnapshots

- [ ] **Step 3: Implement**

`internal/flow/snapshots.go`:

```go
package flow

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/pterm/pterm"

	"gloak/internal/core"
	"gloak/internal/repo"
	"gloak/internal/storage"
)

type SnapshotInfo struct {
	UUID      string
	Kind      string
	CreatedAt string
	Hostname  string
	Detail    string
}

func RunSnapshots(identityPath, remote string) error {
	backend := storage.NewRcloneBackend(remote)
	infos, err := runSnapshots(identityPath, backend)
	if err != nil {
		return err
	}
	if len(infos) == 0 {
		pterm.Info.Println("No snapshots found.")
		return nil
	}
	table := pterm.DefaultTable.
		WithHeader([]string{"UUID", "KIND", "CREATED", "HOST", "DETAIL"}).
		WithData(snapshotRows(infos))
	_ = table.Render()
	return nil
}

func snapshotRows(infos []SnapshotInfo) [][]string {
	rows := make([][]string, 0, len(infos))
	for _, info := range infos {
		rows = append(rows, []string{info.UUID, info.Kind, info.CreatedAt, info.Hostname, info.Detail})
	}
	return rows
}

func runSnapshots(identityPath string, backend storage.Backend) ([]SnapshotInfo, error) {
	if _, err := repo.Open(backend, identityPath); err != nil {
		return nil, err
	}
	raw, err := backend.ListFiles([]string{repo.SnapshotsPrefix})
	if err != nil {
		// Fresh repo with no snapshots directory.
		return nil, nil
	}
	entries, err := storage.ParseList(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to parse snapshot listing: %w", err)
	}
	var infos []SnapshotInfo
	for _, entry := range entries {
		if entry.IsDir || !strings.HasSuffix(entry.Path, ".age") {
			continue
		}
		id := strings.TrimSuffix(path.Base(entry.Path), ".age")
		kind, fileM, treeM, err := readAnyManifest(backend, identityPath, id)
		if err != nil {
			return nil, fmt.Errorf("snapshot %s: %w", id, err)
		}
		info := SnapshotInfo{UUID: id, Kind: kind}
		if kind == core.KindTree {
			info.CreatedAt = treeM.CreatedAt
			info.Hostname = treeM.Hostname
			detail := fmt.Sprintf("%d files", treeM.FileCount())
			if len(treeM.Paths) > 0 {
				detail += " from " + path.Base(strings.TrimRight(treeM.Paths[0], "/"))
			}
			info.Detail = detail
		} else {
			info.CreatedAt = fileM.CreatedAt
			info.Detail = fileM.OriginalName
		}
		infos = append(infos, info)
	}
	sort.Slice(infos, func(i, j int) bool {
		if infos[i].CreatedAt == infos[j].CreatedAt {
			return infos[i].UUID < infos[j].UUID
		}
		return infos[i].CreatedAt > infos[j].CreatedAt
	})
	return infos, nil
}
```

`snapshots.go`:

```go
package main

import (
	"path/filepath"

	"github.com/pterm/pterm"

	"gloak/internal/flow"
)

type SnapshotsCmd struct {
	Identity string `help:"Identity private key file (defaults to ~/.gloak/identity.txt)." short:"i"`
	Remote   string `help:"Source Rclone URI (e.g., keraaaki:backup_folder or /mnt/backups)." required:"" short:"R"`
}

func (c *SnapshotsCmd) Run() error {
	pterm.Info.Println("Listing snapshots...")
	ident := c.Identity
	if ident == "" {
		ident = filepath.Join(getDefaultDir(), "identity.txt")
	}
	return flow.RunSnapshots(ident, c.Remote)
}
```

`main.go` add:

```go
	Snapshots SnapshotsCmd `cmd:"" help:"List snapshots in the repository."`
```

`main_test.go` add short-tag rows for i and R on SnapshotsCmd.

- [ ] **Step 4: Run tests and smoke help**

Run: `go test ./... && go vet ./... && go build -o /tmp/gloak-p3 . && /tmp/gloak-p3 snapshots --help`

- [ ] **Step 5: Commit**

```bash
git add internal/flow/snapshots.go internal/flow/snapshots_test.go snapshots.go main.go main_test.go
git commit -m "feat(flow): add snapshots list command"
```

---

### Task 6: backup CLI accepts directories + CHANGELOG + final verification

**Files:**
- Modify: `backup.go`
- Modify: `main_test.go`
- Modify: `CHANGELOG.md`
- Modify: `internal/flow/backup_test.go` if needed

**Interfaces:**
- Consumes: `runBackupPath`
- Produces: CLI accepts file or directory path

- [ ] **Step 1: Change BackupCmd field type**

In `backup.go`:

```go
type BackupCmd struct {
	ExistingFile string `arg:"" name:"path" help:"File or directory to back up"`
	Identity     string `short:"i" default:"~/.gloak/identity.txt" help:"Path to identity file"`
	Remote       string `short:"R" required:"" help:"Repository remote, e.g. b2:my-bucket/gloak"`
}
```

Remove `type:"existingfile"` so directories parse. Missing path still errors in flow layer (`os.Stat`).

- [ ] **Step 2: Update short-tag test if field name changed**

The field name stays `ExistingFile` so no main_test.go change needed for short tags. Add a reflection test that the arg tag is `path`:

```go
func TestBackupCmdPathArgTag(t *testing.T) {
	field, ok := reflect.TypeOf(BackupCmd{}).FieldByName("ExistingFile")
	if !ok {
		t.Fatal("field missing")
	}
	if got := field.Tag.Get("arg"); got != "path" {
		t.Fatalf("arg tag = %q", got)
	}
	if strings.Contains(field.Tag.Get("type"), "existingfile") {
		t.Fatal("existingfile type still set; directories must parse")
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./... -count=1`
Expected: PASS

- [ ] **Step 4: CHANGELOG Unreleased Added**

Append after the check bullet:

```markdown
- `backup` accepts a directory and writes a tree snapshot: sorted walk, empty directories preserved, symlinks skipped. Snapshot JSON carries `kind: tree` (or `kind: file` for single-file backups).
- `gloak snapshots` lists every snapshot with kind, creation time, host, and a short detail string.
- `restore` recreates directory trees and accepts `--path <rel>` to restore a subtree only. Tree entry paths are validated against traversal and absolute paths.
```

- [ ] **Step 5: Style grep and full verification**

Run: `grep -nE '—|–|\b(robust|seamless|crucial|comprehensive|landscape|pivotal|leverage|utilize)\b' CHANGELOG.md docs/superpowers/specs/2026-10-12-gloak-tree-snapshots-design.md || echo 'style: clean'`
Expected: clean

Run: `go test ./... && go vet ./... && go build ./... && go build -o /tmp/gloak-final . && /tmp/gloak-final --help`
Expected: all green; help lists snapshots

- [ ] **Step 6: Commit**

```bash
git add backup.go main_test.go CHANGELOG.md
git commit -m "feat(cli): accept directory paths and document tree snapshots"
```

---

### Task 7: Merge to main

- [ ] Run full suite one last time on branch.
- [ ] `git checkout main && git merge phase3-tree && go test ./... && go vet ./... && go build ./... && git branch -d phase3-tree`
- [ ] Do not push. Report completion summary to owner for morning review.

---

## Self-Review notes

- Spec coverage: tree types Task 1, tree backup Task 2, tree restore + filter Task 3, tree verify/check Task 4, snapshots list Task 5, CLI + CHANGELOG Task 6, merge Task 7. Path validation lives in core and is enforced on restore. Single-file path regression covered by existing tests.
- Placeholders: none.
- Type consistency: `core.KindTree`, `readAnyManifest`, `runBackupPath`, `runRestore(..., pathFilter string)`, `ValidateRelPath` used identically across tasks.
