package flow

import (
	"bytes"
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

	master, cfg, err := runInit([]string{pub}, backend, 4096, false)
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
	if _, _, err := runInit(nil, memtest.NewMemoryBackend(), 4096, false); err == nil {
		t.Fatal("runInit() succeeded with no recipients")
	} else if !strings.Contains(err.Error(), "recipient") {
		t.Fatalf("runInit() error = %v, want recipient mention", err)
	}
}

func TestRunInitRejectsInvalidRecipient(t *testing.T) {
	if _, _, err := runInit([]string{"age1bad"}, memtest.NewMemoryBackend(), 4096, false); err == nil {
		t.Fatal("runInit() succeeded with invalid recipient")
	}
}

func TestRunInitRejectsExistingRepository(t *testing.T) {
	pub, _, _, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error: %v", err)
	}
	backend := memtest.NewMemoryBackend()

	if _, _, err := runInit([]string{pub}, backend, 4096, false); err != nil {
		t.Fatalf("runInit() error: %v", err)
	}
	if _, _, err := runInit([]string{pub}, backend, 4096, false); err == nil {
		t.Fatal("runInit() succeeded on existing repository without force")
	} else if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("runInit() error = %v, want already exists mention", err)
	}
}

func TestRunInitForceOverwrites(t *testing.T) {
	pub, _, _, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error: %v", err)
	}
	backend := memtest.NewMemoryBackend()

	first, _, err := runInit([]string{pub}, backend, 4096, false)
	if err != nil {
		t.Fatalf("runInit() error: %v", err)
	}
	second, _, err := runInit([]string{pub}, backend, 4096, true)
	if err != nil {
		t.Fatalf("runInit(force) error: %v", err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("runInit(force) kept the old master key")
	}
	if !backend.Has(repo.ConfigPath) {
		t.Fatal("repo.json missing after force init")
	}
}
