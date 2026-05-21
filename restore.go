package main

import (
	"path/filepath"

	"github.com/pterm/pterm"

	"securebackup/internal/flow"
)

type RestoreCmd struct {
	UUID      string `arg:"" name:"uuid" help:"Backup UUID to restore."`
	Identity  string `help:"Identity private key file (defaults to ~/.securebackup/identity.txt)." short:"i"`
	Remote    string `help:"Source Rclone URI (e.g., keraaaki:backup_folder or /mnt/backups)." required:""`
	OutputDir string `help:"Output directory for restored file." default:"./restored" short:"o"`
}

func (r *RestoreCmd) Run() error {
	pterm.Info.Println("Starting RESTORE process...")

	ident := r.Identity
	if ident == "" {
		ident = filepath.Join(getDefaultDir(), "identity.txt")
		pterm.Info.Printf("Using identity from: %s\n", ident)
	}

	return flow.RunRestore(r.UUID, ident, r.Remote, r.OutputDir)
}
