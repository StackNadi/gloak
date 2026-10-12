package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileIsEmpty(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Telegram.Token != "" || cfg.Telegram.ChatID != "" {
		t.Fatalf("cfg = %+v, want zero value", cfg)
	}
}

func TestLoadReadsTelegramSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := "# comment\n[telegram]\ntoken = \"abc\"\nchat_id = \"42\"\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Telegram.Token != "abc" || cfg.Telegram.ChatID != "42" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestLoadMalformedFileErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[telegram\ntoken="), 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("Load() succeeded on malformed file")
	}
}

func TestPathUsesDir(t *testing.T) {
	if got := Path("/x/gloak"); got != "/x/gloak/config.toml" {
		t.Fatalf("Path() = %q", got)
	}
}
