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
	Upload  UploadCmd  `cmd:"" help:"Encrypt, chunk, and upload a file to a storage destination."`
	Restore RestoreCmd `cmd:"" help:"Restore and decrypt a backup from storage."`
	Verify  VerifyCmd  `cmd:"" help:"Verify backup chunks and checksums."`
	Cleanup CleanupCmd `cmd:"" help:"Remove orphan backup directories missing manifest.age."`
	Setup   SetupCmd   `cmd:"" help:"Download and install rclone to ~/.securebackup/bin/."`
}

func getDefaultDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".securebackup")
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
		kong.Name("securebackup"),
		kong.Description("SecureBackup: age-encrypted backup tool"),
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
