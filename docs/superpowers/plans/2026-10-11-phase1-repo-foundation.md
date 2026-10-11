# gloak Phase 1 (Repository Foundation) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the single-file `filesystem-v1` backup path with the repository model from the spec: `gloak init` creates a repo with a wrapped master key, `gloak backup`/`verify`/`restore` operate on content-addressed encrypted chunks with an epoch index, all under `filesystem-v2`.

**Architecture:** A master key (32 random bytes) is wrapped to each age recipient under `keys/`. Chunks are sealed with HKDF-derived per-chunk keys (zstd then XChaCha20-Poly1305), stored content-addressed under `data/{hh}/{sha256}`. An encrypted epoch index records every uploaded chunk so repeat backups upload nothing. Snapshots (the existing single-file manifest, re-versioned) live under `snapshots/` and act as the completion marker. The `storage.Backend` interface stays; a shared memory test double moves to `internal/storage/memtest` so repo, index, and flow packages reuse it.

**Tech Stack:** Go 1.26.3, `filippo.io/age` v1.3.1, `github.com/klauspost/compress` (zstd), `golang.org/x/crypto` (XChaCha20-Poly1305), stdlib `crypto/hkdf`, `github.com/alecthomas/kong`, `github.com/pterm/pterm`.

## Global Constraints

- Module `gloak`, Go 1.26.3, single static binary; rclone stays the only external runtime dependency.
- New dependencies: `github.com/klauspost/compress` and `golang.org/x/crypto` promoted to direct. No other new dependencies.
- Alpha break: `filesystem-v1` (`{uuid}/chunk_00000`, `manifest.age`, `locator.json`) is removed in this phase. No migration tool.
- Envelope constants: HKDF info string `"gloak-chunk-v1"`, salt = raw bytes of the plaintext SHA-256 (hex-decoded), key length 32, nonce 24 bytes, AAD = plaintext SHA-256 hex string, zstd `SpeedDefault` (level 3).
- Ordering rules: upload `keys/*` before `repo.json`; upload a chunk object before recording it in the index; save the index epoch before uploading the snapshot; snapshot upload is the last operation of a backup.
- Default chunk size 20 MB (`core.ChunkSize`), stored in `repo.json` and read by backup.
- Restore keeps the existing filename safety check (`filepath.Base(name) == name`) plus a new guard that manifest chunk paths start with `data/` and contain no `..`.
- Corruption errors keep the `CHUNK CORRUPT` prefix because the README and tests reference it.
- Every task ends green: `go test ./...` from the repo root. Never push.

## Spec Amendment (applies to this plan)

The spec's storage-layout comment says `repo.json` holds "recipient fingerprints". Planning showed that breaks backup: sealing a snapshot needs the full recipient public keys, which would otherwise be unavailable after init. This plan stores the full recipient strings in `repo.json` (public keys are non-secret, so the spec's constraint still holds) and uses fingerprints only for `keys/{fingerprint}.age` file names. Task 10 updates the spec comment to match.

## Pre-Execution Note

The working tree has uncommitted changes (`.gitignore`, `README.md`, `docs/GUIDE.md`). They are out of scope: do not stage, revert, or modify them. Task 10 edits `README.md`, so ask the user how to handle their pending README changes before that task.

---

### Task 1: Storage backend removes the hardcoded rclone config path

**Files:**
- Modify: `internal/storage/rclone.go`
- Create: `internal/storage/rclone_test.go`

**Interfaces:**
- Consumes: none (existing package).
- Produces: `func rcloneArgs(configPath string, base ...string) []string` used by all `RcloneBackend` methods; `RcloneBackend.ConfigPath` becomes optional, empty means "let rclone use its own default config".

- [ ] **Step 1: Write failing tests for the arg builder**

Create `internal/storage/rclone_test.go`:

```go
package storage

import (
	"reflect"
	"testing"
)

func TestRcloneArgsOmitsConfigWhenEmpty(t *testing.T) {
	got := rcloneArgs("", "cat", "remote:file")
	want := []string{"cat", "remote:file"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rcloneArgs() = %v, want %v", got, want)
	}
}

func TestRcloneArgsAppendsConfigWhenSet(t *testing.T) {
	got := rcloneArgs("/etc/rclone.conf", "rcat", "remote:file")
	want := []string{"rcat", "remote:file", "--config", "/etc/rclone.conf"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rcloneArgs() = %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/storage/ -run TestRcloneArgs -v`
Expected: FAIL, undefined: rcloneArgs

- [ ] **Step 3: Implement rcloneArgs and drop the hardcoded path**

In `internal/storage/rclone.go`, replace `NewRcloneBackend`:

```go
func NewRcloneBackend(remote string) *RcloneBackend {
	return &RcloneBackend{Remote: remote}
}

func rcloneArgs(configPath string, base ...string) []string {
	args := append([]string{}, base...)
	if configPath != "" {
		args = append(args, "--config", configPath)
	}
	return args
}
```

Add doc comment on the struct field: `// ConfigPath is optional. When empty, rclone uses its own default config.`

Rewrite each method to use the helper, keeping the original error strings:

```go
func (r *RcloneBackend) Upload(remotePath string, in io.Reader) error {
	fullDest := fmt.Sprintf("%s/%s", r.Remote, remotePath)
	cmd := exec.Command("rclone", rcloneArgs(r.ConfigPath, "rcat", fullDest)...)
	cmd.Stdin = in
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to upload to %s via rclone rcat: %w\nRclone Error: %s", fullDest, err, stderr.String())
	}
	return nil
}
```

Same pattern for the other three: `Download` uses `"cat", fullSrc`, `ListFiles` uses `"lsjson", remotePath`, `Delete` uses `"purge", fullPath`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/storage/ -v && go test ./...`
Expected: PASS; the whole suite stays green because old code never set `ConfigPath`, so rclone already fell back to its default.

- [ ] **Step 5: Commit**

```bash
git add internal/storage/rclone.go internal/storage/rclone_test.go
git commit -m "fix(storage): stop hardcoding rclone config path"
```

---

### Task 2: Chunk envelope (HKDF + zstd + XChaCha20-Poly1305)

**Files:**
- Modify: `go.mod`, `go.sum` (via `go get`)
- Create: `internal/crypto/envelope.go`
- Create: `internal/crypto/envelope_test.go`

**Interfaces:**
- Consumes: none.
- Produces (every later task relies on these exact signatures):
  - `func SealChunk(masterKey []byte, plainSHA256Hex string, plain []byte) ([]byte, error)` returns `nonce || ciphertext` (24-byte nonce prefix).
  - `func OpenChunk(masterKey []byte, plainSHA256Hex string, sealed []byte) ([]byte, error)` returns the original plaintext.

- [ ] **Step 1: Add dependencies**

Run:
```bash
go get github.com/klauspost/compress@latest golang.org/x/crypto@latest
go mod tidy
```
Expected: `go.mod` lists `github.com/klauspost/compress` under direct requires; `golang.org/x/crypto` loses its `// indirect` comment.

- [ ] **Step 2: Write failing tests**

Create `internal/crypto/envelope_test.go`:

```go
package crypto

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestSealOpenChunkRoundTrip(t *testing.T) {
	master := make([]byte, 32)
	if _, err := rand.Read(master); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	plain := []byte("payload with enough bytes to compress meaningfully, payload payload payload")
	sum := sha256Hex(plain)

	sealed, err := SealChunk(master, sum, plain)
	if err != nil {
		t.Fatalf("SealChunk() error: %v", err)
	}
	got, err := OpenChunk(master, sum, sealed)
	if err != nil {
		t.Fatalf("OpenChunk() error: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("OpenChunk() = %q, want %q", got, plain)
	}
}

func TestOpenChunkRejectsTamperedCiphertext(t *testing.T) {
	master := []byte("0123456789abcdef0123456789abcdef")
	plain := []byte("important data")
	sum := sha256Hex(plain)
	sealed, err := SealChunk(master, sum, plain)
	if err != nil {
		t.Fatalf("SealChunk() error: %v", err)
	}
	sealed[len(sealed)-1] ^= 0xff
	if _, err := OpenChunk(master, sum, sealed); err == nil {
		t.Fatal("OpenChunk() succeeded with tampered ciphertext")
	}
}

func TestOpenChunkRejectsWrongChunkHash(t *testing.T) {
	master := []byte("0123456789abcdef0123456789abcdef")
	plain := []byte("important data")
	sealed, err := SealChunk(master, sha256Hex(plain), plain)
	if err != nil {
		t.Fatalf("SealChunk() error: %v", err)
	}
	wrongSum := sha256Hex([]byte("other data"))
	if _, err := OpenChunk(master, wrongSum, sealed); err == nil {
		t.Fatal("OpenChunk() succeeded with wrong chunk hash")
	}
}

func TestOpenChunkRejectsWrongMasterKey(t *testing.T) {
	master := []byte("0123456789abcdef0123456789abcdef")
	other := []byte("fedcba9876543210fedcba9876543210")
	plain := []byte("important data")
	sum := sha256Hex(plain)
	sealed, err := SealChunk(master, sum, plain)
	if err != nil {
		t.Fatalf("SealChunk() error: %v", err)
	}
	if _, err := OpenChunk(other, sum, sealed); err == nil {
		t.Fatal("OpenChunk() succeeded with wrong master key")
	}
}

func TestSealChunkRejectsInvalidChunkHash(t *testing.T) {
	master := []byte("0123456789abcdef0123456789abcdef")
	_, err := SealChunk(master, "not-hex", []byte("x"))
	if err == nil {
		t.Fatal("SealChunk() succeeded with invalid chunk hash")
	}
	if !strings.Contains(err.Error(), "invalid chunk hash") {
		t.Fatalf("SealChunk() error = %v, want invalid chunk hash", err)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/crypto/ -run 'Seal|Open' -v`
Expected: FAIL, undefined: SealChunk

- [ ] **Step 4: Implement the envelope**

Create `internal/crypto/envelope.go`:

