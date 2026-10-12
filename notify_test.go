package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, content string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GLOAK_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestResolveTelegramFromFile(t *testing.T) {
	writeConfig(t, "[telegram]\ntoken = \"file-token\"\nchat_id = \"file-chat\"\n")
	t.Setenv("GLOAK_TELEGRAM_TOKEN", "")
	t.Setenv("GLOAK_TELEGRAM_CHAT_ID", "")

	cfg := resolveTelegram()
	if cfg.Token != "file-token" || cfg.ChatID != "file-chat" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestResolveTelegramEnvOverridesFile(t *testing.T) {
	writeConfig(t, "[telegram]\ntoken = \"file-token\"\nchat_id = \"file-chat\"\n")
	t.Setenv("GLOAK_TELEGRAM_TOKEN", "env-token")
	t.Setenv("GLOAK_TELEGRAM_CHAT_ID", "env-chat")

	cfg := resolveTelegram()
	if cfg.Token != "env-token" || cfg.ChatID != "env-chat" {
		t.Fatalf("cfg = %+v, want env values", cfg)
	}
}

func TestResolveTelegramEmptyWithoutConfigOrEnv(t *testing.T) {
	t.Setenv("GLOAK_DIR", t.TempDir())
	t.Setenv("GLOAK_TELEGRAM_TOKEN", "")
	t.Setenv("GLOAK_TELEGRAM_CHAT_ID", "")

	cfg := resolveTelegram()
	if cfg.Enabled() {
		t.Fatalf("cfg = %+v, want disabled", cfg)
	}
}
