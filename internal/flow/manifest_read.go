package flow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"gloak/internal/core"
	"gloak/internal/crypto"
	"gloak/internal/repo"
	"gloak/internal/storage"
)

// readManifest downloads the snapshot for snapshotID and decrypts it with the
// identity that opened the repository.
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

// validateChunkPath rejects manifest chunk paths that fall outside data/ or
// contain traversal segments.
func validateChunkPath(path string) error {
	if !strings.HasPrefix(path, repo.DataPrefix+"/") || strings.Contains(path, "..") {
		return fmt.Errorf("chunk path escapes data prefix: %s", path)
	}
	return nil
}
