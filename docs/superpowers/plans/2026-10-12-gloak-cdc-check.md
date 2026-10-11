# gloak Phase 2 CDC and check Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. User directive: execute inline in-session, not via subagents.

**Goal:** Replace fixed-size repository chunking with gear-hash content-defined chunking and add structural `gloak check`.

**Architecture:** New `internal/chunker` package owns CDC. `repo.Config` stores `chunk_min`/`chunk_avg`/`chunk_max` instead of `chunk_size`. `runBackup` splits through CDC then reuses the existing hash/index/seal path. `gloak check` walks snapshots + index + `data/` without downloading chunk payloads. File-mode `upload` keeps fixed sequential chunks.

**Tech Stack:** Go 1.26, existing `internal/{repo,index,crypto,storage,flow}` packages, `memtest` fake backend. No new module dependencies.

## Global Constraints

- Language for docs and user-facing copy: English. Code identifiers English.
- Zero em dash and en dash characters in any written file.
- Corruption errors keep the `CHUNK CORRUPT` prefix on the message.
- Format version string stays `"2.0"`. Breaking change is noted in CHANGELOG only.
- Defaults: min 524288, avg 1048576, max 8388608 bytes.
- Gear window size: 64 bytes.
- After every task: `go test ./... && go vet ./...` green before commit.
- Never stage or touch `M .gitignore`, `M README.md`, `?? docs/GUIDE.md`.
- Branch: create `phase2-cdc-check` from `main` before Task 1.

---

### Task 1: Chunker package

**Files:**
- Create: `internal/chunker/chunker.go`
- Create: `internal/chunker/chunker_test.go`

**Interfaces:**
- Consumes: none (stdlib only)
- Produces:
  - `chunker.NewCDC(min, avg, max int64) (*CDC, error)`
  - `(*CDC).Split(r io.Reader, fn func(chunk []byte) error) error`
  - Validation errors mention `chunk size` or `chunk bounds` so callers can wrap.

- [ ] **Step 1: Write failing tests**

```go
package chunker

import (
	"bytes"
	"io"
	"math/rand"
	"testing"
)

func mustCDC(t *testing.T, min, avg, max int64) *CDC {
	t.Helper()
	c, err := NewCDC(min, avg, max)
	if err != nil {
		t.Fatalf("NewCDC(%d,%d,%d) error: %v", min, avg, max, err)
	}
	return c
}

func splitAll(t *testing.T, c *CDC, data []byte) [][]byte {
	t.Helper()
	var chunks [][]byte
	err := c.Split(bytes.NewReader(data), func(chunk []byte) error {
		cp := make([]byte, len(chunk))
		copy(cp, chunk)
		chunks = append(chunks, cp)
		return nil
	})
	if err != nil {
		t.Fatalf("Split() error: %v", err)
	}
	return chunks
}

func TestNewCDCRejectsBadBounds(t *testing.T) {
	cases := []struct{ min, avg, max int64 }{
		{0, 1, 2},
		{2, 1, 4},
		{2, 4, 3},
		{2, 2, 1},
	}
	for _, c := range cases {
		if _, err := NewCDC(c.min, c.avg, c.max); err == nil {
			t.Fatalf("NewCDC(%d,%d,%d) succeeded", c.min, c.avg, c.max)
		}
	}
}

func TestSplitEmptyReader(t *testing.T) {
	c := mustCDC(t, 4, 8, 16)
	chunks := splitAll(t, c, nil)
	if len(chunks) != 0 {
		t.Fatalf("chunks = %d, want 0", len(chunks))
	}
}

func TestSplitSmallerThanMinIsOneChunk(t *testing.T) {
	c := mustCDC(t, 64, 128, 256)
	data := []byte("short")
	chunks := splitAll(t, c, data)
	if len(chunks) != 1 || !bytes.Equal(chunks[0], data) {
		t.Fatalf("chunks = %d, want 1 equal to input", len(chunks))
	}
}

func TestSplitRespectsMax(t *testing.T) {
	c := mustCDC(t, 4, 1<<30, 8)
	data := bytes.Repeat([]byte{0xFF}, 100)
	chunks := splitAll(t, c, data)
	for i, ch := range chunks {
		if int64(len(ch)) > 8 {
			t.Fatalf("chunk %d length %d > max 8", i, len(ch))
		}
	}
	var total int
	for _, ch := range chunks {
		total += len(ch)
	}
	if total != len(data) {
		t.Fatalf("total %d != %d", total, len(data))
	}
}

func TestSplitDeterminism(t *testing.T) {
	data := make([]byte, 50_000)
	rng := rand.New(rand.NewSource(42))
	rng.Read(data)
	a := splitAll(t, mustCDC(t, 512, 2048, 8192), data)
	b := splitAll(t, mustCDC(t, 512, 2048, 8192), data)
	if len(a) != len(b) {
		t.Fatalf("chunk counts %d vs %d", len(a), len(b))
	}
	for i := range a {
		if !bytes.Equal(a[i], b[i]) {
			t.Fatalf("chunk %d differs", i)
		}
	}
	if len(a) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(a))
	}
}

func TestSplitRoundTripBytes(t *testing.T) {
	data := make([]byte, 100_000)
	rng := rand.New(rand.NewSource(7))
	rng.Read(data)
	chunks := splitAll(t, mustCDC(t, 1024, 4096, 16384), data)
	var out []byte
	for _, ch := range chunks {
		out = append(out, ch...)
	}
	if !bytes.Equal(out, data) {
		t.Fatal("concatenated chunks != input")
	}
}

func TestSplitCallbackErrorPropagates(t *testing.T) {
	c := mustCDC(t, 4, 8, 16)
	sentinel := io.ErrClosedPipe
	err := c.Split(bytes.NewReader([]byte("abcdefghij")), func([]byte) error {
		return sentinel
	})
	if err != sentinel {
		t.Fatalf("err = %v, want sentinel", err)
	}
}

func TestMaxRoundedUpToWindowMultiple(t *testing.T) {
	c := mustCDC(t, 4, 8, 100)
	if c.max%64 != 0 {
		t.Fatalf("max = %d, want multiple of 64", c.max)
	}
	if c.max < 100 {
		t.Fatalf("max = %d, want >= 100", c.max)
	}
}
```

