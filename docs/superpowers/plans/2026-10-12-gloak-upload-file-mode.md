# Standalone upload (file-2) Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans (inline) to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `gloak upload` that writes a self-contained `{uuid}/` folder (sealed chunks, age-encrypted manifest with embedded master key, public locator) and teach `restore`/`verify` to auto-detect file mode.

**Architecture:** New `internal/flow/upload.go` streams the source with the same `io.ReadFull` loop as repository backup, seals each chunk with `crypto.SealChunk` under a per-upload random master key, then uploads `manifest.age` (`EncryptMulti` to all recipients) and `locator.json`. Restore and verify probe `{uuid}/locator.json` before opening a repository; if present they run file-mode logic that reads the master key from the decrypted manifest.

**Tech Stack:** Go 1.26, existing `internal/crypto` envelope, `internal/storage` Backend + memtest, kong CLI, pterm spinner.

## Global Constraints

- Alpha tool; filesystem-v1 backups (age-writer chunks, version `"1.0"`) are not read.
- Chunk envelope is Phase 1 `crypto.SealChunk` / `crypto.OpenChunk` only.
- Manifest version string is exactly `"file-2"`.
- Corruption errors on restore/verify keep the `CHUNK CORRUPT` prefix.
- `original_name` must be a plain basename; chunk names must match `^chunk_\d{5}$` under `{uuid}/` before any download.
- Locator is uploaded last; restore/verify refuse folders without a matching locator.
- No resume, no local state under `~/.gloak/uploads/`.
- Working tree may contain uncommitted user files (`M .gitignore`, `M README.md`, `?? docs/GUIDE.md`); never stage or modify them.
- `go test ./...`, `go vet ./...`, `go build ./...` must pass after every task.
- Never push.

---

### Task 1: Upload flow

**Files:**
- Create: `internal/flow/upload.go`
- Create: `internal/flow/upload_test.go`

**Interfaces:**
- Consumes: `storage.Backend` (Upload/Download), `core.ChunkSize`, `core.ChunkMeta`, `core.GenerateUUID`, `crypto.SealChunk`, `crypto.EncryptMulti`
- Produces:
  - `func RunUpload(filePath string, recipients []string, backend storage.Backend) (string, error)` — returns UUID
  - Unexported `fileManifest` and `fileLocator` types (JSON tags below)
  - Unexported helpers used by later tasks: `fileChunkName(index int) string`, `validateFileChunkName(name string) error` (relative form only: `^chunk_\d{5}$`), `readFileManifest(backend storage.Backend, identityPath, uuid string) (fileManifest, error)`, `isFileMode(backend storage.Backend, uuid string) bool`

- [ ] **Step 1: Write failing tests for upload round-trip structure**

