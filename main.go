package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/pterm/pterm"
)

var cli struct {
	Debug bool `help:"Enable debug mode." short:"d"`

	Keygen  KeygenCmd  `cmd:"" help:"Generate an X25519 identity and recipient file."`
	Init    InitCmd    `cmd:"" help:"Initialize a new gloak repository." group:"Repository"`
	Backup  BackupCmd  `cmd:"" help:"Encrypt, chunk, and upload a file to a repository."`
	Upload  UploadCmd  `cmd:"" help:"Encrypt and upload a file as a standalone folder (no repository)."`
	Restore RestoreCmd `cmd:"" help:"Restore and decrypt a backup from storage."`
	Verify  VerifyCmd  `cmd:"" help:"Verify backup chunks and checksums."`
	Snapshots SnapshotsCmd `cmd:"" help:"List snapshots in the repository."`
	Check   CheckCmd   `cmd:"" help:"Check repository consistency (structural)."`
	Setup   SetupCmd   `cmd:"" help:"Download and install rclone to ~/.gloak/bin/."`
}

func getDefaultDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".gloak")
}

func readKeyFile(path string, prefix string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(b), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return line, nil
		}
	}
	return "", fmt.Errorf("key with prefix %s not found in %s", prefix, path)
}

func main() {
	pterm.DisableColor()

	ctx := kong.Parse(&cli,
		kong.Name("gloak"),
		kong.Description("gloak: age-encrypted backup tool"),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{
			Compact: true,
		}),
	)

	if cli.Debug {
		pterm.EnableDebugMessages()
		pterm.Debug.Println("Debug mode enabled.")
	}

	err := ctx.Run()
	ctx.FatalIfErrorf(err)
}