```go
package crypto

import (
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/klauspost/compress/zstd"
	"golang.org/x/crypto/chacha20poly1305"
)

const chunkKeyInfo = "gloak-chunk-v1"

func deriveChunkKey(masterKey []byte, plainSHA256Hex string) ([]byte, error) {
	salt, err := hex.DecodeString(plainSHA256Hex)
	if err != nil || len(salt) != sha256.Size {
		return nil, fmt.Errorf("invalid chunk hash: %q", plainSHA256Hex)
	}
	return hkdf.Key(sha256.New, masterKey, salt, chunkKeyInfo, chacha20poly1305.KeySize)
}

// SealChunk compresses plain with zstd, then encrypts it with XChaCha20-Poly1305
// under a key derived from masterKey. Output layout: 24-byte nonce || ciphertext.
func SealChunk(masterKey []byte, plainSHA256Hex string, plain []byte) ([]byte, error) {
	key, err := deriveChunkKey(masterKey, plainSHA256Hex)
	if err != nil {
		return nil, err
	}
	compressed, err := zstdEncode(plain)
	if err != nil {
		return nil, fmt.Errorf("failed to compress chunk: %w", err)
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("failed to init cipher: %w", err)
	}
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}
	sealed := make([]byte, 0, len(nonce)+len(compressed)+aead.Overhead())
	sealed = append(sealed, nonce...)
	sealed = aead.Seal(sealed, nonce, compressed, []byte(plainSHA256Hex))
	return sealed, nil
}

// OpenChunk reverses SealChunk and verifies both the AEAD tag and the chunk hash.
func OpenChunk(masterKey []byte, plainSHA256Hex string, sealed []byte) ([]byte, error) {
	key, err := deriveChunkKey(masterKey, plainSHA256Hex)
	if err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("failed to init cipher: %w", err)
	}
	if len(sealed) < chacha20poly1305.NonceSizeX+aead.Overhead() {
		return nil, fmt.Errorf("chunk too short: %d bytes", len(sealed))
	}
	nonce := sealed[:chacha20poly1305.NonceSizeX]
	ciphertext := sealed[chacha20poly1305.NonceSizeX:]
	compressed, err := aead.Open(nil, nonce, ciphertext, []byte(plainSHA256Hex))
	if err != nil {
		return nil, fmt.Errorf("chunk authentication failed: %w", err)
	}
	plain, err := zstdDecode(compressed)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress chunk: %w", err)
	}
	return plain, nil
}

func zstdEncode(src []byte) ([]byte, error) {
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return nil, err
	}
	defer enc.Close()
	return enc.EncodeAll(src, nil), nil
}

func zstdDecode(src []byte) ([]byte, error) {
	dec, err := zstd.NewReader(nil)
	if err != nil {
		return nil, err
	}
	defer dec.Close()
	return dec.DecodeAll(src, nil)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/crypto/ -v && go test ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/crypto/envelope.go internal/crypto/envelope_test.go
git commit -m "feat(crypto): add zstd + XChaCha20 chunk envelope"
```

---

### Task 3: Multi-recipient age encryption and fingerprints

**Files:**
- Create: `internal/crypto/multi.go`
- Create: `internal/crypto/multi_test.go`

**Interfaces:**
- Consumes: existing `func DecryptReader(identityPath string, in io.Reader) (io.Reader, error)`, existing `func GenerateKey() (pubKey, privKey, fileContent string, err error)`.
- Produces:
  - `func EncryptMulti(recipientKeys []string, plaintext []byte) ([]byte, error)`
  - `func FingerprintRecipient(recipient string) string` returns the first 16 hex chars of SHA-256 over the recipient string.

- [ ] **Step 1: Write failing tests**

Create `internal/crypto/multi_test.go`:

```go
package crypto

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func generateTestKey(t *testing.T) (string, string) {
	t.Helper()
	pub, _, fileContent, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error: %v", err)
	}
	path := filepath.Join(t.TempDir(), "identity.txt")
	if err := os.WriteFile(path, []byte(fileContent), 0600); err != nil {
		t.Fatalf("write identity: %v", err)
	}
	return pub, path
}

func TestEncryptMultiOpensWithAnyRecipientIdentity(t *testing.T) {
	pubA, identA := generateTestKey(t)
	pubB, identB := generateTestKey(t)
	_, identC := generateTestKey(t)

	plaintext := []byte(`{"hello":"world"}`)
	sealed, err := EncryptMulti([]string{pubA, pubB}, plaintext)
	if err != nil {
		t.Fatalf("EncryptMulti() error: %v", err)
	}

	for _, identPath := range []string{identA, identB} {
		r, err := DecryptReader(identPath, bytes.NewReader(sealed))
		if err != nil {
			t.Fatalf("DecryptReader(%s) error: %v", identPath, err)
		}
		got, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("ReadAll error: %v", err)
		}
		if !bytes.Equal(got, plaintext) {
			t.Fatalf("decrypted = %q, want %q", got, plaintext)
		}
	}

	if r, err := DecryptReader(identC, bytes.NewReader(sealed)); err == nil {
		if _, err := io.ReadAll(r); err == nil {
			t.Fatal("DecryptReader succeeded with unrelated identity")
		}
	}
}

func TestEncryptMultiRejectsNoRecipients(t *testing.T) {
	if _, err := EncryptMulti(nil, []byte("x")); err == nil {
		t.Fatal("EncryptMulti() succeeded with no recipients")
	}
}

func TestEncryptMultiRejectsInvalidRecipient(t *testing.T) {
	if _, err := EncryptMulti([]string{"age1notavalidkey"}, []byte("x")); err == nil {
		t.Fatal("EncryptMulti() succeeded with invalid recipient")
	}
}

func TestFingerprintRecipientIsStableAndShort(t *testing.T) {
	pub, _ := generateTestKey(t)
	fp := FingerprintRecipient(pub)
	if fp != FingerprintRecipient(pub) {
		t.Fatal("FingerprintRecipient() is not stable")
	}
	if len(fp) != 16 {
		t.Fatalf("FingerprintRecipient() = %q, want 16 hex chars", fp)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/crypto/ -run 'Multi|Fingerprint' -v`
Expected: FAIL, undefined: EncryptMulti

- [ ] **Step 3: Implement**

Create `internal/crypto/multi.go`:

```go
package crypto

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"filippo.io/age"
)

// EncryptMulti encrypts plaintext to every recipient. Any matching identity
// can decrypt the result.
func EncryptMulti(recipientKeys []string, plaintext []byte) ([]byte, error) {
	if len(recipientKeys) == 0 {
		return nil, fmt.Errorf("no recipients given")
	}
	recipients := make([]age.Recipient, 0, len(recipientKeys))
	for _, key := range recipientKeys {
		r, err := age.ParseX25519Recipient(key)
		if err != nil {
			return nil, fmt.Errorf("invalid recipient %q: %w", key, err)
		}
		recipients = append(recipients, r)
	}

	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, recipients...)
	if err != nil {
		return nil, fmt.Errorf("failed to init age encryptor: %w", err)
	}
	if _, err := w.Write(plaintext); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("failed to write plaintext: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("failed to finish age encryption: %w", err)
	}
	return buf.Bytes(), nil
}

// FingerprintRecipient returns a short stable identifier for a recipient key.
func FingerprintRecipient(recipient string) string {
	sum := sha256.Sum256([]byte(recipient))
	return hex.EncodeToString(sum[:])[:16]
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/crypto/ -v && go test ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/crypto/multi.go internal/crypto/multi_test.go
git commit -m "feat(crypto): add multi-recipient age encryption"
```

---

### Task 4: Shared memory test backend + repo package (init/open/paths)

**Files:**
- Create: `internal/storage/list.go`
- Create: `internal/storage/memtest/memory.go`
- Create: `internal/repo/config.go`
- Create: `internal/repo/repo.go`
- Create: `internal/repo/repo_test.go`

**Interfaces:**
- Consumes: `crypto.EncryptMulti`, `crypto.DecryptReader`, `crypto.FingerprintRecipient` (Task 3), `storage.Backend`.
- Produces:
  - `memtest.NewMemoryBackend() *memtest.MemoryBackend` implementing `Upload/Download/ListFiles/Delete` plus helpers `Has(path string) bool`, `Put(path string, data []byte)`, `UploadCount(path string) int`, `TotalUploads(prefix string) int`, `KeysWithPrefix(prefix string) []string`.
  - `storage.ParseList(raw []byte) ([]storage.ListEntry, error)` with `ListEntry{Path string; IsDir bool}`.
  - `repo.Config` = `{Version, CreatedAt string; Recipients []string; ChunkSize int64}` where `Recipients` holds full public keys (non-secret, see Spec Amendment).
  - Constants `repo.FormatVersion = "2.0"`, `repo.ConfigPath = "repo.json"`, `repo.KeysPrefix = "keys"`, `repo.SnapshotsPrefix = "snapshots"`, `repo.DataPrefix = "data"`.
  - `func repo.Init(backend storage.Backend, recipients []string, chunkSize int64) ([]byte, Config, error)` returns the new master key and config.
  - `func repo.Open(backend storage.Backend, identityPath string) (*Repo, error)` where `Repo{Backend storage.Backend; Config Config; MasterKey []byte}`.
  - `func repo.DataPath(plainSHA256Hex string) string` returns `"data/{hh}/{hash}"`.
  - `func repo.SnapshotPath(uuid string) (string, error)` validates `uuid.Parse` first, then returns `"snapshots/{uuid}.age"`.
  - `func repo.KeysPathFor(recipient string) string` returns `"keys/{fingerprint}.age"`.

- [ ] **Step 1: Write shared list parsing**

Create `internal/storage/list.go`:

```go
package storage

import "encoding/json"

// ListEntry is the subset of rclone lsjson output that gloak uses.
type ListEntry struct {
	Path  string `json:"Path"`
	IsDir bool   `json:"IsDir"`
}

func ParseList(raw []byte) ([]ListEntry, error) {
	var entries []ListEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}
```

- [ ] **Step 2: Write the memory test backend**

Create `internal/storage/memtest/memory.go`:

