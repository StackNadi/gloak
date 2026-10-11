package main

import (
	"path/filepath"

	"github.com/pterm/pterm"

	"gloak/internal/flow"
)

type CheckCmd struct {
	Identity string `help:"Identity private key file (defaults to ~/.gloak/identity.txt)." short:"i"`
	Remote   string `help:"Source Rclone URI (e.g., keraaaki:backup_folder or /mnt/backups)." required:"" short:"R"`
}

func (c *CheckCmd) Run() error {
	pterm.Info.Println("Starting CHECK process...")

	ident := c.Identity
	if ident == "" {
		ident = filepath.Join(getDefaultDir(), "identity.txt")
		pterm.Info.Printf("Using identity from: %s\n", ident)
	}

	if err := flow.RunCheck(ident, c.Remote); err != nil {
		return err
	}
	notifyTelegram("gloak check completed")
	return nil
}
