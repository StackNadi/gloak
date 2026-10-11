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

// readTreeManifest downloads the tree snapshot for snapshotID and decrypts
// it with the identity that opened the repository.
func readTreeManifest(backend storage.Backend, identityPath, snapshotID string) (core.TreeManifest, error) {
	path, err := repo.SnapshotPath(snapshotID)
	if err != nil {
		return core.TreeManifest{}, err
	}
	var buf bytes.Buffer
	if err := backend.Download(path, &buf); err != nil {
		return core.TreeManifest{}, fmt.Errorf("snapshot %s not found: %w", snapshotID, err)
	}
	plain, err := decryptSnapshotBytes(identityPath, buf.Bytes())
	if err != nil {
		return core.TreeManifest{}, err
	}
	var manifest core.TreeManifest
	if err := json.Unmarshal(plain, &manifest); err != nil {
		return core.TreeManifest{}, fmt.Errorf("failed to parse tree snapshot: %w", err)
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
	plain, err := decryptSnapshotBytes(identityPath, buf.Bytes())
	if err != nil {
		return "", nil, nil, err
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

func decryptSnapshotBytes(identityPath string, sealed []byte) ([]byte, error) {
	reader, err := crypto.DecryptReader(identityPath, bytes.NewReader(sealed))
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt snapshot: %w", err)
	}
	plain, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read snapshot: %w", err)
	}
	return plain, nil
}

// validateChunkPath rejects manifest chunk paths that fall outside data/ or
// contain traversal segments.
func validateChunkPath(path string) error {
	if !strings.HasPrefix(path, repo.DataPrefix+"/") || strings.Contains(path, "..") {
		return fmt.Errorf("chunk path escapes data prefix: %s", path)
	}
	return nil
}
