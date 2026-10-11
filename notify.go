package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/pterm/pterm"

	"gloak/internal/notify"
)

// notifyCommand sends a short best-effort report after a successful command.
// details are optional lines rendered as-is under the header. Missing
// credentials or network failures only print a warning; they never fail the
// host command. The token itself is never printed.
func notifyCommand(command string, details ...string) {
	cfg := notify.FromEnv()
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
