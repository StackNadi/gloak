// Package index tracks which chunks already exist in the repository so
// repeat backups upload nothing.
package index

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

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

// New returns an empty index with no pending entries and an epoch counter of zero.
func New() *Index {
	return &Index{
		pending: make(map[string]Entry),
		all:     make(map[string]Entry),
	}
}

// Load merges readable index files in ascending name order, with later entries
// replacing earlier entries for the same plaintext hash. Listing failures yield
// an empty index; malformed listing JSON returns an error. Files that cannot be
// downloaded, decrypted, read, or parsed are skipped. Skipped entries are not
// queued for Save. The epoch counter is restored from the highest recognized
// epoch name, including skipped files.
func Load(backend storage.Backend, identityPath string) (*Index, error) {
	ix := New()

	raw, err := backend.ListFiles([]string{indexPrefix})
	if err != nil {
		// A fresh repo has no index/ directory yet (rclone lsjson exits 3
		// on a missing directory); that is normal, so treat the listing as
		// empty and start from epoch 0. Any other listing failure is a real
		// error and must not be mistaken for an empty index.
		if storage.IsMissingPath(err) {
			return ix, nil
		}
		return nil, fmt.Errorf("failed to list index: %w", err)
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
		path := fmt.Sprintf("%s/%s", indexPrefix, name)
		// Count the epoch number even when the file's contents end up
		// skipped, so a later Save never reuses an existing name.
		if epoch, ok := parseEpoch(path); ok && epoch > ix.epoch {
			ix.epoch = epoch
		}
		var epochBuf bytes.Buffer
		if err := backend.Download(path, &epochBuf); err != nil {
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

// parseEpoch extracts the epoch number from a path like "index/000001.age".
// ok is false when the path does not match the zero-padded epoch pattern.
func parseEpoch(path string) (int, bool) {
	prefix := indexPrefix + "/"
	suffix := ".age"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return 0, false
	}
	digits := path[len(prefix) : len(path)-len(suffix)]
	if len(digits) != 6 {
		return 0, false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	epoch, err := strconv.Atoi(digits)
	if err != nil {
		return 0, false
	}
	return epoch, true
}

// Has reports whether a plaintext hash is known, including pending entries.
func (ix *Index) Has(hash string) bool {
	_, ok := ix.Get(hash)
	return ok
}

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

// Get looks up a plaintext hash, preferring a pending entry. It returns the
// zero entry and false when the hash is unknown.
func (ix *Index) Get(hash string) (Entry, bool) {
	if e, ok := ix.pending[hash]; ok {
		return e, true
	}
	e, ok := ix.all[hash]
	return e, ok
}

// Add records an entry by its plaintext hash and queues it for the next Save,
// replacing any entry with the same hash.
func (ix *Index) Add(entry Entry) {
	ix.pending[entry.PlainSHA256] = entry
	ix.all[entry.PlainSHA256] = entry
}

// Compact writes a new epoch containing known entries marked true in liveHashes
// and returns the number written, or zero without writing if none match.
// It leaves ix unchanged and returns encryption or upload errors from Save.
// Callers delete old epochs separately.
func (ix *Index) Compact(backend storage.Backend, recipients []string, liveHashes map[string]bool) (int, error) {
	// Reuse Save by replacing all and pending with the live subset, then
	// force a save even if pending is empty (when there is something to write).
	next := New()
	next.epoch = ix.epoch
	count := 0
	for h, e := range ix.all {
		if liveHashes[h] {
			next.all[h] = e
			next.pending[h] = e
			count++
		}
	}
	if count == 0 {
		return 0, nil
	}
	return next.Save(backend, recipients)
}

// Save writes only the pending entries as the next encrypted epoch and returns
// the number written. It is a no-op when nothing is pending. Pending entries
// are cleared and the epoch counter advances only after a successful upload;
// serialization, encryption, and upload errors leave them unchanged.
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
