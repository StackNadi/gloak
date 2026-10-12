package main

import (
	"path/filepath"
	"time"

	"github.com/pterm/pterm"

	"gloak/internal/flow"
)

type VerifyCmd struct {
	UUID     string `arg:"" name:"uuid" help:"Backup UUID to verify."`
	Identity string `help:"Identity private key file (defaults to ~/.gloak/identity.txt)." short:"i"`
	Remote   string `help:"Source Rclone URI (e.g., keraaaki:backup_folder or /mnt/backups)." required:"" short:"R"`
}

// Run verifies the backup using the selected or default identity and sends an
// optional completion notification. Access, decryption, and integrity errors
// are returned.
func (v *VerifyCmd) Run() error {
	pterm.Info.Println("Starting VERIFY process...")

	ident := v.Identity
	if ident == "" {
		ident = filepath.Join(getDefaultDir(), "identity.txt")
		pterm.Info.Printf("Using identity from: %s\n", ident)
	}

	start := time.Now()
	if err := flow.RunVerify(v.UUID, ident, v.Remote); err != nil {
		return err
	}
	notifyCommand("verify",
		"Snapshot: "+v.UUID,
		"Duration: "+formatDuration(time.Since(start)),
	)
	return nil
}
