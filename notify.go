package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/pterm/pterm"

	"gloak/internal/config"
	"gloak/internal/notify"
)

// resolveTelegram merges Telegram credentials from the config file with
// environment overrides. The file at <gloak-dir>/config.toml provides the
// base values; GLOAK_TELEGRAM_TOKEN and GLOAK_TELEGRAM_CHAT_ID win when set.
// A malformed config file only warns; notify stays best-effort.
func resolveTelegram() notify.TelegramConfig {
	fileCfg, err := config.Load(config.Path(getDefaultDir()))
	if err != nil {
		pterm.Warning.Printf("ignoring config file: %v\n", err)
	}
	cfg := notify.TelegramConfig{
		Token:  fileCfg.Telegram.Token,
		ChatID: fileCfg.Telegram.ChatID,
	}
	if v := os.Getenv("GLOAK_TELEGRAM_TOKEN"); v != "" {
		cfg.Token = v
	}
	if v := os.Getenv("GLOAK_TELEGRAM_CHAT_ID"); v != "" {
		cfg.ChatID = v
	}
	return cfg
}

// notifyCommand sends a short best-effort report after a successful command.
// details are optional lines rendered as-is under the header. Missing
// credentials or network failures only print a warning; they never fail the
// host command. The token itself is never printed.
func notifyCommand(command string, details ...string) {
	cfg := resolveTelegram()
	if !cfg.Enabled() {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "gloak %s completed", command)
	for _, d := range details {
		if d == "" {
			continue
		}
		b.WriteByte('\n')
		b.WriteString(d)
	}
	if err := cfg.Send(b.String()); err != nil {
		pterm.Warning.Printf("telegram notify failed: %v\n", err)
	}
}

// formatDuration renders d in a short human form.
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return d.Round(time.Millisecond).String()
	}
	return d.Round(time.Second).String()
}