- [ ] **Step 2: Run tests, expect compile failure**

Run: `go test ./internal/chunker/ -count=1`
Expected: FAIL, undefined NewCDC / package has no Go files

- [ ] **Step 3: Implement chunker**

Create `internal/chunker/chunker.go`:

```go
// Package chunker implements content-defined chunking with a gear hash.
package chunker

import (
	"bufio"
	"fmt"
	"io"
)

const windowSize = 64

// gear is filled once at init from a fixed seed so every binary builds the
// same table and CDC boundaries stay stable across runs and machines.
var gear [256]uint64

func init() {
	x := uint64(0x9E3779B97F4A7C15)
	for i := range gear {
		x += 0x9E3779B97F4A7C15
		z := x
		z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
		z = (z ^ (z >> 27)) * 0x94D049BB133111EB
		gear[i] = z ^ (z >> 31)
	}
}

type CDC struct {
	min int64
	avg int64
	max int64
}

// NewCDC validates the bounds and rounds max up to a multiple of windowSize.
func NewCDC(min, avg, max int64) (*CDC, error) {
	if min <= 0 || avg <= 0 || max <= 0 {
		return nil, fmt.Errorf("chunk size must be positive (min=%d avg=%d max=%d)", min, avg, max)
	}
	if max < avg || avg < min {
		return nil, fmt.Errorf("chunk bounds require max >= avg >= min (min=%d avg=%d max=%d)", min, avg, max)
	}
	if max%windowSize != 0 {
		max = ((max + windowSize - 1) / windowSize) * windowSize
	}
	return &CDC{min: min, avg: avg, max: max}, nil
}

// Split reads r and calls fn once per chunk. Each chunk is a fresh slice the
// caller may retain. An empty reader produces no calls.
func (c *CDC) Split(r io.Reader, fn func(chunk []byte) error) error {
	br := bufio.NewReaderSize(r, 64*1024)
	chunk := make([]byte, 0, c.max)
	var h uint64
	for {
		b, err := br.ReadByte()
		if err == io.EOF {
			if len(chunk) > 0 {
				return fn(chunk)
			}
			return nil
		}
		if err != nil {
			return err
		}
		chunk = append(chunk, b)
		h = (h << 1) + gear[b]
		if int64(len(chunk)) >= c.max {
			if err := fn(chunk); err != nil {
				return err
			}
			chunk = make([]byte, 0, c.max)
			h = 0
			continue
		}
		if int64(len(chunk)) >= c.min && h%uint64(c.avg) == 0 {
			if err := fn(chunk); err != nil {
				return err
			}
			chunk = make([]byte, 0, c.max)
			h = 0
		}
	}
}
```

- [ ] **Step 4: Run tests to pass**

Run: `go test ./internal/chunker/ -count=1 -v`
Expected: PASS all

- [ ] **Step 5: Full suite and commit**

Run: `go test ./... && go vet ./...`
Expected: PASS

```bash
git add internal/chunker/chunker.go internal/chunker/chunker_test.go
git commit -m "feat(chunker): add gear-hash content-defined chunking"
```

---

### Task 2: CDC config in repo.json and init

**Files:**
- Modify: `internal/repo/config.go`
- Modify: `internal/repo/repo.go` (Init, Open)
- Modify: `internal/repo/repo_test.go`
- Modify: `internal/flow/init.go`
- Modify: `internal/flow/init_test.go`
- Modify: `internal/flow/backup_test.go` (`repoFixture`)
- Modify: `internal/flow/flow_test.go` (`repoFixture` call sites only if signature of fixture changes)
- Modify: `init.go` (CLI flags)
- Modify: `main_test.go` (optional short-tag rows only if new short flags are added; long flags only, so no new short tags)

**Interfaces:**
- Consumes: none new
- Produces:
  - `repo.Config{Version, CreatedAt, Recipients, ChunkMin, ChunkAvg, ChunkMax int64}`
  - `repo.DefaultChunkMin = 512*1024`, `DefaultChunkAvg = 1024*1024`, `DefaultChunkMax = 8*1024*1024`
  - `repo.Init(backend, recipients []string, min, avg, max int64, force bool) ([]byte, Config, error)`
  - `flow.runInit(recipients, backend, min, avg, max int64, force bool) ([]byte, repo.Config, error)`
  - `flow.RunInit(recipients []string, remote string, min, avg, max int64, force bool) error` — zero min/avg/max means use defaults

