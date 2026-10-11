package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alecthomas/kong"

	"gloak/internal/flow"
)

// BackupCmd backs up a file into a gloak repository.
type BackupCmd struct {
	ExistingFile string `arg:"" name:"existingfile" type:"existingfile" help:"File to back up"`
	Identity     string `short:"i" default:"~/.gloak/identity.txt" help:"Path to identity file"`
	Remote       string `short:"R" required:"" help:"Repository remote, e.g. b2:my-bucket/gloak"`
}

func (c *BackupCmd) Run(ctx *kong.Context) error {
	identityPath, err := expandHome(c.Identity)
	if err != nil {
		return err
	}
	if _, err := flow.RunBackup(c.ExistingFile, identityPath, c.Remote); err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}
	return nil
}

// expandHome resolves a leading ~ in path to the user's home directory.
func expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve home directory: %w", err)
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, path[2:]), nil
}