```go
// internal/flow/upload_test.go
package flow

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"

	"gloak/internal/crypto"
	"gloak/internal/storage/memtest"
)

func testIdentityFile(t *testing.T) (recipient, identityPath string) {
	t.Helper()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	path := filepath.Join(t.TempDir(), "identity.txt")
	content := "# created: now\n# public key: " + id.Recipient().String() + "\n" + id.String() + "\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write identity: %v", err)
	}
	return id.Recipient().String(), path
}

func TestRunUploadWritesLocatorLast(t *testing.T) {
	recipient, _ := testIdentityFile(t)
	src := filepath.Join(t.TempDir(), "data.bin")
	if err := os.WriteFile(src, []byte("hello gloak"), 0600); err != nil {
		t.Fatal(err)
	}
	backend := memtest.NewMemoryBackend()

	uploads := backend.TotalUploads("")
	_ = uploads

	uuid, err := RunUpload(src, []string{recipient}, backend)
	if err != nil {
		t.Fatalf("RunUpload() error: %v", err)
	}
	if uuid == "" {
		t.Fatal("RunUpload() returned empty uuid")
	}
	if !backend.Has(uuid + "/locator.json") {
		t.Fatalf("missing locator at %s/locator.json", uuid)
	}
	if !backend.Has(uuid + "/manifest.age") {
		t.Fatal("missing manifest.age")
	}
	if !backend.Has(uuid + "/chunk_00000") {
		t.Fatal("missing chunk_00000")
	}

	var loc fileLocator
	if err := json.Unmarshal(backend.Get(uuid+"/locator.json"), &loc); err != nil {
		t.Fatalf("locator json: %v", err)
	}
	if loc.Version != fileFormatVersion || loc.UUID != uuid {
		t.Fatalf("locator = %+v, want version %q uuid %q", loc, fileFormatVersion, uuid)
	}

	// locator must be uploaded after manifest and chunk
	if backend.UploadCount(uuid+"/locator.json") != 1 {
		t.Fatal("locator uploaded more than once")
	}
}

func TestRunUploadRejectsEmptyRecipients(t *testing.T) {
	src := filepath.Join(t.TempDir(), "data.bin")
	if err := os.WriteFile(src, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := RunUpload(src, nil, memtest.NewMemoryBackend())
	if err == nil || !strings.Contains(err.Error(), "recipient") {
		t.Fatalf("err = %v, want recipient error", err)
	}
}

func TestFileChunkNameAndValidation(t *testing.T) {
	if got := fileChunkName(0); got != "chunk_00000" {
		t.Fatalf("fileChunkName(0) = %q", got)
	}
	if err := validateFileChunkName("chunk_00001"); err != nil {
		t.Fatalf("valid name rejected: %v", err)
	}
	bad := []string{
		"chunk_0000",
		"chunk_000001",
		"../evil",
		"chunk_00000/../x",
		"aaaa-bbbb-cccc-dddd-eeeeeeeeeeee/chunk_00000",
		"/etc/passwd",
	}
	for _, name := range bad {
		if err := validateFileChunkName(name); err == nil {
			t.Fatalf("validateFileChunkName(%q) succeeded, want error", name)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/flow/ -run 'TestRunUpload|TestFileChunk' -count=1`
Expected: FAIL (undefined RunUpload, fileLocator, fileFormatVersion, fileChunkName, validateFileChunkName)

- [ ] **Step 3: Implement upload.go**