```go
// Package memtest provides an in-memory storage.Backend for tests.
package memtest

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
)

type MemoryBackend struct {
	mu      sync.Mutex
	files   map[string][]byte
	uploads map[string]int
}

func NewMemoryBackend() *MemoryBackend {
	return &MemoryBackend{files: make(map[string][]byte), uploads: make(map[string]int)}
}

func (m *MemoryBackend) Upload(remotePath string, in io.Reader) error {
	data, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[remotePath] = append([]byte(nil), data...)
	m.uploads[remotePath]++
	return nil
}

func (m *MemoryBackend) Download(remotePath string, out io.Writer) error {
	m.mu.Lock()
	data, ok := m.files[remotePath]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("not found: %s", remotePath)
	}
	_, err := out.Write(data)
	return err
}

// ListFiles mimics rclone lsjson: Path is relative to the listed prefix,
// only direct children are returned, directories are marked IsDir.
func (m *MemoryBackend) ListFiles(prefix []string) ([]byte, error) {
	p := strings.Trim(strings.Join(prefix, "/"), "/")
	m.mu.Lock()
	defer m.mu.Unlock()

	children := map[string]bool{}
	for key := range m.files {
		var rel string
		switch {
		case p == "":
			rel = key
		case strings.HasPrefix(key, p+"/"):
			rel = strings.TrimPrefix(key, p+"/")
		default:
			continue
		}
		if idx := strings.Index(rel, "/"); idx >= 0 {
			children[rel[:idx]] = true
		} else {
			children[rel] = false
		}
	}

	names := make([]string, 0, len(children))
	for name := range children {
		names = append(names, name)
	}
	sort.Strings(names)

	entries := make([]map[string]any, 0, len(names))
	for _, name := range names {
		entries = append(entries, map[string]any{
			"Path":  name,
			"Name":  name,
			"IsDir": children[name],
			"Size":  0,
		})
	}
	return json.Marshal(entries)
}

func (m *MemoryBackend) Delete(remotePath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, remotePath)
	return nil
}

func (m *MemoryBackend) Has(path string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.files[path]
	return ok
}

func (m *MemoryBackend) Put(path string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[path] = append([]byte(nil), data...)
}

func (m *MemoryBackend) UploadCount(path string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.uploads[path]
}

func (m *MemoryBackend) TotalUploads(prefix string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	total := 0
	for path, n := range m.uploads {
		if strings.HasPrefix(path, prefix) {
			total += n
		}
	}
	return total
}

func (m *MemoryBackend) KeysWithPrefix(prefix string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var keys []string
	for key := range m.files {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}
```

- [ ] **Step 3: Write failing repo tests**

Create `internal/repo/repo_test.go`:

```go
package repo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gloak/internal/crypto"
	"gloak/internal/storage/memtest"
)

func testRecipient(t *testing.T) (string, string) {
	t.Helper()
	pub, _, fileContent, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error: %v", err)
	}
	path := filepath.Join(t.TempDir(), "identity.txt")
	if err := os.WriteFile(path, []byte(fileContent), 0600); err != nil {
		t.Fatalf("write identity: %v", err)
	}
	return pub, path
}

func TestInitWritesKeysAndConfig(t *testing.T) {
	pubA, _ := testRecipient(t)
	pubB, _ := testRecipient(t)
	backend := memtest.NewMemoryBackend()

	master, cfg, err := Init(backend, []string{pubA, pubB}, 4096)
	if err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	if len(master) != 32 {
		t.Fatalf("master key length = %d, want 32", len(master))
	}
	if cfg.Version != FormatVersion || cfg.ChunkSize != 4096 {
		t.Fatalf("cfg = %+v", cfg)
	}
	if !backend.Has(ConfigPath) {
		t.Fatal("repo.json not uploaded")
	}
	for _, pub := range []string{pubA, pubB} {
		path := KeysPathFor(pub)
		if !backend.Has(path) {
			t.Fatalf("key file %s not uploaded", path)
		}
	}
}

func TestOpenWithEachRecipientIdentity(t *testing.T) {
	pubA, identA := testRecipient(t)
	pubB, identB := testRecipient(t)
	backend := memtest.NewMemoryBackend()
	master, _, err := Init(backend, []string{pubA, pubB}, 4096)
	if err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	for _, identPath := range []string{identA, identB} {
		r, err := Open(backend, identPath)
		if err != nil {
			t.Fatalf("Open(%s) error: %v", identPath, err)
		}
		if string(r.MasterKey) != string(master) {
			t.Fatalf("Open(%s) master key mismatch", identPath)
		}
	}
}

func TestOpenRejectsWrongIdentity(t *testing.T) {
	pubA, _ := testRecipient(t)
	_, identOther := testRecipient(t)
	backend := memtest.NewMemoryBackend()
	if _, _, err := Init(backend, []string{pubA}, 4096); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	_, err := Open(backend, identOther)
	if err == nil {
		t.Fatal("Open() succeeded with unrelated identity")
	}
	if !strings.Contains(err.Error(), "cannot open") {
		t.Fatalf("Open() error = %v, want cannot open", err)
	}
}

func TestOpenRejectsMissingConfig(t *testing.T) {
	_, ident := testRecipient(t)
	_, err := Open(memtest.NewMemoryBackend(), ident)
	if err == nil {
		t.Fatal("Open() succeeded on empty backend")
	}
	if !strings.Contains(err.Error(), ConfigPath) {
		t.Fatalf("Open() error = %v, want repo.json mention", err)
	}
}

func TestDataPathUsesHashSharding(t *testing.T) {
	got := DataPath(strings.Repeat("ab", 32))
	want := "data/ab/" + strings.Repeat("ab", 32)
	if got != want {
		t.Fatalf("DataPath() = %q, want %q", got, want)
	}
}

func TestSnapshotPathRejectsInvalidUUID(t *testing.T) {
	if _, err := SnapshotPath("../escape"); err == nil {
		t.Fatal("SnapshotPath() accepted invalid uuid")
	}
	got, err := SnapshotPath("7f91c6c7-7a0b-44aa-ae23-997b60e4e998")
	if err != nil {
		t.Fatalf("SnapshotPath() error: %v", err)
	}
	if got != "snapshots/7f91c6c7-7a0b-44aa-ae23-997b60e4e998.age" {
		t.Fatalf("SnapshotPath() = %q", got)
	}
}
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `go test ./internal/repo/ -v`
Expected: FAIL, undefined: Init, Open, and the rest.

- [ ] **Step 5: Implement config and repo**

Create `internal/repo/config.go`:

```go
package repo

const (
	FormatVersion   = "2.0"
	ConfigPath      = "repo.json"
	KeysPrefix      = "keys"
	SnapshotsPrefix = "snapshots"
	DataPrefix      = "data"
)

// Config is the plaintext repository descriptor. It carries no user data:
// version, creation time, recipient public keys, and chunk parameters.
type Config struct {
	Version    string   `json:"version"`
	CreatedAt  string   `json:"created_at"`
	Recipients []string `json:"recipients"`
	ChunkSize  int64    `json:"chunk_size"`
}
```

Create `internal/repo/repo.go`:

```go
package repo

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"filippo.io/age"
	"github.com/google/uuid"

	"gloak/internal/crypto"
	"gloak/internal/storage"
)

const masterKeySize = 32

type Repo struct {
	Backend   storage.Backend
	Config    Config
	MasterKey []byte
}

// KeysPathFor returns the remote path of the wrapped master key for a recipient.
func KeysPathFor(recipient string) string {
	return fmt.Sprintf("%s/%s.age", KeysPrefix, crypto.FingerprintRecipient(recipient))
}

// DataPath returns the content-addressed object path for a plaintext chunk hash.
func DataPath(plainSHA256Hex string) string {
	return fmt.Sprintf("%s/%s/%s", DataPrefix, plainSHA256Hex[:2], plainSHA256Hex)
}

// SnapshotPath returns the snapshot manifest path, rejecting anything that is
// not a valid UUID so a crafted id can never escape the snapshots/ prefix.
func SnapshotPath(snapshotID string) (string, error) {
	if _, err := uuid.Parse(snapshotID); err != nil {
		return "", fmt.Errorf("invalid snapshot id: %w", err)
	}
	return fmt.Sprintf("%s/%s.age", SnapshotsPrefix, snapshotID), nil
}

// Init creates the repository on the backend: one wrapped master key per
// recipient first, repo.json last as the completion marker.
func Init(backend storage.Backend, recipients []string, chunkSize int64) ([]byte, Config, error) {
	if len(recipients) == 0 {
		return nil, Config{}, fmt.Errorf("at least one recipient is required")
	}
	for _, r := range recipients {
		if _, err := age.ParseX25519Recipient(r); err != nil {
			return nil, Config{}, fmt.Errorf("invalid recipient %q: %w", r, err)
		}
	}
	if chunkSize <= 0 {
		return nil, Config{}, fmt.Errorf("chunk size must be positive")
	}

	master := make([]byte, masterKeySize)
	if _, err := rand.Read(master); err != nil {
		return nil, Config{}, fmt.Errorf("failed to generate master key: %w", err)
	}

	cfg := Config{
		Version:    FormatVersion,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		Recipients: recipients,
		ChunkSize:  chunkSize,
	}

	for _, recipient := range recipients {
		wrapped, err := crypto.EncryptMulti([]string{recipient}, master)
		if err != nil {
			return nil, Config{}, fmt.Errorf("failed to wrap master key: %w", err)
		}
		if err := backend.Upload(KeysPathFor(recipient), bytes.NewReader(wrapped)); err != nil {
			return nil, Config{}, fmt.Errorf("failed to upload wrapped key: %w", err)
		}
	}

	cfgBytes, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, Config{}, fmt.Errorf("failed to marshal repo config: %w", err)
	}
	if err := backend.Upload(ConfigPath, bytes.NewReader(cfgBytes)); err != nil {
		return nil, Config{}, fmt.Errorf("failed to upload repo.json: %w", err)
	}
	return master, cfg, nil
}

