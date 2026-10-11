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