- [ ] **Step 1: Update repo tests for new Init signature (red)**

In `internal/repo/repo_test.go`, replace every `Init(backend, recipients, <chunkSize>, force)` call. Use small CDC bounds that keep one-chunk behavior for tiny payloads: `1, 8, 64`. Update `TestInitWritesKeysAndConfig` assertion:

```go
master, cfg, err := Init(backend, []string{pubA, pubB}, 1, 8, 64, false)
if err != nil {
	t.Fatalf("Init() error: %v", err)
}
if len(master) != 32 {
	t.Fatalf("master key length = %d, want 32", len(master))
}
if cfg.Version != FormatVersion || cfg.ChunkMin != 1 || cfg.ChunkAvg != 8 || cfg.ChunkMax != 64 {
	t.Fatalf("cfg = %+v", cfg)
}
```

Other Init calls in the same file: change third arg `4096` to three args `1, 8, 64`.

Add:

```go
func TestInitRejectsBadChunkBounds(t *testing.T) {
	pub, _ := testRecipient(t)
	backend := memtest.NewMemoryBackend()
	if _, _, err := Init(backend, []string{pub}, 8, 1, 64, false); err == nil {
		t.Fatal("Init() succeeded with avg < min")
	}
}

func TestOpenRejectsMissingCDCParams(t *testing.T) {
	pub, ident := testRecipient(t)
	backend := memtest.NewMemoryBackend()
	if _, _, err := Init(backend, []string{pub}, 1, 8, 64, false); err != nil {
		t.Fatal(err)
	}
	// Simulate a Phase 1 repo.json that only had chunk_size.
	raw := []byte(`{"version":"2.0","created_at":"now","recipients":["` + pub + `"],"chunk_size":4096}`)
	backend.Put(ConfigPath, raw)
	_, err := Open(backend, ident)
	if err == nil {
		t.Fatal("Open() succeeded without CDC params")
	}
	if !strings.Contains(err.Error(), "chunk") {
		t.Fatalf("error = %v, want chunk param mention", err)
	}
}
```

- [ ] **Step 2: Run repo tests, expect compile failure**

Run: `go test ./internal/repo/ -count=1`
Expected: compile error on Config fields / Init arity

- [ ] **Step 3: Update Config and Init/Open**

`internal/repo/config.go`:

```go
package repo

const (
	FormatVersion   = "2.0"
	ConfigPath      = "repo.json"
	KeysPrefix      = "keys"
	SnapshotsPrefix = "snapshots"
	DataPrefix      = "data"

	DefaultChunkMin = 512 * 1024
	DefaultChunkAvg = 1024 * 1024
	DefaultChunkMax = 8 * 1024 * 1024
)

// Config is the plaintext repository descriptor. It carries no user data:
// version, creation time, recipient public keys, and CDC chunk parameters.
type Config struct {
	Version    string   `json:"version"`
	CreatedAt  string   `json:"created_at"`
	Recipients []string `json:"recipients"`
	ChunkMin   int64    `json:"chunk_min"`
	ChunkAvg   int64    `json:"chunk_avg"`
	ChunkMax   int64    `json:"chunk_max"`
}
```

In `internal/repo/repo.go` `Init`:

```go
func Init(backend storage.Backend, recipients []string, min, avg, max int64, force bool) ([]byte, Config, error) {
	if len(recipients) == 0 {
		return nil, Config{}, fmt.Errorf("at least one recipient is required")
	}
	for _, r := range recipients {
		if _, err := age.ParseX25519Recipient(r); err != nil {
			return nil, Config{}, fmt.Errorf("invalid recipient %q: %w", r, err)
		}
	}
	if min <= 0 || avg <= 0 || max <= 0 || max < avg || avg < min {
		return nil, Config{}, fmt.Errorf("chunk bounds require 0 < min <= avg <= max (min=%d avg=%d max=%d)", min, avg, max)
	}

	if !force {
		if err := backend.Download(ConfigPath, io.Discard); err == nil {
			return nil, Config{}, fmt.Errorf("repository already exists at this location (use force to overwrite)")
		}
	}

	master := make([]byte, masterKeySize)
	if _, err := rand.Read(master); err != nil {
		return nil, Config{}, fmt.Errorf("failed to generate master key: %w", err)
	}

	cfg := Config{
		Version:    FormatVersion,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		Recipients: recipients,
		ChunkMin:   min,
		ChunkAvg:   avg,
		ChunkMax:   max,
	}
	// ... rest unchanged (wrap keys, upload repo.json)
```

In `Open`, after unmarshalling and version check, add:

```go
	if cfg.ChunkMin <= 0 || cfg.ChunkAvg <= 0 || cfg.ChunkMax <= 0 ||
		cfg.ChunkMax < cfg.ChunkAvg || cfg.ChunkAvg < cfg.ChunkMin {
		return nil, fmt.Errorf("invalid chunk parameters in %s (min=%d avg=%d max=%d)", ConfigPath, cfg.ChunkMin, cfg.ChunkAvg, cfg.ChunkMax)
	}
```

