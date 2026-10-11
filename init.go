package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gloak/internal/flow"
)

// InitCmd creates a new gloak repository.
type InitCmd struct {
	Remote    string `short:"R" required:"" help:"Repository remote, e.g. b2:my-bucket/gloak or local path"`
	Recipient string `short:"r" help:"Recipient public key file or comma-separated keys (default ~/.gloak/recipient.txt)"`
}

func (c *InitCmd) Run() error {
	recipients, err := resolveInitRecipients(c.Recipient)
	if err != nil {
		return err
	}
	return flow.RunInit(recipients, c.Remote)
}

// resolveInitRecipients turns the --recipient value into a key list. An empty
// path falls back to ~/.gloak/recipient.txt. A value that looks like keys
// (starts with age1 or contains commas) is split on commas; anything else is
// treated as a file path with one key per line, skipping blanks and # comments.
func resolveInitRecipients(path string) ([]string, error) {
	if path == "" {
		path = filepath.Join(getDefaultDir(), "recipient.txt")
	}
	if !strings.HasPrefix(path, "age1") && !strings.Contains(path, ",") {
		return readRecipientFile(path)
	}
	var out []string
	for _, part := range strings.Split(path, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no recipients found (run 'gloak keygen' first)")
	}
	return out, nil
}

func readRecipientFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read recipients from %s: %w (run 'gloak keygen' first)", path, err)
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no recipients found in %s (run 'gloak keygen' first)", path)
	}
	return out, nil
}
