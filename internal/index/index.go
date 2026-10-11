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