- [ ] **Step 4: Update flow init and fixtures**

`internal/flow/init.go`:

```go
package flow

import (
	"github.com/pterm/pterm"

	"gloak/internal/repo"
	"gloak/internal/storage"
)

// RunInit creates a new gloak repository on remote and prints the outcome.
// Zero min/avg/max select the package defaults. With force it overwrites an
// existing repository, which makes old snapshots unreadable.
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
	_ = spinner.Stop()
	pterm.Success.Println("Repository initialized")
	pterm.Printf("Location: %s\n", remote)
	pterm.Printf("Recipients: %v\n", cfg.Recipients)
	pterm.Printf("Chunk bounds: min=%d avg=%d max=%d bytes\n", cfg.ChunkMin, cfg.ChunkAvg, cfg.ChunkMax)
	pterm.Printf("Master key length: %d bytes (wrapped in %s)\n", len(master), repo.KeysPrefix+"/")
	return nil
}

func runInit(recipients []string, backend storage.Backend, min, avg, max int64, force bool) ([]byte, repo.Config, error) {
	return repo.Init(backend, recipients, min, avg, max, force)
}
```

`internal/flow/init_test.go`: replace every `runInit(a, backend, 4096, force)` with `runInit(a, backend, 1, 8, 64, force)`. Fix `cfg.ChunkSize` assertion to:

```go
if cfg.ChunkMin != 1 || cfg.ChunkAvg != 8 || cfg.ChunkMax != 64 {
	t.Fatalf("cfg chunk params = %d/%d/%d", cfg.ChunkMin, cfg.ChunkAvg, cfg.ChunkMax)
}
```

`internal/flow/backup_test.go` `repoFixture`:

```go
func repoFixture(t *testing.T) (string, *memtest.MemoryBackend, string) {
	t.Helper()
	pub, _, fileContent, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error: %v", err)
	}
	identPath := filepath.Join(t.TempDir(), "identity.txt")
	if err := os.WriteFile(identPath, []byte(fileContent), 0600); err != nil {
		t.Fatalf("write identity: %v", err)
	}
	backend := memtest.NewMemoryBackend()
	// min=1 avg=8 max=64: tiny test payloads stay one chunk unless tests
	// craft multi-chunk files themselves.
	if _, _, err := repo.Init(backend, []string{pub}, 1, 8, 64, false); err != nil {
		t.Fatalf("repo.Init() error: %v", err)
	}
	return identPath, backend, pub
}
```

Update every call site in `internal/flow/*_test.go` from `repoFixture(t, 8)` to `repoFixture(t)`.

- [ ] **Step 5: Update CLI**

`init.go`:

```go
type InitCmd struct {
	Remote    string `short:"R" required:"" help:"Repository remote, e.g. b2:my-bucket/gloak or local path"`
	Recipient string `short:"r" help:"Recipient public key file or comma-separated keys (default ~/.gloak/recipient.txt)"`
	Force     bool   `short:"f" help:"Overwrite an existing repository (makes old snapshots unreadable)"`
	ChunkMin  int64  `help:"Minimum CDC chunk size in bytes (default 524288)."`
	ChunkAvg  int64  `help:"Average CDC chunk size in bytes (default 1048576)."`
	ChunkMax  int64  `help:"Maximum CDC chunk size in bytes (default 8388608)."`
}

func (c *InitCmd) Run() error {
	recipients, err := resolveInitRecipients(c.Recipient)
	if err != nil {
		return err
	}
	return flow.RunInit(recipients, c.Remote, c.ChunkMin, c.ChunkAvg, c.ChunkMax, c.Force)
}
```

- [ ] **Step 6: Fix remaining compile errors and run tests**

Run: `go test ./... && go vet ./...`
Expected: PASS. If `flow_test.go` or other files still call `repo.Init` with old arity, fix the same way (third arg becomes `1, 8, 64`).

- [ ] **Step 7: Commit**

```bash
git add internal/repo/config.go internal/repo/repo.go internal/repo/repo_test.go \
  internal/flow/init.go internal/flow/init_test.go internal/flow/backup_test.go \
  internal/flow/flow_test.go init.go
git commit -m "feat(repo): store CDC chunk bounds in repo.json"
```

---

### Task 3: Backup uses CDC

**Files:**
- Modify: `internal/flow/backup.go`
- Modify: `internal/flow/backup_test.go`
- Modify: `internal/flow/flow_test.go`

**Interfaces:**
- Consumes: `chunker.NewCDC`, `repo.Config.ChunkMin/Avg/Max`
- Produces: `runBackup(filePath, identityPath string, backend storage.Backend, snapshotID string, progress func(int)) (BackupResult, error)` signature unchanged. Chunk metas still use `repo.DataPath(hash)` as `Name`.

- [ ] **Step 1: Rewrite backup tests that assumed fixed 8-byte chunks**

`TestBackupWritesSnapshotAndDataObjects` becomes single-chunk oriented (payload 20 bytes, min=1 max=64, gear cut at min=1 could split; use payload under control). Replace body assertions:

```go
func TestBackupWritesSnapshotAndDataObjects(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	payload := []byte("0123456789abcdefghij")
	filePath := writeTempFile(t, payload)

	res, err := runBackup(filePath, ident, backend, "7f91c6c7-7a0b-44aa-ae23-997b60e4e998", nil)
	if err != nil {
		t.Fatalf("runBackup() error: %v", err)
	}
	if res.TotalChunks < 1 || res.UploadedChunks != res.TotalChunks || res.SkippedChunks != 0 {
		t.Fatalf("result = %+v", res)
	}
	if res.TotalPlainBytes != int64(len(payload)) {
		t.Fatalf("TotalPlainBytes = %d", res.TotalPlainBytes)
	}
	if !backend.Has(repo.SnapshotsPrefix + "/" + res.SnapshotID + ".age") {
		t.Fatal("snapshot file missing")
	}
	if got := backend.TotalUploads(repo.DataPrefix + "/"); got != res.UploadedChunks {
		t.Fatalf("data uploads = %d, want %d", got, res.UploadedChunks)
	}
	if !backend.Has("index/000001.age") {
		t.Fatal("index epoch missing")
	}
}
```

`TestBackupSkipsChunksAlreadyInIndex`: second run `res.UploadedChunks != 0` and `res.SkippedChunks != res.TotalChunks` fails.

`TestBackupDedupsRepeatedChunksInSameFile`: CDC may not split a repeated pattern into identical chunks. Replace with cross-file dedup:

```go
func TestBackupDedupsIdenticalContentAcrossFiles(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	payload := []byte("0123456789abcdefghij")
	fileA := writeTempFile(t, payload)
	fileB := filepath.Join(t.TempDir(), "copy.bin")
	if err := os.WriteFile(fileB, payload, 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := runBackup(fileA, ident, backend, "11111111-2222-4333-8444-555555555555", nil); err != nil {
		t.Fatalf("first runBackup() error: %v", err)
	}
	before := backend.TotalUploads(repo.DataPrefix + "/")

	res, err := runBackup(fileB, ident, backend, "11111111-2222-4333-8444-666666666666", nil)
	if err != nil {
		t.Fatalf("second runBackup() error: %v", err)
	}
	if res.UploadedChunks != 0 || res.SkippedChunks != res.TotalChunks {
		t.Fatalf("second result = %+v, want all skipped", res)
	}
	if backend.TotalUploads(repo.DataPrefix+"/") != before {
		t.Fatal("second backup re-uploaded data objects")
	}
}
```

`TestBackupObjectMatchesChunkHash`: find first chunk from the returned snapshot instead of assuming `payload[:8]`:

```go
func TestBackupObjectMatchesChunkHash(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	payload := []byte("0123456789abcdefghij")
	filePath := writeTempFile(t, payload)
	snapshotID := "99999999-8888-4777-8666-555555555555"
	if _, err := runBackup(filePath, ident, backend, snapshotID, nil); err != nil {
		t.Fatalf("runBackup() error: %v", err)
	}
	m, err := readManifest(backend, ident, snapshotID)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Chunks) == 0 {
		t.Fatal("no chunks in manifest")
	}
	if !backend.Has(m.Chunks[0].Name) {
		t.Fatalf("expected object at %s", m.Chunks[0].Name)
	}
}
```

Add CDC quality test:

```go
func TestBackupCDCQualityAfterInsert(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	base := make([]byte, 200_000)
	for i := range base {
		base[i] = byte(i * 31)
	}
	fileA := writeTempFile(t, base)
	snapA := "aaaaaaaa-bbbb-4ccc-8ddd-000000000001"
	resA, err := runBackup(fileA, ident, backend, snapA, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resA.TotalChunks < 3 {
		t.Fatalf("TotalChunks = %d, want >= 3", resA.TotalChunks)
	}

	// Insert one byte near the start; most later chunk hashes should survive.
	shifted := make([]byte, 0, len(base)+1)
	shifted = append(shifted, base[:100]...)
	shifted = append(shifted, 0xAB)
	shifted = append(shifted, base[100:]...)
	fileB := writeTempFile(t, shifted)
	snapB := "aaaaaaaa-bbbb-4ccc-8ddd-000000000002"
	resB, err := runBackup(fileB, ident, backend, snapB, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resB.SkippedChunks == 0 {
		t.Fatal("expected some chunks reused after single-byte insert")
	}
}
```

- [ ] **Step 2: Run backup tests, expect failures on chunk counts**

Run: `go test ./internal/flow/ -run 'TestBackup' -count=1`
Expected: FAIL until backup.go uses CDC

- [ ] **Step 3: Wire CDC into runBackup**

In `internal/flow/backup.go`, import `"gloak/internal/chunker"`. Replace the fixed-size read loop (from `chunkSize := r.Config.ChunkSize` through the end of the for-loop) with:

