package main

import (
	"github.com/pterm/pterm"

	"gloak/internal/notify"
)

// notifyTelegram sends a short best-effort report after a command. Missing
// credentials or network failures only print a warning; they never fail the
// host command. The token itself is never printed.
func notifyTelegram(message string) {
	cfg := notify.FromEnv()
	if !cfg.Enabled() {
		return
	}
	if err := cfg.Send(message); err != nil {
		pterm.Warning.Printf("telegram notify failed: %v\n", err)
	}
}