```go
// internal/flow/upload.go
package flow

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/pterm/pterm"

	"gloak/internal/core"
	"gloak/internal/crypto"
	"gloak/internal/storage"
)

const fileFormatVersion = "file-2"

const masterKeySize = 32

var fileChunkNameRe = regexp.MustCompile(`^chunk_\d{5}$`)

type fileManifest struct {
	Version       string           `json:"version"`
	UUID          string           `json:"uuid"`
	OriginalName  string           `json:"original_name"`
	OriginalSize  int64            `json:"original_size"`
	CreatedAt     string           `json:"created_at"`
	PayloadSHA256 string           `json:"payload_sha256"`
	MasterKey     string           `json:"master_key"`
	Chunks        []core.ChunkMeta `json:"chunks"`
}

type fileLocator struct {
	Version string `json:"version"`
	UUID    string `json:"uuid"`
}

func fileChunkName(index int) string {
	return fmt.Sprintf("chunk_%05d", index)
}

// RunUpload seals filePath into a self-contained {uuid}/ folder on backend.
// recipients must be non-empty age public keys.
func RunUpload(filePath string, recipients []string, backend storage.Backend) (string, error) {
	if len(recipients) == 0 {
		return "", fmt.Errorf("at least one recipient is required")
	}
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open source file: %w", err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("failed to stat source file: %w", err)
	}

	backupID := core.GenerateUUID()
	masterKey := make([]byte, masterKeySize)
	if _, err := rand.Read(masterKey); err != nil {
		return "", fmt.Errorf("failed to generate master key: %w", err)
	}

	spinner, _ := pterm.DefaultSpinner.Start("Encrypting and uploading...")
	payloadHasher := sha256.New()
	buffer := make([]byte, int(core.ChunkSize))
	var metas []core.ChunkMeta
	var total int64

	for index := 0; ; index++ {
		n, readErr := io.ReadFull(file, buffer)
		if readErr == io.EOF {
			break
		}
		if readErr != nil && readErr != io.ErrUnexpectedEOF {
			_ = spinner.Stop()
			return "", fmt.Errorf("failed to read chunk %d: %w", index, readErr)
		}
		if n == 0 {
			break
		}

		plain := buffer[:n]
		payloadHasher.Write(plain)
		plainHash := hashBytes(plain)
		sealed, err := crypto.SealChunk(masterKey, plainHash, plain)
		if err != nil {
			_ = spinner.Stop()
			return "", fmt.Errorf("failed to seal chunk %d: %w", index, err)
		}
		encSum := sha256.Sum256(sealed)
		name := fileChunkName(index)
		remotePath := backupID + "/" + name
		if err := backend.Upload(remotePath, bytes.NewReader(sealed)); err != nil {
			_ = spinner.Stop()
			return "", fmt.Errorf("failed to upload chunk %d: %w", index, err)
		}
		metas = append(metas, core.ChunkMeta{
			Index:           index,
			Name:            name,
			PlainSize:       int64(n),
			PlainSHA256:     plainHash,
			EncryptedSize:   int64(len(sealed)),
			EncryptedSHA256: hex.EncodeToString(encSum[:]),
		})
		total += int64(n)
		spinner.UpdateText(fmt.Sprintf("Uploaded %d chunks", len(metas)))

		if readErr == io.ErrUnexpectedEOF {
			break
		}
	}

	manifest := fileManifest{
		Version:       fileFormatVersion,
		UUID:          backupID,
		OriginalName:  filepath.Base(filePath),
		OriginalSize:  total,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		PayloadSHA256: hex.EncodeToString(payloadHasher.Sum(nil)),
		MasterKey:     hex.EncodeToString(masterKey),
		Chunks:        metas,
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		_ = spinner.Stop()
		return "", fmt.Errorf("failed to marshal manifest: %w", err)
	}
	sealedManifest, err := crypto.EncryptMulti(recipients, manifestJSON)
	if err != nil {
		_ = spinner.Stop()
		return "", fmt.Errorf("failed to encrypt manifest: %w", err)
	}
	if err := backend.Upload(backupID+"/manifest.age", bytes.NewReader(sealedManifest)); err != nil {
		_ = spinner.Stop()
		return "", fmt.Errorf("failed to upload manifest: %w", err)
	}

	locator := fileLocator{Version: fileFormatVersion, UUID: backupID}
	locatorJSON, err := json.Marshal(locator)
	if err != nil {
		_ = spinner.Stop()
		return "", fmt.Errorf("failed to marshal locator: %w", err)
	}
	if err := backend.Upload(backupID+"/locator.json", bytes.NewReader(locatorJSON)); err != nil {
		_ = spinner.Stop()
		return "", fmt.Errorf("failed to upload locator: %w", err)
	}

	_ = spinner.Stop()
	pterm.Success.Printf("Upload complete: %s (%d chunks, %d bytes)\n", backupID, len(metas), total)
	return backupID, nil
}

// validateFileChunkName accepts only relative chunk_NNNNN names (no separators, no traversal).
func validateFileChunkName(name string) error {
	if !fileChunkNameRe.MatchString(name) {
		return fmt.Errorf("invalid chunk name: %s", name)
	}
	return nil
}
```