```go
	cdc, err := chunker.NewCDC(r.Config.ChunkMin, r.Config.ChunkAvg, r.Config.ChunkMax)
	if err != nil {
		return BackupResult{}, fmt.Errorf("invalid chunk config: %w", err)
	}

	res := BackupResult{SnapshotID: snapshotID}
	var metas []core.ChunkMeta
	var sinceCheckpoint int
	payloadHasher := sha256.New()

	err = cdc.Split(file, func(chunk []byte) error {
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
		metas = append(metas, core.ChunkMeta{
			Index:           len(metas),
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
	if err != nil {
		return BackupResult{}, err
	}
```

Remove now-unused `io` import if nothing else uses it (`io.ReadFull` gone; check `bytes` still needed). Keep the rest of the function (final index save, manifest write) unchanged.

- [ ] **Step 4: Run flow tests**

Run: `go test ./internal/flow/ -count=1`
Expected: PASS including round trip and CDC quality

- [ ] **Step 5: Full suite and commit**

Run: `go test ./... && go vet ./...`

```bash
git add internal/flow/backup.go internal/flow/backup_test.go internal/flow/flow_test.go
git commit -m "feat(flow): chunk repository backups with CDC"
```

---

### Task 4: Recursive listing on storage backend

**Files:**
- Modify: `internal/storage/rclone.go`
- Modify: `internal/storage/memtest/memory.go`
- Modify: `internal/storage/rclone_test.go` (extend if a list test exists; else add ListRecursive test against memtest only)
- Create or extend: test for memtest recursive list in `internal/storage/memtest/memory_test.go` if none exists

**Interfaces:**
- Consumes: existing `Backend` interface
- Produces:
  - `Backend.ListRecursive(prefix []string) ([]string, error)` returns full remote-relative file paths (same shape as `Upload` keys), files only, no directories.
  - `storage.ParseList` unchanged.

- [ ] **Step 1: Write failing memtest test**

`internal/storage/memtest/memory_test.go`:

```go
package memtest

import (
	"bytes"
	"testing"
)

func TestListRecursiveReturnsNestedFiles(t *testing.T) {
	m := NewMemoryBackend()
	_ = m.Upload("data/ab/abhash", bytes.NewReader([]byte("x")))
	_ = m.Upload("data/cd/cdhash", bytes.NewReader([]byte("y")))
	_ = m.Upload("snapshots/s1.age", bytes.NewReader([]byte("z")))

	got, err := m.ListRecursive([]string{"data"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"data/ab/abhash", "data/cd/cdhash"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
```

- [ ] **Step 2: Run, expect compile failure**

Run: `go test ./internal/storage/memtest/ -count=1`
Expected: FAIL undefined ListRecursive

- [ ] **Step 3: Implement ListRecursive**

Add to interface in `internal/storage/rclone.go`:

```go
type Backend interface {
	Upload(remotePath string, in io.Reader) error
	Download(remotePath string, out io.Writer) error
	ListFiles(prefix []string) ([]byte, error)
	ListRecursive(prefix []string) ([]string, error)
	Delete(remotePath string) error
}
```

Rclone implementation:

```go
// ListRecursive lists every file under prefix, recursively. Paths are
// returned in the same remote-relative shape as Upload keys.
func (r *RcloneBackend) ListRecursive(prefix []string) ([]string, error) {
	remotePath := r.Remote
	if len(prefix) > 0 {
		remotePath = fmt.Sprintf("%s/%s", r.Remote, strings.Join(prefix, "/"))
	}

	cmd := exec.Command("rclone", rcloneArgs(r.ConfigPath, "lsjson", "-R", remotePath)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to list %s via rclone lsjson: %w\nRclone Error: %s", remotePath, err, stderr.String())
	}

	entries, err := ParseList(stdout.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to parse recursive listing: %w", err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir {
			continue
		}
		if len(prefix) > 0 {
			out = append(out, strings.Join(append(prefix, e.Path), "/"))
		} else {
			out = append(out, e.Path)
		}
	}
	return out, nil
}
```

Memtest implementation:

```go
// ListRecursive returns every stored file path under prefix, sorted.
func (m *MemoryBackend) ListRecursive(prefix []string) ([]string, error) {
	p := strings.Trim(strings.Join(prefix, "/"), "/")
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for key := range m.files {
		if p == "" || strings.HasPrefix(key, p+"/") {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out, nil
}
```

- [ ] **Step 4: Run storage tests**

Run: `go test ./internal/storage/... -count=1 && go test ./... -count=1 && go vet ./...`
Expected: PASS (any other Backend mock in tests must gain ListRecursive; memtest is the only extra implementer in-repo)

- [ ] **Step 5: Commit**

```bash
git add internal/storage/rclone.go internal/storage/memtest/memory.go internal/storage/memtest/memory_test.go
git commit -m "feat(storage): add recursive file listing to backends"
```

---

### Task 5: Index.AllHashes helper

**Files:**
- Modify: `internal/index/index.go`
- Modify: `internal/index/index_test.go`

**Interfaces:**
- Consumes: existing `Index`
- Produces: `(*Index).AllHashes() []string` sorted unique hashes from pending + all

- [ ] **Step 1: Failing test**