// Open loads repo.json and unwraps the master key with the given identity.
func Open(backend storage.Backend, identityPath string) (*Repo, error) {
	var buf bytes.Buffer
	if err := backend.Download(ConfigPath, &buf); err != nil {
		return nil, fmt.Errorf("not a gloak repository (%s missing): %w", ConfigPath, err)
	}

	var cfg Config
	if err := json.Unmarshal(buf.Bytes(), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", ConfigPath, err)
	}
	if cfg.Version != FormatVersion {
		return nil, fmt.Errorf("unsupported repository format %q (want %q)", cfg.Version, FormatVersion)
	}

	raw, err := backend.ListFiles([]string{KeysPrefix})
	if err != nil {
		return nil, fmt.Errorf("failed to list keys: %w", err)
	}
	entries, err := storage.ParseList(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to parse keys listing: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir {
			continue
		}
		var keyBuf bytes.Buffer
		if err := backend.Download(fmt.Sprintf("%s/%s", KeysPrefix, entry.Path), &keyBuf); err != nil {
			continue
		}
		reader, err := crypto.DecryptReader(identityPath, &keyBuf)
		if err != nil {
			continue
		}
		master, err := io.ReadAll(reader)
		if err != nil || len(master) != masterKeySize {
			continue
		}
		return &Repo{Backend: backend, Config: cfg, MasterKey: master}, nil
	}
	return nil, fmt.Errorf("identity cannot open this repository")
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/repo/ ./internal/storage/... -v && go test ./...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/storage/list.go internal/storage/memtest/ internal/repo/
git commit -m "feat(repo): add repository init, open, and path helpers"
```

---

### Task 5: Encrypted epoch chunk index

**Files:**
- Create: `internal/index/index.go`
- Create: `internal/index/index_test.go`

**Interfaces:**
- Consumes: `crypto.EncryptMulti`, `crypto.DecryptReader`, `storage.Backend` (Task 3, 4).
- Produces:
  - `type Entry = {PlainSHA256 string; PlainSize int64; EncryptedSize int64; EncryptedSHA256 string}`.
  - `type Index = {epoch int; pending map[string]Entry; all map[string]Entry}` with methods `New() *Index`, `Load(backend storage.Backend, identityPath string) (*Index, error)`, `Has(hash string) bool`, `Get(hash string) (Entry, bool)`, `Add(entry Entry)`, `Save(backend storage.Backend, recipients []string) (int, error)` (returns number of new entries written; writes nothing when pending is empty; each successful Save bumps the epoch).
  - Index objects live at `index/%06d.age`, zero-padded epoch numbers, encrypted with `EncryptMulti` to the repo recipients.
  - `Load` merges every `index/*.age` file in ascending name order; empty/corrupt-but-unreadable-with-identity files are skipped with empty maps (a file that fails age decryption with the identity is skipped, missing index dir is valid).
  - `Add` records into both `pending` and `all`; `Has`/`Get` check pending first then all.

- [ ] **Step 1: Write failing tests**

Create `internal/index/index_test.go`:

```go
package index

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"gloak/internal/crypto"
	"gloak/internal/storage/memtest"
)

func identityFixture(t *testing.T) (string, string) {
	t.Helper()
	pub, _, fileContent, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error: %v", err)
	}
	path := filepath.Join(t.TempDir(), "identity.txt")
	if err := os.WriteFile(path, []byte(fileContent), 0600); err != nil {
		t.Fatalf("write identity: %v", err)
	}
	return pub, path
}

func entryFixture(hash string) Entry {
	return Entry{
		PlainSHA256:     hash,
		PlainSize:       100,
		EncryptedSize:   120,
		EncryptedSHA256: hash + "-enc",
	}
}

func TestAddHasGet(t *testing.T) {
	ix := New()
	ix.Add(entryFixture("aaa"))
	if !ix.Has("aaa") {
		t.Fatal("Has() = false after Add")
	}
	if e, ok := ix.Get("aaa"); !ok || e.PlainSize != 100 {
		t.Fatalf("Get() = %+v, %v", e, ok)
	}
	if ix.Has("bbb") {
		t.Fatal("Has() = true for unknown hash")
	}
}

func TestSaveAndLoadAcrossEpochs(t *testing.T) {
	pub, ident := identityFixture(t)
	backend := memtest.NewMemoryBackend()

	ix := New()
	ix.Add(entryFixture("aaa"))
	ix.Add(entryFixture("bbb"))
	n, err := ix.Save(backend, []string{pub})
	if err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	if n != 2 {
		t.Fatalf("Save() = %d, want 2", n)
	}

	ix.Add(entryFixture("ccc"))
	if n, err := ix.Save(backend, []string{pub}); err != nil || n != 1 {
		t.Fatalf("second Save() = %d, %v, want 1, nil", n, err)
	}

	loaded, err := Load(backend, ident)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	for _, hash := range []string{"aaa", "bbb", "ccc"} {
		if !loaded.Has(hash) {
			t.Fatalf("Load() missing %s", hash)
		}
	}
}

func TestSaveSkipsEmptyEpoch(t *testing.T) {
	pub, _ := identityFixture(t)
	backend := memtest.NewMemoryBackend()
	ix := New()
	ix.Add(entryFixture("aaa"))
	if _, err := ix.Save(backend, []string{pub}); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	before := len(backend.KeysWithPrefix("index/"))
	n, err := ix.Save(backend, []string{pub})
	if err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	if n != 0 || len(backend.KeysWithPrefix("index/")) != before {
		t.Fatalf("empty Save() wrote files: n=%d files=%d", n, len(backend.KeysWithPrefix("index/")))
	}
}

func TestLoadSkipsUnreadableEpoch(t *testing.T) {
	pub, ident := identityFixture(t)
	backend := memtest.NewMemoryBackend()
	ix := New()
	ix.Add(entryFixture("aaa"))
	if _, err := ix.Save(backend, []string{pub}); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	// Corrupt one epoch file with garbage that is not valid age ciphertext.
	backend.Put("index/000002.age", []byte("garbage-not-age"))
	// Add a second valid epoch so we can confirm the merge continues.
	ix.Add(entryFixture("bbb"))
	if _, err := ix.Save(backend, []string{pub}); err != nil {
		t.Fatalf("second Save() error: %v", err)
	}

	loaded, err := Load(backend, ident)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if !loaded.Has("aaa") || !loaded.Has("bbb") {
		t.Fatalf("Load() lost entries: %+v", loaded.all)
	}
}

func TestLoadOnEmptyBackendIsClean(t *testing.T) {
	_, ident := identityFixture(t)
	loaded, err := Load(memtest.NewMemoryBackend(), ident)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(loaded.all) != 0 {
		t.Fatalf("Load() on empty backend = %+v", loaded.all)
	}
}

func TestSaveOutputIsEncryptedJSON(t *testing.T) {
	pub, ident := identityFixture(t)
	backend := memtest.NewMemoryBackend()
	ix := New()
	ix.Add(entryFixture("aaa"))
	if _, err := ix.Save(backend, []string{pub}); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	var buf bytes.Buffer
	if err := backend.Download("index/000001.age", &buf); err != nil {
		t.Fatalf("Download() error: %v", err)
	}
	reader, err := crypto.DecryptReader(ident, &buf)
	if err != nil {
		t.Fatalf("index file is not age-encrypted with identity: %v", err)
	}
	plain, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if !bytes.Contains(plain, []byte("aaa")) {
		t.Fatalf("decrypted index missing entry hash: %s", plain)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/index/ -v`
Expected: FAIL, package not found / undefined types.

- [ ] **Step 3: Implement**

Create `internal/index/index.go`:

```go
// Package index tracks which chunks already exist in the repository so
// repeat backups upload nothing.
package index

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"gloak/internal/crypto"
	"gloak/internal/storage"
)

const indexPrefix = "index"

type Entry struct {
	PlainSHA256     string `json:"plain_sha256"`
	PlainSize       int64  `json:"plain_size"`
	EncryptedSize   int64  `json:"encrypted_size"`
	EncryptedSHA256 string `json:"encrypted_sha256"`
}

type Index struct {
	epoch   int
	pending map[string]Entry
	all     map[string]Entry
}

type epochFile struct {
	Entries []Entry `json:"entries"`
}

func New() *Index {
	return &Index{
		pending: make(map[string]Entry),
		all:     make(map[string]Entry),
	}
}

// Load merges every index epoch in ascending name order. Epochs that cannot
// be decrypted with the identity are skipped: the caller still has the data
// objects, and the next Save rewrites what is missing.
func Load(backend storage.Backend, identityPath string) (*Index, error) {
	ix := New()

	raw, err := backend.ListFiles([]string{indexPrefix})
	if err != nil {
		return nil, fmt.Errorf("failed to list %s: %w", indexPrefix, err)
	}
	entries, err := storage.ParseList(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to parse index listing: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir {
			names = append(names, entry.Path)
		}
	}
	sort.Strings(names)

	for _, name := range names {
		var epochBuf bytes.Buffer
		if err := backend.Download(fmt.Sprintf("%s/%s", indexPrefix, name), &epochBuf); err != nil {
			continue
		}
		reader, err := crypto.DecryptReader(identityPath, &epochBuf)
		if err != nil {
			continue
		}
		plain, err := io.ReadAll(reader)
		if err != nil {
			continue
		}
		var file epochFile
		if err := json.Unmarshal(plain, &file); err != nil {
			continue
		}
		for _, e := range file.Entries {
			ix.all[e.PlainSHA256] = e
		}
	}
	return ix, nil
}

func (ix *Index) Has(hash string) bool {
	_, ok := ix.Get(hash)
	return ok
}

func (ix *Index) Get(hash string) (Entry, bool) {
	if e, ok := ix.pending[hash]; ok {
		return e, true
	}
	e, ok := ix.all[hash]
	return e, ok
}

func (ix *Index) Add(entry Entry) {
	ix.pending[entry.PlainSHA256] = entry
	ix.all[entry.PlainSHA256] = entry
}

// Save writes only the pending entries as the next epoch. It is a no-op when
// nothing is pending, so a second backup of unchanged data writes no files.
func (ix *Index) Save(backend storage.Backend, recipients []string) (int, error) {
	if len(ix.pending) == 0 {
		return 0, nil
	}

	hashes := make([]string, 0, len(ix.pending))
	for hash := range ix.pending {
		hashes = append(hashes, hash)
	}
	sort.Strings(hashes)

	file := epochFile{Entries: make([]Entry, 0, len(hashes))}
	for _, hash := range hashes {
		file.Entries = append(file.Entries, ix.pending[hash])
	}

	plain, err := json.Marshal(file)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal index epoch: %w", err)
	}
	sealed, err := crypto.EncryptMulti(recipients, plain)
	if err != nil {
		return 0, fmt.Errorf("failed to encrypt index epoch: %w", err)
	}

	name := fmt.Sprintf("%s/%06d.age", indexPrefix, ix.epoch+1)
	if err := backend.Upload(name, bytes.NewReader(sealed)); err != nil {
		return 0, fmt.Errorf("failed to upload index epoch: %w", err)
	}

	for _, hash := range hashes {
		delete(ix.pending, hash)
	}
	ix.epoch++
	return len(hashes), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/index/ -v && go test ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/index/
git commit -m "feat(index): add encrypted epoch chunk index"
```

---

### Task 6: Init flow + CLI wiring for `gloak init`

**Files:**
- Create: `internal/flow/init.go`
- Create: `root` command file: `init.go` at repo root (alongside existing `keygen.go`)
- Modify: `main.go` (command table only)
- Modify: `main_test.go` (option-short-tag test expects InitCmd)
- Create: `internal/flow/init_test.go`

**Interfaces:**
- Consumes: `repo.Init` (Task 4), existing pterm helpers style from `root keygen.go`.
- Produces:
  - `func flow.RunInit(recipients []string, remote string) error` prints progress and the new repo location; calls unexported `runInit` for tests.
  - `func runInit(recipients []string, backend storage.Backend, chunkSize int64) ([]byte, repo.Config, error)` pure backend-based version.
  - CLI: `InitCmd` with `-R/--remote` (required) and `-r/--recipient` (optional; when empty, gloak prints a hint to run `gloak keygen` first and fails with a clear error, same UX spirit as today's upload path).

- [ ] **Step 1: Write failing flow tests**

Create `internal/flow/init_test.go`:

```go
package flow

import (
	"strings"
	"testing"

	"gloak/internal/crypto"
	"gloak/internal/repo"
	"gloak/internal/storage/memtest"
)

func TestRunInitCreatesRepository(t *testing.T) {
	pub, _, fileContent, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error: %v", err)
	}
	_ = fileContent
	backend := memtest.NewMemoryBackend()

	master, cfg, err := runInit([]string{pub}, backend, 4096)
	if err != nil {
		t.Fatalf("runInit() error: %v", err)
	}
	if len(master) != 32 {
		t.Fatalf("master key length = %d", len(master))
	}
	if !backend.Has(repo.ConfigPath) {
		t.Fatal("repo.json missing")
	}
	if !backend.Has(repo.KeysPathFor(pub)) {
		t.Fatal("wrapped key missing")
	}
	if cfg.ChunkSize != 4096 {
		t.Fatalf("cfg.ChunkSize = %d", cfg.ChunkSize)
	}
}

func TestRunInitRejectsNoRecipients(t *testing.T) {
	if _, _, err := runInit(nil, memtest.NewMemoryBackend(), 4096); err == nil {
		t.Fatal("runInit() succeeded with no recipients")
	} else if !strings.Contains(err.Error(), "recipient") {
		t.Fatalf("runInit() error = %v, want recipient mention", err)
	}
}

func TestRunInitRejectsInvalidRecipient(t *testing.T) {
	if _, _, err := runInit([]string{"age1bad"}, memtest.NewMemoryBackend(), 4096); err == nil {
		t.Fatal("runInit() succeeded with invalid recipient")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/flow/ -run TestRunInit -v`
Expected: FAIL, undefined: runInit

- [ ] **Step 3: Implement the flow**

Create `internal/flow/init.go`:

```go
package flow

import (
	"fmt"

	"github.com/pterm/pterm"

	"gloak/internal/repo"
	"gloak/internal/storage"
)

// RunInit creates a new gloak repository on remote and prints the outcome.
func RunInit(recipients []string, remote string) error {
	backend := storage.NewRcloneBackend(remote)
	spinner, _ := pterm.DefaultSpinner.Start("Initializing repository...")

	master, cfg, err := runInit(recipients, backend, defaultChunkSize)
	if err != nil {
		_ = spinner.Stop()
		return err
	}
	_ = spinner.Stop()
	pterm.Success.Println("Repository initialized")
	pterm.Printf("Location: %s\n", remote)
	pterm.Printf("Recipients: %v\n", cfg.Recipients)
	pterm.Printf("Chunk size: %d bytes\n", cfg.ChunkSize)
	pterm.Printf("Master key length: %d bytes (wrapped in %s)\n", len(master), repo.KeysPrefix+"/")
	return nil
}

func runInit(recipients []string, backend storage.Backend, chunkSize int64) ([]byte, repo.Config, error) {
	return repo.Init(backend, recipients, chunkSize)
}

// resolveRecipients reads public keys from the given file (one per line,
// lines starting with # ignored) or, when path is empty, from the default
// ~/.gloak/recipient.txt. Comma-separated inline keys are also accepted.
func resolveRecipients(path string) ([]string, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("cannot determine home directory: %w", err)
		}
		path = filepath.Join(home, ".gloak", "recipient.txt")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read recipients from %s: %w (run 'gloak keygen' first)", path, err)
	}
	var recipients []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		recipients = append(recipients, line)
	}
	if len(recipients) == 0 {
		return nil, fmt.Errorf("no recipients found in %s", path)
	}
	return recipients, nil
}
```

Note: `resolveRecipients` needs `os`, `path/filepath`, `strings` imports in the same file; add them when writing.

- [ ] **Step 4: Add the root CLI command**

Create root `init.go` (mirrors existing root command style in `keygen.go`):

```go
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/alecthomas/kong"

	"gloak/internal/flow"
)

// InitCmd creates a new gloak repository.
type InitCmd struct {
	Remote    string `short:"R" required:"" help:"Repository remote, e.g. b2:my-bucket/gloak or local path"`
	Recipient string `short:"r" help:"Recipient public key file or comma-separated keys (default ~/.gloak/recipient.txt)"`
}

func (c *InitCmd) Run(ctx *kong.Context) error {
	recipients, err := resolveInitRecipients(c.Recipient)
	if err != nil {
		return err
	}
	return flow.RunInit(recipients, c.Remote)
}

func resolveInitRecipients(path string) ([]string, error) {
	if path != "" && !strings.Contains(path, "\n") && !strings.HasPrefix(path, "age1") {
		if data, err := os.ReadFile(path); err == nil {
			var out []string
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				out = append(out, line)
			}
			if len(out) == 0 {
				return nil, fmt.Errorf("no recipients found in %s", path)
			}
			return out, nil
		}
	}
	if path == "" {
		return flowResolveDefaultRecipients()
	}
	var out []string
	for _, part := range strings.Split(path, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no recipients given")
	}
	return out, nil
}

func flowResolveDefaultRecipients() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory: %w", err)
	}
	return resolveInitRecipients(home + "/.gloak/recipient.txt")
}
```

Simplification note for the implementer: the above is intentionally straightforward; prefer reusing a single `resolveInitRecipients` helper in the root package and delete the unused `flow.resolveRecipients` if nothing calls it, or keep only one of the two (pick whichever keeps `main_test.go` passing with less duplication).

- [ ] **Step 5: Register in main.go**

In `main.go`, add to the kong command group (order near Keygen):

```go
	Init InitCmd `cmd:"" help:"Initialize a new gloak repository" group:"Repository"`
```

- [ ] **Step 6: Update main_test.go**

In `TestCommandOptionShortTags`, add a case for InitCmd asserting `R` and `r` tags exist (mirror how UploadCmd/CleanupCmd are asserted today; UploadCmd case will be removed in Task 9, keep it for now if the test still references it).

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 8: Manual smoke (optional, no real backend needed)**

Run: `go build -o /tmp/gloak-smoke . && /tmp/gloak-smoke init --help`
Expected: help text shows `-R` and `-r`.

- [ ] **Step 9: Commit**

```bash
git add internal/flow/init.go internal/flow/init_test.go init.go main.go main_test.go
git commit -m "feat(flow): add repository init flow"
```

---

### Task 7: Backup flow with index skip + checkpointing

**Files:**
- Create: `internal/flow/hash.go` (move `hashBytes` here from `internal/flow/upload.go`)
- Modify: `internal/flow/upload.go` (delete local `hashBytes`)
- Create: `internal/flow/backup.go`
- Create: `internal/flow/backup_test.go`
- Modify: `internal/flow/flow_test.go` (only if helpers clash; prefer memtest backend from Task 4 for new tests)

**Interfaces:**
- Consumes: `repo.Open`, `repo.DataPath`, `repo.SnapshotPath`, `crypto.SealChunk`, `index.Load/Index.Save`, `core.ChunkMeta`, `core.Manifest`, `core.GenerateUUID`, `core.CreateChunker` (fixed-size chunker from existing `core/chunk.go`).
- Produces:
  - `func RunBackup(filePath, identityPath, remote string) (snapshotID string, err error)` (CLI-facing; spinner/pterm like existing flows).
  - `func runBackup(filePath, identityPath string, backend storage.Backend, snapshotID string, progress func(int)) (BackupResult, error)` where `BackupResult = {SnapshotID string; TotalChunks int; UploadedChunks int; SkippedChunks int; TotalPlainBytes int64}`.
  - Checkpoint constant `indexCheckpointEvery = 64`: after every 64 newly uploaded chunks, call `ix.Save` so an interrupted backup keeps most of its progress.
  - Ordering inside `runBackup`: load repo and index, stream chunks, for each chunk hash+skip-or-seal+upload, `ix.Add` only after a successful upload, periodic `ix.Save`, final `ix.Save`, build+encrypt+upload snapshot last.
  - Snapshot manifest: reuse existing `core.Manifest` JSON but set `Version` to `"2.0"` (repo.FormatVersion) and keep existing fields (UUID, BackupPath, OriginalPath, BackupTime, Chunks, TotalChunks, TotalSize). Chunk entries reuse `core.ChunkMeta` as-is (Index, Name, PlainSize, PlainSHA256, EncryptedSize, EncryptedSHA256) where `Name` is the `DataPath` string and the size fields match the sealed object length and plain chunk length.
  - Snapshot plaintext is encrypted with `crypto.EncryptMulti(repo.Config.Recipients, manifestJSON)` and uploaded to `repo.SnapshotPath(uuid)`.

- [ ] **Step 1: Move hashBytes to hash.go**

Create `internal/flow/hash.go` with the existing `hashBytes` function body (copy from `upload.go`, same package, no signature change). Remove the copy from `upload.go`.

Run: `go test ./internal/flow/ -count=1` to confirm nothing broke (same package, pure move).

- [ ] **Step 2: Write failing backup tests**

Create `internal/flow/backup_test.go`:

```go
package flow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gloak/internal/crypto"
	"gloak/internal/repo"
	"gloak/internal/storage/memtest"
)

func repoFixture(t *testing.T, chunkSize int64) (string, *memtest.MemoryBackend, string) {
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
	if _, _, err := repo.Init(backend, []string{pub}, chunkSize); err != nil {
		t.Fatalf("repo.Init() error: %v", err)
	}
	return identPath, backend, pub
}

func writeTempFile(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "payload.bin")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func TestBackupWritesSnapshotAndDataObjects(t *testing.T) {
	ident, backend, _ := repoFixture(t, 8)
	payload := []byte("0123456789abcdefghij") // 3 chunks with size 8
	filePath := writeTempFile(t, payload)

	res, err := runBackup(filePath, ident, backend, "7f91c6c7-7a0b-44aa-ae23-997b60e4e998", nil)
	if err != nil {
		t.Fatalf("runBackup() error: %v", err)
	}
	if res.TotalChunks != 3 || res.UploadedChunks != 3 || res.SkippedChunks != 0 {
		t.Fatalf("result = %+v", res)
	}
	if !backend.Has(repo.SnapshotsPrefix + "/" + res.SnapshotID + ".age") {
		t.Fatal("snapshot file missing")
	}
	if got := backend.TotalUploads(repo.DataPrefix + "/"); got != 3 {
		t.Fatalf("data uploads = %d, want 3", got)
	}
	if !backend.Has("index/000001.age") {
		t.Fatal("index epoch missing")
	}
}

func TestBackupSkipsChunksAlreadyInIndex(t *testing.T) {
	ident, backend, _ := repoFixture(t, 8)
	payload := []byte("0123456789abcdefghij")
	filePath := writeTempFile(t, payload)

	id1 := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	if _, err := runBackup(filePath, ident, backend, id1, nil); err != nil {
		t.Fatalf("first runBackup() error: %v", err)
	}
	dataUploadsBefore := backend.TotalUploads(repo.DataPrefix + "/")

	id2 := "aaaaaaaa-bbbb-4ccc-8ddd-ffffffffffff"
	res, err := runBackup(filePath, ident, backend, id2, nil)
	if err != nil {
		t.Fatalf("second runBackup() error: %v", err)
	}
	if res.UploadedChunks != 0 || res.SkippedChunks != 3 {
		t.Fatalf("second result = %+v, want all skipped", res)
	}
	if backend.TotalUploads(repo.DataPrefix+"/") != dataUploadsBefore {
		t.Fatal("second backup re-uploaded data objects")
	}
	if !backend.Has(repo.SnapshotsPrefix + "/" + id2 + ".age") {
		t.Fatal("second snapshot missing")
	}
}

func TestBackupDedupsRepeatedChunksInSameFile(t *testing.T) {
	ident, backend, _ := repoFixture(t, 8)
	payload := []byte("012345670123456701234567") // same 8-byte chunk three times
	filePath := writeTempFile(t, payload)

	res, err := runBackup(filePath, ident, backend, "11111111-2222-4333-8444-555555555555", nil)
	if err != nil {
		t.Fatalf("runBackup() error: %v", err)
	}
	if res.TotalChunks != 3 {
		t.Fatalf("TotalChunks = %d", res.TotalChunks)
	}
	if res.UploadedChunks != 1 {
		t.Fatalf("UploadedChunks = %d, want 1 (content-addressed dedup)", res.UploadedChunks)
	}
	if got := backend.TotalUploads(repo.DataPrefix + "/"); got != 1 {
		t.Fatalf("data uploads = %d, want 1", got)
	}
}

func TestBackupFailsWithoutRepository(t *testing.T) {
	ident, _, _ := repoFixture(t, 8)
	filePath := writeTempFile(t, []byte("x"))
	_, err := runBackup(filePath, ident, memtest.NewMemoryBackend(), "7f91c6c7-7a0b-44aa-ae23-997b60e4e998", nil)
	if err == nil {
		t.Fatal("runBackup() succeeded without repository")
	}
	if !strings.Contains(err.Error(), "not a gloak repository") {
		t.Fatalf("error = %v, want not a gloak repository", err)
	}
}

func TestBackupRejectsInvalidSnapshotID(t *testing.T) {
	ident, backend, _ := repoFixture(t, 8)
	filePath := writeTempFile(t, []byte("x"))
	_, err := runBackup(filePath, ident, backend, "../escape", nil)
	if err == nil {
		t.Fatal("runBackup() succeeded with invalid snapshot id")
	}
	if !strings.Contains(err.Error(), "invalid snapshot id") {
		t.Fatalf("error = %v", err)
	}
}

func TestBackupObjectMatchesChunkHash(t *testing.T) {
	ident, backend, _ := repoFixture(t, 8)
	payload := []byte("0123456789abcdefghij")
	filePath := writeTempFile(t, payload)
	if _, err := runBackup(filePath, ident, backend, "99999999-8888-4777-8666-555555555555", nil); err != nil {
		t.Fatalf("runBackup() error: %v", err)
	}
	sum := sha256.Sum256(payload[:8])
	wantPath := repo.DataPath(hex.EncodeToString(sum[:]))
	if !backend.Has(wantPath) {
		t.Fatalf("expected object at %s", wantPath)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/flow/ -run TestBackup -v`
Expected: FAIL, undefined: runBackup

- [ ] **Step 4: Implement backup.go**

Create `internal/flow/backup.go`:

```go
package flow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/pterm/pterm"

	"gloak/internal/core"
	"gloak/internal/crypto"
	"gloak/internal/index"
	"gloak/internal/repo"
	"gloak/internal/storage"
)

const indexCheckpointEvery = 64

type BackupResult struct {
	SnapshotID      string
	TotalChunks     int
	UploadedChunks  int
	SkippedChunks   int
	TotalPlainBytes int64
}

// RunBackup backs up filePath into the repository on remote using identityPath.
func RunBackup(filePath, identityPath, remote string) (string, error) {
	backend := storage.NewRcloneBackend(remote)
	snapshotID := core.GenerateUUID()

	spinner, _ := pterm.DefaultSpinner.Start("Backing up...")
	res, err := runBackup(filePath, identityPath, backend, snapshotID, func(done int) {
		_ = spinner.UpdateText(fmt.Sprintf("Backed up %d chunks", done))
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

func runBackup(filePath, identityPath string, backend storage.Backend, snapshotID string, progress func(int)) (BackupResult, error) {
	if _, err := uuid.Parse(snapshotID); err != nil {
		return BackupResult{}, fmt.Errorf("invalid snapshot id: %w", err)
	}

	r, err := repo.Open(backend, identityPath)
	if err != nil {
		return BackupResult{}, err
	}

	ix, err := index.Load(backend, identityPath)
	if err != nil {
		return BackupResult{}, fmt.Errorf("failed to load index: %w", err)
	}

	file, err := os.Open(filePath)
	if err != nil {
		return BackupResult{}, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	chunker := core.CreateChunker(file, r.Config.ChunkSize)

	res := BackupResult{SnapshotID: snapshotID}
	var metas []core.ChunkMeta
	var sinceCheckpoint int

	for {
		chunk, err := chunker.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return BackupResult{}, fmt.Errorf("failed to read chunk: %w", err)
		}
		if len(chunk) == 0 {
			continue
		}

		sum := sha256.Sum256(chunk)
		hash := hex.EncodeToString(sum[:])

		res.TotalChunks++
		res.TotalPlainBytes += int64(len(chunk))

		dataPath := repo.DataPath(hash)

		if ix.Has(hash) {
			res.SkippedChunks++
		} else {
			sealed, err := crypto.SealChunk(r.MasterKey, hash, chunk)
			if err != nil {
				return BackupResult{}, fmt.Errorf("failed to seal chunk %s: %w", hash, err)
			}
			encSum := sha256.Sum256(sealed)
			if err := backend.Upload(dataPath, bytes.NewReader(sealed)); err != nil {
				return BackupResult{}, fmt.Errorf("failed to upload chunk %s: %w", hash, err)
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
					return BackupResult{}, fmt.Errorf("failed to checkpoint index: %w", err)
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
	}

	if _, err := ix.Save(backend, r.Config.Recipients); err != nil {
		return BackupResult{}, fmt.Errorf("failed to save index: %w", err)
	}

	manifest := core.Manifest{
		Version:      repo.FormatVersion,
		UUID:         snapshotID,
		BackupPath:   filePath,
		OriginalPath: filePath,
		BackupTime:   time.Now().UTC().Format(time.RFC3339),
		Chunks:       metas,
		TotalChunks:  len(metas),
		TotalSize:    res.TotalPlainBytes,
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return BackupResult{}, fmt.Errorf("failed to marshal manifest: %w", err)
	}
	sealed, err := crypto.EncryptMulti(r.Config.Recipients, manifestJSON)
	if err != nil {
		return BackupResult{}, fmt.Errorf("failed to encrypt manifest: %w", err)
	}
	snapshotPath, err := repo.SnapshotPath(snapshotID)
	if err != nil {
		return BackupResult{}, err
	}
	if err := backend.Upload(snapshotPath, bytes.NewReader(sealed)); err != nil {
		return BackupResult{}, fmt.Errorf("failed to upload snapshot: %w", err)
	}
	return res, nil
}
```

Note: confirm the exact `core.Chunker` API (`CreateChunker` / `Next` / error for EOF) against `internal/core/chunk.go` before writing; adapt the loop to whatever the existing chunker already exposes (the current `core.CreateChunker` is what `flow.Upload` already uses; keep that call style).

Also confirm `core.Manifest` field names against `internal/core/manifest.go`; if `OriginalPath` does not exist, drop it and keep only the fields that do. Do not invent fields.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/flow/ -run TestBackup -v && go test ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/flow/hash.go internal/flow/upload.go internal/flow/backup.go internal/flow/backup_test.go
git commit -m "feat(flow): add repository backup with index skip"
```

---

### Task 8: Rewrite verify and restore for the repository layout

**Files:**
- Rewrite: `internal/flow/verify.go`
- Rewrite: `internal/flow/restore.go`
- Modify: `internal/flow/flow_test.go` (replace the two v1 layout tests; keep `memoryBackend` local helper only if still needed, prefer `memtest`)
- Create (helpers used by both): `internal/flow/manifest_read.go`

**Interfaces:**
- Consumes: `repo.Open`, `repo.SnapshotPath`, `crypto.OpenChunk`, `core.Manifest`, `storage.Backend`.
- Produces (same exported signatures as today so root CLI files do not change in this task):
  - `func RunVerify(snapshotID, identityPath, remote string) error` (unchanged signature).
  - `func RunRestore(snapshotID, outputDir, identityPath, remote string) (string, error)` (unchanged signature; returns the restored file path).
  - Unexported `runVerify(snapshotID, identityPath string, backend storage.Backend) error`.
  - Unexported `runRestore(snapshotID, outputDir, identityPath string, backend storage.Backend) (string, error)`.
  - Helper `func readManifest(r *repo.Repo, snapshotID string) (core.Manifest, error)`: downloads `repo.SnapshotPath(snapshotID)`, decrypts with the identity via the repo master key path (manifest is encrypted to recipients; open with `crypto.DecryptReader(identityPath, ...)`), parses JSON, and returns it.
  - Helper `func validateChunkPath(path string) error`: rejects anything that does not start with `repo.DataPrefix+"/"` or contains `..`; error message `chunk path escapes data prefix: %s`.
  - Corruption handling: when `crypto.OpenChunk` fails or the recomputed plain SHA-256 mismatches `meta.PlainSHA256`, return an error whose message contains the exact string `CHUNK CORRUPT` followed by the chunk index (keeps README/tests compatible). Missing objects also count as `CHUNK CORRUPT`.
  - Restore writes to `outputDir`, recreating nested paths with `os.MkdirAll`, filename safety: `filepath.Base(name) == name` check stays (reuse the existing check from current restore.go).

- [ ] **Step 1: Read the current implementations**

Run: `sed -n '1,200p' internal/flow/verify.go; sed -n '1,200p' internal/flow/restore.go; grep -n "CHUNK CORRUPT\|TestBackupVerify\|TestVerifyRejects\|memoryBackend\|keysWithPrefix" internal/flow/flow_test.go`
Purpose: capture the exact current error strings and the two test functions that must be rewritten, so the rewrite keeps the `CHUNK CORRUPT` contract and drops only what is layout-specific.

- [ ] **Step 2: Write failing tests**

In `internal/flow/flow_test.go`, replace `TestBackupVerifyRestoreRoundTrip` and `TestVerifyRejectsCorruptChunk` with versions that use the repository layout (build fixtures with `repoFixture` + `runBackup` from Task 7). Suggested replacements:

```go
func TestBackupVerifyRestoreRoundTrip(t *testing.T) {
	ident, backend, _ := repoFixture(t, 8)
	payload := []byte("0123456789abcdefghij")
	filePath := writeTempFile(t, payload)
	snapshotID := "7f91c6c7-7a0b-44aa-ae23-997b60e4e998"

	if _, err := runBackup(filePath, ident, backend, snapshotID, nil); err != nil {
		t.Fatalf("runBackup() error: %v", err)
	}
	if err := runVerify(snapshotID, ident, backend); err != nil {
		t.Fatalf("runVerify() error: %v", err)
	}

	outDir := t.TempDir()
	restoredPath, err := runRestore(snapshotID, outDir, ident, backend)
	if err != nil {
		t.Fatalf("runRestore() error: %v", err)
	}
	got, err := os.ReadFile(restoredPath)
	if err != nil {
		t.Fatalf("read restored: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("restored = %q, want %q", got, payload)
	}
}

func TestVerifyRejectsCorruptChunk(t *testing.T) {
	ident, backend, _ := repoFixture(t, 8)
	payload := []byte("0123456789abcdefghij")
	filePath := writeTempFile(t, payload)
	snapshotID := "7f91c6c7-7a0b-44aa-ae23-997b60e4e998"
	if _, err := runBackup(filePath, ident, backend, snapshotID, nil); err != nil {
		t.Fatalf("runBackup() error: %v", err)
	}

	// Corrupt the first data object in place.
	var dataKey string
	for _, key := range backend.KeysWithPrefix(repo.DataPrefix + "/") {
		dataKey = key
		break
	}
	if dataKey == "" {
		t.Fatal("no data objects to corrupt")
	}
	blob := backend.Get(dataKey)
	blob[len(blob)-1] ^= 0xff
	backend.Put(dataKey, blob)

	err := runVerify(snapshotID, ident, backend)
	if err == nil {
		t.Fatal("runVerify() succeeded on corrupt chunk")
	}
	if !strings.Contains(err.Error(), "CHUNK CORRUPT") {
		t.Fatalf("error = %v, want CHUNK CORRUPT", err)
	}
}
```

Note: if `memtest.MemoryBackend` does not yet have a `Get(path) []byte` helper, add one next to `Has`/`Put` in Task 4's file (small addition, same style).

Also add a path-traversal test:

```go
func TestRestoreRejectsEscapingChunkPath(t *testing.T) {
	ident, backend, _ := repoFixture(t, 8)
	// Hand-craft a snapshot whose manifest points outside data/.
	// (Build via EncryptMulti of a Manifest with Chunks[0].Name = "../evil".)
	// Assert runRestore returns an error mentioning "escapes data prefix".
}
```

Implement this test by constructing `core.Manifest` directly, encrypting with `crypto.EncryptMulti` using the recipient from the fixture, uploading to `repo.SnapshotPath(id)`, then calling `runRestore`. Wire `repoFixture` to also return the public key if that makes the test cleaner (change its signature only inside the flow package tests).

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/flow/ -run 'TestBackupVerify|TestVerifyRejects|TestRestoreRejects' -v`
Expected: FAIL (old verify/restore still read the v1 layout).

- [ ] **Step 4: Implement manifest_read.go**

Create `internal/flow/manifest_read.go`:

```go
package flow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"gloak/internal/core"
	"gloak/internal/crypto"
	"gloak/internal/repo"
)

func readManifest(r *repo.Repo, snapshotID string) (core.Manifest, error) {
	path, err := repo.SnapshotPath(snapshotID)
	if err != nil {
		return core.Manifest{}, err
	}
	var buf bytes.Buffer
	if err := r.Backend.Download(path, &buf); err != nil {
		return core.Manifest{}, fmt.Errorf("snapshot %s not found: %w", snapshotID, err)
	}
	// The snapshot is encrypted to the repo recipients; the identity that
	// opened the repo can decrypt it.
	reader, err := crypto.DecryptReader(repo.IdentityHint, &buf)
	if err != nil {
		return core.Manifest{}, fmt.Errorf("failed to decrypt snapshot: %w", err)
	}
	...
}
```

Correction for the implementer: `readManifest` needs the identity path, not a hint constant. Change the signature to `func readManifest(r *repo.Repo, identityPath, snapshotID string) (core.Manifest, error)` and use `crypto.DecryptReader(identityPath, &buf)` then `io.ReadAll` + `json.Unmarshal`. The `r *repo.Repo` argument is only needed for `r.Backend`; alternatively take `backend storage.Backend` and drop `r` — pick whichever keeps call sites in verify/restore simplest.

Full intended body:

```go
func readManifest(backend storage.Backend, identityPath, snapshotID string) (core.Manifest, error) {
	path, err := repo.SnapshotPath(snapshotID)
	if err != nil {
		return core.Manifest{}, err
	}
	var buf bytes.Buffer
	if err := backend.Download(path, &buf); err != nil {
		return core.Manifest{}, fmt.Errorf("snapshot %s not found: %w", snapshotID, err)
	}
	reader, err := crypto.DecryptReader(identityPath, &buf)
	if err != nil {
		return core.Manifest{}, fmt.Errorf("failed to decrypt snapshot: %w", err)
	}
	plain, err := io.ReadAll(reader)
	if err != nil {
		return core.Manifest{}, fmt.Errorf("failed to read snapshot: %w", err)
	}
	var manifest core.Manifest
	if err := json.Unmarshal(plain, &manifest); err != nil {
		return core.Manifest{}, fmt.Errorf("failed to parse snapshot: %w", err)
	}
	return manifest, nil
}

func validateChunkPath(path string) error {
	if !strings.HasPrefix(path, repo.DataPrefix+"/") || strings.Contains(path, "..") {
		return fmt.Errorf("chunk path escapes data prefix: %s", path)
	}
	return nil
}
```

Add `"io"`, `"gloak/internal/storage"` to the imports.

- [ ] **Step 5: Rewrite verify.go**

Replace the body of `internal/flow/verify.go` (keep package, imports style, and the exported `RunVerify` wrapper with pterm):

```go
func RunVerify(snapshotID, identityPath, remote string) error {
	backend := storage.NewRcloneBackend(remote)
	spinner, _ := pterm.DefaultSpinner.Start("Verifying backup...")
	err := runVerify(snapshotID, identityPath, backend)
	if err != nil {
		_ = spinner.Stop()
		return err
	}
	_ = spinner.Stop()
	pterm.Success.Println("Backup verified successfully")
	return nil
}

func runVerify(snapshotID, identityPath string, backend storage.Backend) error {
	r, err := repo.Open(backend, identityPath)
	if err != nil {
		return err
	}
	manifest, err := readManifest(backend, identityPath, snapshotID)
	if err != nil {
		return err
	}
	for _, meta := range manifest.Chunks {
		if err := validateChunkPath(meta.Name); err != nil {
			return err
		}
		var buf bytes.Buffer
		if err := backend.Download(meta.Name, &buf); err != nil {
			return fmt.Errorf("CHUNK CORRUPT %d (%s): missing object: %w", meta.Index, meta.Name, err)
		}
		sealed := buf.Bytes()
		encSum := sha256.Sum256(sealed)
		if hex.EncodeToString(encSum[:]) != meta.EncryptedSHA256 {
			return fmt.Errorf("CHUNK CORRUPT %d (%s): encrypted sha256 mismatch", meta.Index, meta.Name)
		}
		plain, err := crypto.OpenChunk(r.MasterKey, meta.PlainSHA256, sealed)
		if err != nil {
			return fmt.Errorf("CHUNK CORRUPT %d (%s): %w", meta.Index, meta.Name, err)
		}
		plainSum := sha256.Sum256(plain)
		if hex.EncodeToString(plainSum[:]) != meta.PlainSHA256 || int64(len(plain)) != meta.PlainSize {
			return fmt.Errorf("CHUNK CORRUPT %d (%s): plaintext mismatch", meta.Index, meta.Name)
		}
	}
	return nil
}
```

- [ ] **Step 6: Rewrite restore.go**

Keep the exported `RunRestore` wrapper and output-dir creation; replace the core loop:

```go
func runRestore(snapshotID, outputDir, identityPath string, backend storage.Backend) (string, error) {
	r, err := repo.Open(backend, identityPath)
	if err != nil {
		return "", err
	}
	manifest, err := readManifest(backend, identityPath, snapshotID)
	if err != nil {
		return "", err
	}

	name := filepath.Base(manifest.OriginalPath)
	if name == "" || name == "." || name == string(filepath.Separator) || filepath.Base(name) != name {
		return "", fmt.Errorf("unsafe filename in manifest: %q", manifest.OriginalPath)
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create output dir: %w", err)
	}
	outputPath := filepath.Join(outputDir, name)

	outFile, err := os.Create(outputPath)
	if err != nil {
		return "", fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	for _, meta := range manifest.Chunks {
		if err := validateChunkPath(meta.Name); err != nil {
			return "", err
		}
		var buf bytes.Buffer
		if err := backend.Download(meta.Name, &buf); err != nil {
			return "", fmt.Errorf("CHUNK CORRUPT %d (%s): missing object: %w", meta.Index, meta.Name, err)
		}
		plain, err := crypto.OpenChunk(r.MasterKey, meta.PlainSHA256, buf.Bytes())
		if err != nil {
			return "", fmt.Errorf("CHUNK CORRUPT %d (%s): %w", meta.Index, meta.Name, err)
		}
		if _, err := outFile.Write(plain); err != nil {
			return "", fmt.Errorf("failed to write chunk %d: %w", meta.Index, err)
		}
	}
	return outputPath, nil
}
```

Adapt `manifest.OriginalPath` to whichever field the existing `core.Manifest` actually uses for the source filename (check `internal/core/manifest.go`; if the field is `BackupPath` only, use that).

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/flow/ -v && go test ./...`
Expected: PASS. The two rewritten tests plus the traversal test pass; no remaining references to v1 paths (`chunk_0000`, `manifest.age`, `locator.json`) in the flow package.

- [ ] **Step 8: Commit**

```bash
git add internal/flow/verify.go internal/flow/restore.go internal/flow/manifest_read.go internal/flow/flow_test.go internal/storage/memtest/memory.go
git commit -m "feat(flow): rewrite verify and restore for repository layout"
```

---

### Task 9: CLI switchover — add `backup`, drop `upload`/`resume`/`cleanup`

**Files:**
- Create: root `backup.go`
- Modify: `main.go` (command table: remove Upload/Resume/Cleanup entries, keep Init from Task 6, add Backup)
- Modify: `main_test.go` (remove UploadCmd/CleanupCmd short-tag cases, add BackupCmd)
- Delete: root `upload.go`, `resume.go`, `cleanup.go`
- Delete: `internal/flow/upload.go`, `resume.go`, `upload_state.go`, `resume_test.go`, `upload_state_test.go`
- Modify: `internal/flow/flow_test.go` (remove TestRunUpload*, TestRunResume* and any helpers only they used)
- Modify: `internal/core/manifest.go` (keep only `Manifest` struct and `GenerateUUID`; delete `Locator`, `CreateLocator`, `SerializeLocator`, `CreateManifest`, `EncryptManifest`)
- Delete: `internal/core/manifest_test.go` (tests for the deleted functions; if it also tests `Manifest` JSON shape that backup still relies on, keep those cases and delete only the Locator/CreateManifest ones)

**Interfaces:**
- Consumes: `flow.RunBackup` (Task 7).
- Produces:
  - `BackupCmd` with `ExistingFile string` positional arg, `-i/--identity`, `-R/--remote` (required). Same flag names as today's UploadCmd so muscle memory and README examples stay close.
  - CLI surface after this task: `keygen`, `setup`, `init`, `backup`, `verify`, `restore`. No upload/resume/cleanup.
  - Root `backup.go`:

```go
package main

import (
	"fmt"

	"github.com/alecthomas/kong"

	"gloak/internal/flow"
)

// BackupCmd backs up a file into a gloak repository.
type BackupCmd struct {
	ExistingFile string `arg:"" name:"existingfile" help:"File to back up"`
	Identity     string `short:"i" default:"~/.gloak/identity.txt" help:"Path to identity file"`
	Remote       string `short:"R" required:"" help:"Repository remote, e.g. b2:my-bucket/gloak"`
}

func (c *BackupCmd) Run(ctx *kong.Context) error {
	identityPath, err := expandHome(c.Identity)
	if err != nil {
		return err
	}
	if _, err := flow.RunBackup(c.ExistingFile, identityPath, c.Remote); err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}
	return nil
}
```

Note: check how existing root commands expand `~` (verify.go/restore.go already do this); reuse that exact helper name instead of inventing `expandHome` if one already exists in package main.

- [ ] **Step 1: Confirm no remaining external callers of deleted symbols**

Run: `grep -rn "flow.RunUpload\|flow.RunResume\|core.CreateManifest\|core.EncryptManifest\|core.Locator\|UploadCmd\|ResumeCmd\|CleanupCmd" --include='*.go' .`
Purpose: list every reference that Task 9's deletions must also update (expected: main.go command table, main_test.go, root upload/resume/cleanup files themselves, flow tests).

- [ ] **Step 2: Add BackupCmd, update main.go and main_test.go**

Do the additions first (green intermediate state), then delete.

- [ ] **Step 3: Delete v1 files and symbols**

```bash
git rm internal/flow/upload.go internal/flow/resume.go internal/flow/upload_state.go \
       internal/flow/resume_test.go internal/flow/upload_state_test.go \
       upload.go resume.go cleanup.go
```

Then edit `internal/flow/flow_test.go` to drop TestRunUpload*/TestRunResume* and their private helpers (`writeFlowIdentityFile` stays if backup/verify/restore tests still use it; otherwise remove). Edit `internal/core/manifest.go` down to `Manifest` + `GenerateUUID`; delete `internal/core/manifest_test.go` cases that tested removed functions.

- [ ] **Step 4: Run tests and fix compile errors**

Run: `go test ./...`
Expected: everything green. If `core.ChunkMeta` lost its only constructor (`CreateManifest`), ensure `internal/core/chunk.go` still compiles and backup builds `ChunkMeta` literals directly (Task 7 already does).

- [ ] **Step 5: Manual smoke**

Run: `go build -o /tmp/gloak-smoke . && /tmp/gloak-smoke --help`
Expected: commands listed are exactly `keygen`, `setup`, `init`, `backup`, `verify`, `restore` (plus any hidden). No upload/resume/cleanup.

- [ ] **Step 6: Commit**

```bash
git add -A -- . ':!docs/superpowers' ':!README.md' ':!.gitignore' ':!docs/GUIDE.md'
git commit -m "feat(cli): switch to init/backup commands, drop v1 path"
```

(Stage explicitly if `-A` risks pulling in the user's uncommitted README/.gitignore/GUIDE.md changes; those must not be part of this commit.)

---

### Task 10: Documentation, spec amendment, handoff

**Files:**
- Modify: `README.md` (coordinate with the user's uncommitted changes first — ask before editing)
- Create: `CHANGELOG.md`
- Modify: `docs/superpowers/specs/2026-10-11-gloak-repo-model-design.md` (one-line amendment)
- Create: `docs/GUIDE.md` already exists untracked — leave it alone unless the user asks

Apply the humanizer + avoid-ai-writing skills to every document before committing: no em dashes, no "robust/seamless/crucial/comprehensive" register, sentence-case headings, plain technical English.

- [ ] **Step 1: Ask the user about README.md**

The working tree has uncommitted README edits. Ask (question tool, Indonesian, brief): should Task 10 rewrite README on top of their edits, wait until they finish, or leave README for them and only ship CHANGELOG + spec amendment?

- [ ] **Step 2: Update README** (if approved)

- Commands section: replace upload/resume/cleanup examples with `gloak init -R <remote> -r <keyfile>` and `gloak backup <file> -R <remote>`.
- Add a short "Repository layout" subsection listing `repo.json`, `keys/`, `data/{hh}/`, `index/`, `snapshots/` with one-line purposes.
- Keep the existing `CHUNK CORRUPT` / exit-code documentation; it still holds.
- Add a one-line break notice at the top of the relevant section: format `filesystem-v1` is gone; old backups made with pre-repository gloak cannot be read by this version.

- [ ] **Step 3: Create CHANGELOG.md**

Unreleased section describing: repository model (`init`/`backup`), chunk dedup via epoch index, multi-recipient snapshots, removal of upload/resume/cleanup and of `filesystem-v1`, and the rclone config-path fix from Task 1.

- [ ] **Step 4: Amend the spec**

In the storage-layout comment of the approved spec, change the `repo.json` recipients note from fingerprints to full public keys, and add one sentence: "Full recipient strings (not fingerprints) are stored so backup can seal snapshots without the identity file's public key line." Commit this with a `docs:` conventional message.

- [ ] **Step 5: Final verification**

Run: `go test ./... && go vet ./... && go build ./...`
Expected: all green. Then `git log --oneline` should show the nine implementation commits plus docs commits, newest on top, none pushed.

- [ ] **Step 6: Draft commits and ask**

Create draft commits (git-commit skill, humanized messages). Then ask the user whether to keep them as-is, squash anything, or adjust wording. Never push.

- [ ] **Step 7: Offer execution choice**

Using the question tool (Indonesian, brief), offer: subagent-driven execution (one fresh subagent per task, spec review between) or inline execution in this session (TDD, user reviews at checkpoints).

---

## Post-Completion

1. **Verify** every task's checkbox was ticked and `git log --oneline` matches the nine code commits + docs commits. Re-run `go test ./...` once from a clean shell.
2. **Summarize** what shipped (init/backup/verify/restore on layout v2, index dedup, multi-recipient) and what was deliberately removed (upload/resume/cleanup, filesystem-v1) in a short user-facing recap, in Indonesian.
3. **Stop.** Do not start Phase 2 (full-featured prune, CDC chunker, pack files, compression-tier selection) without a fresh design discussion; that is a separate spec and a separate plan.