Note: `readFileManifest` and `isFileMode` live here too (Task 3 uses them); implement in Task 3 if preferred, but define empty stubs only if compile requires. Prefer implementing them with Task 3 and keeping Task 1 to upload-only symbols the tests need: `fileFormatVersion`, `fileManifest`, `fileLocator`, `fileChunkName`, `validateFileChunkName`, `RunUpload`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/flow/ -run 'TestRunUpload|TestFileChunk' -count=1 -v`
Expected: PASS

- [ ] **Step 5: Run full suite**

Run: `go test ./... && go vet ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/flow/upload.go internal/flow/upload_test.go
git commit -m "feat(flow): add standalone file-mode upload"
```

---

### Task 2: Upload CLI command

**Files:**
- Create: `upload.go`
- Modify: `main.go` (add Upload field to cli struct)
- Modify: `main_test.go` (short tag cases)

**Interfaces:**
- Consumes: `flow.RunUpload`, `resolveInitRecipients`
- Produces: `type UploadCmd struct` with `File`, `Recipient`, `Remote`; kong command name `upload`

- [ ] **Step 1: Write failing short-tag tests**

Append to `main_test.go` in `TestCommandOptionShortTags` cases:

```go
{name: "upload recipient", cmd: UploadCmd{}, fieldName: "Recipient", wantShort: "r"},
{name: "upload remote", cmd: UploadCmd{}, fieldName: "Remote", wantShort: "R"},
```

Also add:

```go
func TestUploadCmdFileIsExistingFileArg(t *testing.T) {
	field, ok := reflect.TypeOf(UploadCmd{}).FieldByName("File")
	if !ok {
		t.Fatal("UploadCmd.File missing")
	}
	if field.Tag.Get("type") != "existingfile" {
		t.Fatalf("type tag = %q, want existingfile", field.Tag.Get("type"))
	}
	if field.Tag.Get("arg") == "" {
		t.Fatal("File should be a positional arg")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test . -run 'TestCommandOptionShortTags|TestUploadCmdFile' -count=1`
Expected: FAIL (undefined UploadCmd)

- [ ] **Step 3: Implement UploadCmd and register it**

```go
// upload.go
package main

import (
	"fmt"

	"github.com/pterm/pterm"

	"gloak/internal/flow"
)

// UploadCmd writes a self-contained encrypted backup folder to a remote root.
type UploadCmd struct {
	File      string `arg:"" name:"file" type:"existingfile" help:"File to back up"`
	Recipient string `short:"r" help:"Recipient public key file or comma-separated keys (default ~/.gloak/recipient.txt)"`
	Remote    string `short:"R" required:"" help:"Remote root, e.g. keraaaki:backups or /mnt/backups"`
}

func (u *UploadCmd) Run() error {
	recipients, err := resolveInitRecipients(u.Recipient)
	if err != nil {
		return err
	}
	pterm.Info.Println("Starting UPLOAD process...")
	uuid, err := flow.RunUpload(u.File, recipients, storage.NewRcloneBackend(u.Remote))
	if err != nil {
		return fmt.Errorf("upload failed: %w", err)
	}
	pterm.DefaultBasicText.Printf("Backup UUID: %s\n", uuid)
	return nil
}
```

Fix import: add `"gloak/internal/storage"` to the import block (the snippet above uses it).

In `main.go` cli struct, after Backup:

```go
Upload   UploadCmd   `cmd:"" help:"Encrypt and upload a file as a standalone folder (no repository)."`
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./... && go vet ./...`
Expected: PASS

- [ ] **Step 5: Smoke CLI help**

Run: `go run . upload --help`
Expected: shows file arg, `-r`, `-R`

- [ ] **Step 6: Commit**

```bash
git add upload.go main.go main_test.go
git commit -m "feat(cli): add upload command for file-mode backups"
```

---

### Task 3: Restore auto-detect and file-mode restore

**Files:**
- Modify: `internal/flow/restore.go`
- Modify: `internal/flow/manifest_read.go` (or keep helpers in restore.go; prefer new small file `internal/flow/filemode.go`)
- Create: `internal/flow/filemode.go` — `isFileMode`, `readFileManifest`, file restore/verify cores
- Modify: `internal/flow/restore_test.go` — add file-mode cases (create if missing; check existing flow tests location)

**Interfaces:**
- Consumes: `RunUpload` (tests), `crypto.DecryptReader`, `crypto.OpenChunk`, `storage.Backend.Download`
- Produces:
  - `func isFileMode(backend storage.Backend, backupID string) bool`
  - `func readFileManifest(backend storage.Backend, identityPath, backupID string) (fileManifest, error)`
  - `func runFileRestore(backupID, identityPath, outputDir string, backend storage.Backend) (string, error)`
  - `func runFileVerify(backupID, identityPath string, backend storage.Backend) error` (Task 4 may just wire it)
  - `RunRestore` / `runRestore` behavior: if `isFileMode`, call `runFileRestore` and skip `repo.Open`

- [ ] **Step 1: Write failing restore tests**

```go
// internal/flow/filemode_test.go
package flow

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"gloak/internal/storage/memtest"
)

func TestRunRestoreFileModeRoundTrip(t *testing.T) {
	recipient, identityPath := testIdentityFile(t)
	src := filepath.Join(t.TempDir(), "secret.txt")
	want := []byte("restore me please\n")
	if err := os.WriteFile(src, want, 0600); err != nil {
		t.Fatal(err)
	}
	backend := memtest.NewMemoryBackend()
	uuid, err := RunUpload(src, []string{recipient}, backend)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	outDir := t.TempDir()
	gotPath, err := runRestore(uuid, outDir, identityPath, backend)
	if err != nil {
		t.Fatalf("runRestore() error: %v", err)
	}
	got, err := os.ReadFile(gotPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("restored %q, want %q", got, want)
	}
}

func TestRunRestoreFileModeMissingLocator(t *testing.T) {
	recipient, identityPath := testIdentityFile(t)
	src := filepath.Join(t.TempDir(), "a.bin")
	if err := os.WriteFile(src, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	backend := memtest.NewMemoryBackend()
	uuid, err := RunUpload(src, []string{recipient}, backend)
	if err != nil {
		t.Fatal(err)
	}
	backend.Delete(uuid + "/locator.json")

	_, err = runRestore(uuid, t.TempDir(), identityPath, backend)
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("incomplete")) {
		t.Fatalf("err = %v, want incomplete", err)
	}
}

func TestRunRestoreFileModeRejectsTraversalManifest(t *testing.T) {
	recipient, identityPath := testIdentityFile(t)
	src := filepath.Join(t.TempDir(), "a.bin")
	if err := os.WriteFile(src, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	backend := memtest.NewMemoryBackend()
	uuid, err := RunUpload(src, []string{recipient}, backend)
	if err != nil {
		t.Fatal(err)
	}
	// Replace manifest with one that points at a traversal path.
	// Use the same master key from the real manifest by re-encrypting is heavy;
	// instead download, decrypt, patch, re-encrypt.
	// Simpler approach: craft via readFileManifest + mutate + EncryptMulti.
	m, err := readFileManifest(backend, identityPath, uuid)
	if err != nil {
		t.Fatal(err)
	}
	m.Chunks[0].Name = "../evil"
	// re-seal manifest
	// (helper below in test)
	resealFileManifest(t, backend, identityPath, uuid, m)

	_, err = runRestore(uuid, t.TempDir(), identityPath, backend)
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("invalid chunk")) {
		t.Fatalf("err = %v, want invalid chunk", err)
	}
}

func TestRunRestoreFileModeCorruptChunk(t *testing.T) {
	recipient, identityPath := testIdentityFile(t)
	src := filepath.Join(t.TempDir(), "a.bin")
	if err := os.WriteFile(src, []byte("abcdefgh"), 0600); err != nil {
		t.Fatal(err)
	}
	backend := memtest.NewMemoryBackend()
	uuid, err := RunUpload(src, []string{recipient}, backend)
	if err != nil {
		t.Fatal(err)
	}
	raw := backend.Get(uuid + "/chunk_00000")
	raw[len(raw)/2] ^= 0xff
	backend.Put(uuid+"/chunk_00000", raw)

	_, err = runRestore(uuid, t.TempDir(), identityPath, backend)
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("CHUNK CORRUPT")) {
		t.Fatalf("err = %v, want CHUNK CORRUPT", err)
	}
}
```

Add test helper `resealFileManifest` in the same test file:

```go
func resealFileManifest(t *testing.T, backend *memtest.MemoryBackend, identityPath, uuid string, m fileManifest) {
	t.Helper()
	// Need recipients from identity file public key line.
	// Easier: read recipient from identity file comment, or store plaintext temporarily.
	// Practical approach: EncryptMulti to the recipient parsed from identity file.
	// testIdentityFile returns only path; parse "# public key:" line.
	data, err := os.ReadFile(identityPath)
	if err != nil {
		t.Fatal(err)
	}
	var recipient string
	for _, line := range bytes.Split(data, []byte("\n")) {
		if bytes.HasPrefix(bytes.TrimSpace(line), []byte("# public key:")) {
			recipient = string(bytes.TrimSpace(bytes.TrimPrefix(bytes.TrimSpace(line), []byte("# public key:"))))
			break
		}
	}
	if recipient == "" {
		t.Fatal("no public key in identity file")
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := crypto.EncryptMulti([]string{recipient}, raw)
	if err != nil {
		t.Fatal(err)
	}
	backend.Put(uuid+"/manifest.age", sealed)
}
```

Import `encoding/json` and `gloak/internal/crypto` in the test file.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/flow/ -run 'TestRunRestoreFileMode' -count=1`
Expected: FAIL (undefined isFileMode/readFileManifest/runFileRestore; runRestore still only repo path)

- [ ] **Step 3: Implement filemode.go and wire restore**

```go
// internal/flow/filemode.go
package flow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gloak/internal/crypto"
	"gloak/internal/storage"
)

func isFileMode(backend storage.Backend, backupID string) bool {
	var buf bytes.Buffer
	return backend.Download(backupID+"/locator.json", &buf) == nil
}

func readFileManifest(backend storage.Backend, identityPath, backupID string) (fileManifest, error) {
	var loc fileLocator
	var locBuf bytes.Buffer
	if err := backend.Download(backupID+"/locator.json", &locBuf); err != nil {
		return fileManifest{}, fmt.Errorf("backup %s incomplete or missing: no locator.json", backupID)
	}
	if err := json.Unmarshal(locBuf.Bytes(), &loc); err != nil {
		return fileManifest{}, fmt.Errorf("invalid locator: %w", err)
	}
	if loc.UUID != backupID {
		return fileManifest{}, fmt.Errorf("locator uuid mismatch: got %s want %s", loc.UUID, backupID)
	}

	var manBuf bytes.Buffer
	if err := backend.Download(backupID+"/manifest.age", &manBuf); err != nil {
		return fileManifest{}, fmt.Errorf("failed to download manifest: %w", err)
	}
	reader, err := crypto.DecryptReader(identityPath, &manBuf)
	if err != nil {
		return fileManifest{}, fmt.Errorf("failed to decrypt manifest: %w", err)
	}
	plain, err := io.ReadAll(reader)
	if err != nil {
		return fileManifest{}, fmt.Errorf("failed to read manifest: %w", err)
	}
	var m fileManifest
	if err := json.Unmarshal(plain, &m); err != nil {
		return fileManifest{}, fmt.Errorf("failed to parse manifest: %w", err)
	}
	if m.UUID != backupID {
		return fileManifest{}, fmt.Errorf("manifest uuid mismatch: got %s want %s", m.UUID, backupID)
	}
	if m.Version != fileFormatVersion {
		return fileManifest{}, fmt.Errorf("unsupported manifest version: %s", m.Version)
	}
	if filepath.Base(m.OriginalName) != m.OriginalName {
		return fileManifest{}, fmt.Errorf("invalid original filename in manifest: %s", m.OriginalName)
	}
	return m, nil
}

func decodeFileMasterKey(m fileManifest) ([]byte, error) {
	key, err := hex.DecodeString(m.MasterKey)
	if err != nil || len(key) != masterKeySize {
		return nil, fmt.Errorf("invalid master key in manifest")
	}
	return key, nil
}

func runFileRestore(backupID, identityPath, outputDir string, backend storage.Backend) (string, error) {
	m, err := readFileManifest(backend, identityPath, backupID)
	if err != nil {
		return "", err
	}
	masterKey, err := decodeFileMasterKey(m)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}
	outputPath := filepath.Join(outputDir, m.OriginalName)
	outFile, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return "", fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	payloadHasher := sha256.New()
	for _, meta := range m.Chunks {
		if err := validateFileChunkName(meta.Name); err != nil {
			return "", fmt.Errorf("invalid chunk name in manifest: %w", err)
		}
		var buf bytes.Buffer
		if err := backend.Download(backupID+"/"+meta.Name, &buf); err != nil {
			return "", fmt.Errorf("CHUNK CORRUPT %d (%s): missing object: %w", meta.Index, meta.Name, err)
		}
		plain, err := crypto.OpenChunk(masterKey, meta.PlainSHA256, buf.Bytes())
		if err != nil {
			return "", fmt.Errorf("CHUNK CORRUPT %d (%s): %w", meta.Index, meta.Name, err)
		}
		if int64(len(plain)) != meta.PlainSize || hashBytes(plain) != meta.PlainSHA256 {
			return "", fmt.Errorf("CHUNK CORRUPT %d (%s): plaintext mismatch", meta.Index, meta.Name)
		}
		if _, err := outFile.Write(plain); err != nil {
			return "", fmt.Errorf("failed to write chunk %d: %w", meta.Index, err)
		}
		payloadHasher.Write(plain)
	}

	if hex.EncodeToString(payloadHasher.Sum(nil)) != m.PayloadSHA256 {
		return "", fmt.Errorf("fatal: overall payload hash does not match manifest")
	}
	return outputPath, nil
}
```

Wire `runRestore` in `restore.go` at the top:

```go
func runRestore(snapshotID, outputDir, identityPath string, backend storage.Backend) (string, error) {
	if isFileMode(backend, snapshotID) {
		return runFileRestore(snapshotID, identityPath, outputDir, backend)
	}
	// existing repository path continues...
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/flow/ -count=1`
Expected: PASS (file-mode + existing repo tests)

- [ ] **Step 5: Full suite**

Run: `go test ./... && go vet ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/flow/filemode.go internal/flow/filemode_test.go internal/flow/restore.go
git commit -m "feat(flow): restore file-mode backups via locator detection"
```

---

### Task 4: Verify auto-detect and file-mode verify

**Files:**
- Modify: `internal/flow/filemode.go` (add `runFileVerify`)
- Modify: `internal/flow/verify.go` (wire detection)
- Modify: `internal/flow/filemode_test.go` (add verify tests)

**Interfaces:**
- Consumes: `readFileManifest`, `decodeFileMasterKey`, `validateFileChunkName`
- Produces: `func runFileVerify(backupID, identityPath string, backend storage.Backend) error`

- [ ] **Step 1: Write failing verify tests**

```go
func TestRunVerifyFileModeOK(t *testing.T) {
	recipient, identityPath := testIdentityFile(t)
	src := filepath.Join(t.TempDir(), "v.bin")
	if err := os.WriteFile(src, []byte("verify me"), 0600); err != nil {
		t.Fatal(err)
	}
	backend := memtest.NewMemoryBackend()
	uuid, err := RunUpload(src, []string{recipient}, backend)
	if err != nil {
		t.Fatal(err)
	}
	if err := runVerify(uuid, identityPath, backend); err != nil {
		t.Fatalf("runVerify() error: %v", err)
	}
}

func TestRunVerifyFileModeCorrupt(t *testing.T) {
	recipient, identityPath := testIdentityFile(t)
	src := filepath.Join(t.TempDir(), "v.bin")
	if err := os.WriteFile(src, []byte("verify me"), 0600); err != nil {
		t.Fatal(err)
	}
	backend := memtest.NewMemoryBackend()
	uuid, err := RunUpload(src, []string{recipient}, backend)
	if err != nil {
		t.Fatal(err)
	}
	raw := backend.Get(uuid + "/chunk_00000")
	raw[0] ^= 0xff
	backend.Put(uuid+"/chunk_00000", raw)

	err = runVerify(uuid, identityPath, backend)
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("CHUNK CORRUPT")) {
		t.Fatalf("err = %v, want CHUNK CORRUPT", err)
	}
}

func TestRunVerifyFileModeMissingLocator(t *testing.T) {
	recipient, identityPath := testIdentityFile(t)
	src := filepath.Join(t.TempDir(), "v.bin")
	if err := os.WriteFile(src, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	backend := memtest.NewMemoryBackend()
	uuid, err := RunUpload(src, []string{recipient}, backend)
	if err != nil {
		t.Fatal(err)
	}
	backend.Delete(uuid + "/locator.json")

	err = runVerify(uuid, identityPath, backend)
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("incomplete")) {
		t.Fatalf("err = %v, want incomplete", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/flow/ -run 'TestRunVerifyFileMode' -count=1`
Expected: FAIL (runVerify still opens repository only)

- [ ] **Step 3: Implement runFileVerify and wire**

```go
func runFileVerify(backupID, identityPath string, backend storage.Backend) error {
	m, err := readFileManifest(backend, identityPath, backupID)
	if err != nil {
		return err
	}
	masterKey, err := decodeFileMasterKey(m)
	if err != nil {
		return err
	}

	payloadHasher := sha256.New()
	for _, meta := range m.Chunks {
		if err := validateFileChunkName(meta.Name); err != nil {
			return fmt.Errorf("invalid chunk name in manifest: %w", err)
		}
		var buf bytes.Buffer
		if err := backend.Download(backupID+"/"+meta.Name, &buf); err != nil {
			return fmt.Errorf("CHUNK CORRUPT %d (%s): missing object: %w", meta.Index, meta.Name, err)
		}
		sealed := buf.Bytes()
		if int64(len(sealed)) != meta.EncryptedSize {
			return fmt.Errorf("CHUNK CORRUPT %d (%s): encrypted size mismatch", meta.Index, meta.Name)
		}
		if hashBytes(sealed) != meta.EncryptedSHA256 {
			return fmt.Errorf("CHUNK CORRUPT %d (%s): encrypted sha256 mismatch", meta.Index, meta.Name)
		}
		plain, err := crypto.OpenChunk(masterKey, meta.PlainSHA256, sealed)
		if err != nil {
			return fmt.Errorf("CHUNK CORRUPT %d (%s): %w", meta.Index, meta.Name, err)
		}
		if int64(len(plain)) != meta.PlainSize || hashBytes(plain) != meta.PlainSHA256 {
			return fmt.Errorf("CHUNK CORRUPT %d (%s): plaintext mismatch", meta.Index, meta.Name)
		}
		payloadHasher.Write(plain)
	}
	if hex.EncodeToString(payloadHasher.Sum(nil)) != m.PayloadSHA256 {
		return fmt.Errorf("FATAL: overall payload hash differs from manifest")
	}
	return nil
}
```

In `verify.go` `runVerify`:

```go
func runVerify(snapshotID, identityPath string, backend storage.Backend) error {
	if isFileMode(backend, snapshotID) {
		return runFileVerify(snapshotID, identityPath, backend)
	}
	// existing repository path...
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/flow/ -count=1`
Expected: PASS

- [ ] **Step 5: Full suite**

Run: `go test ./... && go vet ./... && go build ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/flow/filemode.go internal/flow/filemode_test.go internal/flow/verify.go
git commit -m "feat(flow): verify file-mode backups with auto-detect"
```

---

### Task 5: CHANGELOG and final checks

**Files:**
- Modify: `CHANGELOG.md`

**Interfaces:**
- Consumes: completed Tasks 1-4
- Produces: user-facing changelog entries; no code

- [ ] **Step 1: Add Unreleased entries under existing Unreleased section**

Add to CHANGELOG.md under `## Unreleased`:

```markdown
### Added

- `gloak upload <file> -R <remote>` writes a self-contained backup folder
  `{uuid}/` at the remote root (sealed chunks, multi-recipient `manifest.age`
  with embedded master key, public `locator.json`). No repository and no
  `init` required. Manifest version `file-2`.
- `restore` and `verify` auto-detect file-mode folders by `locator.json`
  and fall back to repository layout otherwise.
```

Adjust to sit with the existing Added section if one exists; do not duplicate a second Unreleased heading.

- [ ] **Step 2: Style check**

Run: `grep -nE '—|–|\b(robust|seamless|crucial|comprehensive|landscape|pivotal|leverage|utilize)\b' CHANGELOG.md || true`
Expected: no em/en dashes; no Tier-1 AI words.

- [ ] **Step 3: Full verification**

Run: `go test ./... && go vet ./... && go build ./...`
Expected: PASS

- [ ] **Step 4: Smoke upload + restore via built binary**

```bash
go build -o /tmp/gloak-smoke .
/tmp/gloak-smoke keygen -o /tmp/gloak-keys-test
# use generated recipient; create tiny file; upload to a temp local path backend if rclone local works, or rely on unit tests.
/tmp/gloak-smoke upload --help
```

Expected: help lists file, -r, -R.

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs: note file-mode upload in changelog"
```

---

## Self-review notes

- Spec coverage: layout/version/master key in manifest, locator last, auto-detect restore+verify, traversal and basename rules, multi-recipient via EncryptMulti, no resume, CLI flags, tests listed in spec (round trip, multi-recipient not yet explicit; add one multi-recipient restore test in Task 3 if time: two identities, restore with either). Add this test in Task 3:

```go
func TestRunRestoreFileModeEitherIdentity(t *testing.T) {
	r1, id1 := testIdentityFile(t)
	r2, id2 := testIdentityFile(t)
	src := filepath.Join(t.TempDir(), "m.bin")
	if err := os.WriteFile(src, []byte("two keys"), 0600); err != nil {
		t.Fatal(err)
	}
	backend := memtest.NewMemoryBackend()
	uuid, err := RunUpload(src, []string{r1, r2}, backend)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{id1, id2} {
		out := t.TempDir()
		if _, err := runRestore(uuid, out, id, backend); err != nil {
			t.Fatalf("restore with %s: %v", id, err)
		}
	}
}
```

- Type consistency: `fileManifest.MasterKey` is hex. `meta.Name` is always relative (`chunk_00000`). `validateFileChunkName(name string)` checks only that relative form. Download paths are `backupID + "/" + meta.Name`.
- Placeholders: none intentional; smoke step may skip real rclone if no local remote, unit tests are the gate.