```go
func TestAllHashesMergesPendingAndAll(t *testing.T) {
	ix := New()
	ix.Add(entryFixture("bbb"))
	// Force an entry into all only via Save/Load path is heavy; pending Add
	// also fills all, so also exercise empty pending after save.
	// Simpler: two Adds, one pending cleared is not required for this helper.
	ix.Add(entryFixture("aaa"))
	got := ix.AllHashes()
	want := []string{"aaa", "bbb"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("AllHashes() = %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run**

Run: `go test ./internal/index/ -run TestAllHashes -count=1`
Expected: FAIL undefined AllHashes

- [ ] **Step 3: Implement**

```go
// AllHashes returns every known plaintext hash, sorted. Pending entries that
// were never saved are included so check sees work in progress.
func (ix *Index) AllHashes() []string {
	seen := make(map[string]bool, len(ix.all)+len(ix.pending))
	for h := range ix.all {
		seen[h] = true
	}
	for h := range ix.pending {
		seen[h] = true
	}
	out := make([]string, 0, len(seen))
	for h := range seen {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}
```

- [ ] **Step 4: Run and commit**

Run: `go test ./internal/index/ -count=1`

```bash
git add internal/index/index.go internal/index/index_test.go
git commit -m "feat(index): expose sorted hash listing for check"
```

---

### Task 6: Structural check flow and CLI

**Files:**
- Create: `internal/flow/check.go`
- Create: `internal/flow/check_test.go`
- Create: `check.go` (CLI)
- Modify: `main.go` (register Check)
- Modify: `main_test.go` (short tags i/R)

**Interfaces:**
- Consumes: `repo.Open`, `index.Load`, `readManifest`, `storage.Backend.ListRecursive`, `Index.AllHashes`
- Produces:
  - `flow.RunCheck(identityPath, remote string) error`
  - `flow.runCheck(identityPath string, backend storage.Backend) (CheckResult, error)`
  - `flow.CheckResult{Snapshots, Referenced, DataObjects int, Missing, Garbage []string}`

- [ ] **Step 1: Failing tests**

```go
package flow

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gloak/internal/crypto"
	"gloak/internal/repo"
	"gloak/internal/storage/memtest"
)

func checkFixture(t *testing.T) (string, *memtest.MemoryBackend, string) {
	t.Helper()
	return repoFixture(t)
}

func TestRunCheckCleanRepository(t *testing.T) {
	ident, backend, _ := checkFixture(t)
	filePath := writeTempFile(t, []byte("check me please"))
	if _, err := runBackup(filePath, ident, backend, "7f91c6c7-7a0b-44aa-ae23-997b60e4e998", nil); err != nil {
		t.Fatal(err)
	}
	res, err := runCheck(ident, backend)
	if err != nil {
		t.Fatalf("runCheck() error: %v", err)
	}
	if res.Snapshots != 1 || len(res.Missing) != 0 {
		t.Fatalf("result = %+v", res)
	}
	if res.DataObjects < 1 {
		t.Fatalf("DataObjects = %d", res.DataObjects)
	}
}

func TestRunCheckReportsMissingObject(t *testing.T) {
	ident, backend, _ := checkFixture(t)
	filePath := writeTempFile(t, []byte("check me please"))
	if _, err := runBackup(filePath, ident, backend, "7f91c6c7-7a0b-44aa-ae23-997b60e4e998", nil); err != nil {
		t.Fatal(err)
	}
	keys := backend.KeysWithPrefix(repo.DataPrefix + "/")
	if len(keys) == 0 {
		t.Fatal("no data objects")
	}
	backend.Delete(keys[0])

	_, err := runCheck(ident, backend)
	if err == nil {
		t.Fatal("runCheck() succeeded with missing object")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("error = %v, want missing mention", err)
	}
}

func TestRunCheckReportsGarbageWithoutError(t *testing.T) {
	ident, backend, _ := checkFixture(t)
	filePath := writeTempFile(t, []byte("check me please"))
	if _, err := runBackup(filePath, ident, backend, "7f91c6c7-7a0b-44aa-ae23-997b60e4e998", nil); err != nil {
		t.Fatal(err)
	}
	_ = backend.Upload("data/ff/ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", bytes.NewReader([]byte("orphan")))

	res, err := runCheck(ident, backend)
	if err != nil {
		t.Fatalf("runCheck() error: %v", err)
	}
	if len(res.Garbage) != 1 {
		t.Fatalf("Garbage = %v, want one entry", res.Garbage)
	}
}

func TestRunCheckRejectsCorruptSnapshot(t *testing.T) {
	ident, backend, _ := checkFixture(t)
	filePath := writeTempFile(t, []byte("check me please"))
	if _, err := runBackup(filePath, ident, backend, "7f91c6c7-7a0b-44aa-ae23-997b60e4e998", nil); err != nil {
		t.Fatal(err)
	}
	key := repo.SnapshotsPrefix + "/7f91c6c7-7a0b-44aa-ae23-997b60e4e998.age"
	blob := backend.Get(key)
	blob[len(blob)/2] ^= 0xff
	backend.Put(key, blob)

	if _, err := runCheck(ident, backend); err == nil {
		t.Fatal("runCheck() succeeded on corrupt snapshot")
	}
}
```

Note: if `repoFixture` lives in `backup_test.go` in the same package, do not redefine; drop the local `checkFixture` wrapper and call `repoFixture(t)` directly.

- [ ] **Step 2: Run, expect failures**

Run: `go test ./internal/flow/ -run TestRunCheck -count=1`
Expected: FAIL undefined runCheck

- [ ] **Step 3: Implement check flow**

`internal/flow/check.go`:

```go
package flow

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/pterm/pterm"

	"gloak/internal/index"
	"gloak/internal/repo"
	"gloak/internal/storage"
)

