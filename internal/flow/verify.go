package flow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/pterm/pterm"

	"gloak/internal/core"
	"gloak/internal/crypto"
	"gloak/internal/repo"
	"gloak/internal/storage"
)

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
	if isFileMode(backend, snapshotID) {
		return runFileVerify(snapshotID, identityPath, backend)
	}
	r, err := repo.Open(backend, identityPath)
	if err != nil {
		return err
	}
	kind, fileM, treeM, err := readAnyManifest(backend, identityPath, snapshotID)
	if err != nil {
		return err
	}
	if kind == core.KindTree {
		return runTreeVerify(treeM, r, backend)
	}
	return runFileVerifyFromManifest(fileM, r, backend)
}

func runFileVerifyFromManifest(manifest *core.Manifest, r *repo.Repo, backend storage.Backend) error {
	payloadHasher := sha256.New()
	for _, meta := range manifest.Chunks {
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
		payloadHasher.Write(plain)
	}

	if hex.EncodeToString(payloadHasher.Sum(nil)) != manifest.PayloadSHA256 {
		return fmt.Errorf("FATAL: overall payload hash differs from manifest")
	}
	return nil
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
