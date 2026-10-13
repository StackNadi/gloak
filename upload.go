package main

import (
	"fmt"

	"github.com/pterm/pterm"

	"gloak/internal/flow"
	"gloak/internal/storage"
)

// UploadCmd writes a self-contained encrypted backup folder to a remote root.
type UploadCmd struct {
	File      string `arg:"" name:"file" type:"existingfile" help:"File to back up"`
	Recipient string `short:"r" help:"Recipient public key file or comma-separated keys (default ~/.gloak/recipient.txt)"`
	Remote    string `short:"R" required:"" help:"Remote root, e.g. keraaaki:backups or /mnt/backups"`
}

// Run resolves recipients and uploads a standalone encrypted backup, printing
// its UUID on success. Recipient resolution and upload errors are returned.
func (u *UploadCmd) Run() error {
	recipients, err := resolveInitRecipients(u.Recipient)
	if err != nil {
		return err
	}
	pterm.Info.Println("Starting UPLOAD process...")
	backupID, err := flow.RunUpload(u.File, recipients, storage.NewRcloneBackend(u.Remote))
	if err != nil {
		return fmt.Errorf("upload failed: %w", err)
	}
	pterm.DefaultBasicText.Printf("Backup UUID: %s\n", backupID)
	return nil
}