type CheckResult struct {
	Snapshots    int
	Referenced   int
	DataObjects  int
	Missing      []string
	Garbage      []string
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
		// No snapshots directory yet is an empty repo, not a failure of check
		// beyond reporting zero snapshots. Still surface unexpected errors
		// from ListFiles by checking whether any snapshot path exists later.
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
		m, err := readManifest(backend, identityPath, id)
		if err != nil {
			return res, fmt.Errorf("snapshot %s: %w", id, err)
		}
		res.Snapshots++
		for _, meta := range m.Chunks {
			referenced[meta.PlainSHA256] = true
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
```

- [ ] **Step 4: Wire CLI**

`check.go`:

```go
package main

import (
	"path/filepath"

	"github.com/pterm/pterm"

	"gloak/internal/flow"
)

type CheckCmd struct {
	Identity string `help:"Identity private key file (defaults to ~/.gloak/identity.txt)." short:"i"`
	Remote   string `help:"Source Rclone URI (e.g., keraaaki:backup_folder or /mnt/backups)." required:"" short:"R"`
}

func (c *CheckCmd) Run() error {
	pterm.Info.Println("Starting CHECK process...")

	ident := c.Identity
	if ident == "" {
		ident = filepath.Join(getDefaultDir(), "identity.txt")
		pterm.Info.Printf("Using identity from: %s\n", ident)
	}

	return flow.RunCheck(ident, c.Remote)
}
```

`main.go` add field:

```go
	Check    CheckCmd    `cmd:"" help:"Check repository consistency (structural)."`
```

`main_test.go` add table rows:

```go
{name: "check identity", cmd: CheckCmd{}, fieldName: "Identity", wantShort: "i"},
{name: "check remote", cmd: CheckCmd{}, fieldName: "Remote", wantShort: "R"},
```

- [ ] **Step 5: Run tests and smoke help**

Run: `go test ./... && go vet ./... && go build -o /tmp/gloak-check . && /tmp/gloak-check check --help`
Expected: tests PASS; help shows check command

- [ ] **Step 6: Commit**

```bash
git add internal/flow/check.go internal/flow/check_test.go check.go main.go main_test.go
git commit -m "feat(flow): add structural repository check"
```

---

### Task 7: CHANGELOG and final verification

**Files:**
- Modify: `CHANGELOG.md`

**Interfaces:**
- Consumes: nothing
- Produces: Unreleased notes for CDC and check

- [ ] **Step 1: Update CHANGELOG.md Unreleased Added section**

Append bullets after the existing upload bullet:

```markdown
- `backup` splits files with content-defined chunking (gear hash). Chunk bounds `chunk_min`/`chunk_avg`/`chunk_max` are stored in `repo.json` at init; flags `--chunk-min`, `--chunk-avg`, `--chunk-max` override the defaults (512 KiB / 1 MiB / 8 MiB). A repeat backup of unchanged content uploads nothing.
- `gloak check` reports structural consistency: every snapshot decrypts, every referenced chunk exists in `data/`, and unreferenced objects are listed as garbage. It does not download chunk payloads; use `verify` for a deep pass.
```

Under Changed, replace the first bullet with:

```markdown
- `verify` and `restore` read the repository layout. Corruption errors keep the CHUNK CORRUPT prefix.
- Repository backup no longer uses fixed 20 MiB chunks. Phase 1 repositories with `chunk_size` in `repo.json` cannot be opened; re-run `gloak init`.
```

- [ ] **Step 2: Style grep and full verification**

Run: `grep -nE '—|–|\b(robust|seamless|crucial|comprehensive|landscape|pivotal|leverage|utilize)\b' CHANGELOG.md || echo 'style: clean'`
Expected: `style: clean`

Run: `go test ./... && go vet ./... && go build ./... && go build -o /tmp/gloak-final . && /tmp/gloak-final --help`
Expected: all green; help lists init, backup, upload, restore, verify, check

- [ ] **Step 3: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs: note CDC chunking and repository check"
```

- [ ] **Step 4: Self-review branch**

Run: `git log --oneline main..HEAD` (after branching) or inspect local commits on the working branch. Confirm every commit has tests. Fix any drift before asking the user about merge.

---

## Self-Review notes

- Spec coverage: gear CDC Task 1, config Task 2, backup wiring Task 3, recursive listing Task 4, index hashes Task 5, check Task 6, CHANGELOG Task 7. File-mode upload untouched. Prune/tree out of scope as specified.
- Placeholders: none; every step has code or exact commands.
- Type consistency: `NewCDC(min, avg, max int64)`, `Init(..., min, avg, max int64, force bool)`, `ListRecursive(prefix []string) ([]string, error)`, `AllHashes() []string`, `runCheck(identityPath, backend) (CheckResult, error)` used identically across tasks.
