package main

import (
	"path/filepath"

	"github.com/pterm/pterm"

	"gloak/internal/flow"
)

type SnapshotsCmd struct {
	Identity string `help:"Identity private key file (defaults to ~/.gloak/identity.txt)." short:"i"`
	Remote   string `help:"Source Rclone URI (e.g., keraaaki:backup_folder or /mnt/backups)." required:"" short:"R"`
}

func (c *SnapshotsCmd) Run() error {
	pterm.Info.Println("Listing snapshots...")
	ident := c.Identity
	if ident == "" {
		ident = filepath.Join(getDefaultDir(), "identity.txt")
	}
	return flow.RunSnapshots(ident, c.Remote)
}
