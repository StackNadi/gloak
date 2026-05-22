package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/pterm/pterm"

	"gloak/internal/flow"
)

type UploadCmd struct {
	File      string `arg:"" help:"File to back up." type:"existingfile"`
	Recipient string `help:"Recipient public key (age1...) or path to recipient.txt." short:"r"`
	Remote    string `help:"Destination Rclone URI (e.g., keraaaki:backup_folder or /mnt/backups)." required:""`
}

func (u *UploadCmd) Run() error {
	pterm.Info.Println("Starting UPLOAD process...")

	recip := u.Recipient
	if recip == "" {
		defaultRecipPath := filepath.Join(getDefaultDir(), "recipient.txt")
		val, err := readKeyFile(defaultRecipPath, "age1")
		if err != nil {
			return fmt.Errorf("recipient not provided and %s could not be read: %w", defaultRecipPath, err)
		}
		recip = val
		pterm.Info.Printf("Using recipient from: %s\n", defaultRecipPath)
	} else if !strings.HasPrefix(recip, "age1") {
		val, err := readKeyFile(recip, "age1")
		if err != nil {
			return fmt.Errorf("failed to read recipient file %s: %w", recip, err)
		}
		recip = val
	}

	pterm.DefaultBasicText.Printf("Target File      : %s\n", u.File)
	pterm.DefaultBasicText.Printf("Destination      : %s\n", u.Remote)

	return flow.RunUpload(u.File, recip, u.Remote)
}
