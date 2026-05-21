package main

import (
	"path/filepath"

	"github.com/pterm/pterm"

	"securebackup/internal/flow"
)

type VerifyCmd struct {
	UUID     string `arg:"" name:"uuid" help:"Backup UUID to verify."`
	Identity string `help:"Identity private key file (defaults to ~/.securebackup/identity.txt)." short:"i"`
	Remote   string `help:"Source Rclone URI (e.g., keraaaki:backup_folder or /mnt/backups)." required:""`
}

func (v *VerifyCmd) Run() error {
	pterm.Info.Println("Starting VERIFY process...")

	ident := v.Identity
	if ident == "" {
		ident = filepath.Join(getDefaultDir(), "identity.txt")
		pterm.Info.Printf("Using identity from: %s\n", ident)
	}

	return flow.RunVerify(v.UUID, ident, v.Remote)
}
