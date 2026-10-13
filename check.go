package main

import (
	"path/filepath"
	"time"

	"github.com/pterm/pterm"

	"gloak/internal/flow"
)

type CheckCmd struct {
	Identity string `help:"Identity private key file (defaults to ~/.gloak/identity.txt)." short:"i"`
	Remote   string `help:"Source Rclone URI (e.g., keraaaki:backup_folder or /mnt/backups)." required:"" short:"R"`
}

// Run checks repository structure using the selected or default identity and
// sends an optional completion notification. Check errors are returned.
func (c *CheckCmd) Run() error {
	pterm.Info.Println("Starting CHECK process...")

	ident := c.Identity
	if ident == "" {
		ident = filepath.Join(getDefaultDir(), "identity.txt")
		pterm.Info.Printf("Using identity from: %s\n", ident)
	}

	start := time.Now()
	if err := flow.RunCheck(ident, c.Remote); err != nil {
		return err
	}
	notifyCommand("check",
		"Remote: "+c.Remote,
		"Duration: "+formatDuration(time.Since(start)),
	)
	return nil
}
