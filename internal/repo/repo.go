package repo

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"time"

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

// DataPath returns the content-addressed object path for a plaintext chunk
// hash. Inputs shorter than 2 bytes cannot be sharded, so it returns
// data/invalid instead of panicking.
func DataPath(plainSHA256Hex string) string {
	if len(plainSHA256Hex) < 2 {
		return DataPrefix + "/invalid"
	}
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
// recipient first, repo.json last as the completion marker. It refuses to
// touch a repository that already exists unless force is set, because a
// fresh master key would orphan every snapshot sealed with the old one.
func Init(backend storage.Backend, recipients []string, min, avg, max int64, force bool) ([]byte, Config, error) {
	if len(recipients) == 0 {
		return nil, Config{}, fmt.Errorf("at least one recipient is required")
	}
	for _, r := range recipients {
		if _, err := crypto.ParseRecipient(r); err != nil {
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

	for _, recipient := range recipients {
		wrapped, err := crypto.EncryptMulti([]string{recipient}, master)
		if err != nil {
			return nil, Config{}, fmt.Errorf("failed to wrap master key: %w", err)
		}
		if err := backend.Upload(KeysPathFor(recipient), bytes.NewReader(wrapped)); err != nil {
			return nil, Config{}, fmt.Errorf("failed to upload wrapped key: %w", err)
		}
	}

	// Force re-init (or a retry after a crashed init) can leave wrapped
	// keys for recipients that are no longer part of the repository. Remove
	// them before repo.json, the completion marker, is replaced, so a
	// removed recipient cannot keep opening the repository.
	if err := deleteStaleKeys(backend, recipients); err != nil {
		return nil, Config{}, err
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

// deleteStaleKeys removes wrapped keys under keys/ whose fingerprint is
// not part of recipients. A missing keys/ directory is normal on first
// init; any other listing failure is returned.
func deleteStaleKeys(backend storage.Backend, recipients []string) error {
	keep := make(map[string]bool, len(recipients))
	for _, recipient := range recipients {
		keep[KeysPathFor(recipient)] = true
	}
	raw, err := backend.ListFiles([]string{KeysPrefix})
	if err != nil {
		if storage.IsMissingPath(err) {
			return nil
		}
		return fmt.Errorf("failed to list keys: %w", err)
	}
	entries, err := storage.ParseList(raw)
	if err != nil {
		return fmt.Errorf("failed to parse keys listing: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir {
			continue
		}
		full := KeysPrefix + "/" + entry.Path
		if keep[full] {
			continue
		}
		if err := backend.Delete(full); err != nil {
			return fmt.Errorf("failed to delete stale key %s: %w", full, err)
		}
	}
	return nil
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
	if cfg.ChunkMin <= 0 || cfg.ChunkAvg <= 0 || cfg.ChunkMax <= 0 ||
		cfg.ChunkMax < cfg.ChunkAvg || cfg.ChunkAvg < cfg.ChunkMin {
		return nil, fmt.Errorf("invalid chunk parameters in %s (min=%d avg=%d max=%d)", ConfigPath, cfg.ChunkMin, cfg.ChunkAvg, cfg.ChunkMax)
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
