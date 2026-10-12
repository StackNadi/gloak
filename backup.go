package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alecthomas/kong"

	"gloak/internal/flow"
)

// BackupCmd backs up a file or directory into a gloak repository.
type BackupCmd struct {
	ExistingFile string `arg:"" name:"path" help:"File or directory to back up"`
	Identity     string `short:"i" help:"Path to identity file (defaults to $GLOAK_DIR/identity.txt, or ~/.gloak/identity.txt)."`
	Remote       string `short:"R" required:"" help:"Repository remote, e.g. b2:my-bucket/gloak"`
}

func (c *BackupCmd) Run(ctx *kong.Context) error {
	identityPath := c.Identity
	if identityPath == "" {
		identityPath = filepath.Join(getDefaultDir(), "identity.txt")
	}
	identityPath, err := expandHome(identityPath)
	if err != nil {
		return err
	}
	start := time.Now()
	snapshotID, err := flow.RunBackup(c.ExistingFile, identityPath, c.Remote)
	if err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}
	notifyCommand("backup",
		"Path: "+c.ExistingFile,
		"Snapshot: "+snapshotID,
		"Duration: "+formatDuration(time.Since(start)),
	)
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
