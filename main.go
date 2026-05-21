package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/pterm/pterm"

	"securebackup/internal/crypto"
	"securebackup/internal/flow"
	"securebackup/internal/storage"
)

var cli struct {
	Debug bool `help:"Enable debug mode." short:"d"`

	Keygen   KeygenCmd   `cmd:"" help:"Generate an X25519 identity and recipient file."`
	Upload   UploadCmd   `cmd:"" help:"Encrypt, chunk, and upload a file to a storage destination."`
	Restore  RestoreCmd  `cmd:"" help:"Restore and decrypt a backup from storage."`
	Verify   VerifyCmd   `cmd:"" help:"Verify backup chunks and checksums."`
	Cleanup  CleanupCmd  `cmd:"" help:"Remove orphan backup directories missing manifest.age."`
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

type KeygenCmd struct {
	OutputDir string `help:"Output directory for identity.txt and recipient.txt." short:"o"`
	Force     bool   `help:"Overwrite existing identity.txt and recipient.txt." short:"f"`
}

func (k *KeygenCmd) Run() error {
	pterm.Info.Println("Generating X25519 Age keypair...")

	dir := k.OutputDir
	if dir == "" {
		dir = getDefaultDir()
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	identPath := filepath.Join(dir, "identity.txt")
	recipPath := filepath.Join(dir, "recipient.txt")

	if !k.Force {
		var existingPaths []string
		for _, path := range []string{identPath, recipPath} {
			if _, err := os.Stat(path); err == nil {
				existingPaths = append(existingPaths, path)
			} else if !os.IsNotExist(err) {
				return fmt.Errorf("failed to check %s: %w", path, err)
			}
		}
		if len(existingPaths) > 0 {
			pterm.Warning.Println("Existing keypair found. Overwriting will make backups encrypted with the old key UNRECOVERABLE. Use --force to confirm.")
			for _, path := range existingPaths {
				pterm.Warning.Printf("Existing file: %s\n", path)
			}
			return fmt.Errorf("refusing to overwrite existing keypair")
		}
	}

	pubKey, _, fileContent, err := crypto.GenerateKey()
	if err != nil {
		pterm.Error.Printf("Failed: %v\n", err)
		return err
	}

	if err := os.WriteFile(identPath, []byte(fileContent), 0600); err != nil {
		return fmt.Errorf("failed to write identity.txt: %w", err)
	}

	if err := os.WriteFile(recipPath, []byte(pubKey+"\n"), 0644); err != nil {
		return fmt.Errorf("failed to write recipient.txt: %w", err)
	}

	pterm.Success.Printf("Keypair generated successfully.\n\n")
	pterm.DefaultBasicText.Printf("Recipient file (Public): %s\n", recipPath)
	pterm.DefaultBasicText.Printf("Identity file (Private): %s\n", identPath)
	
	fmt.Println()
	pterm.Warning.Println("DO NOT SHARE YOUR IDENTITY.TXT WITH ANYONE.")

	return nil
}

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

type VerifyCmd struct {
	UUID      string `arg:"" name:"uuid" help:"Backup UUID to verify."`
	Identity  string `help:"Identity private key file (defaults to ~/.securebackup/identity.txt)." short:"i"`
	Remote    string `help:"Source Rclone URI (e.g., keraaaki:backup_folder or /mnt/backups)." required:""`
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

type CleanupCmd struct {
	Remote string `help:"Rclone remote URI (e.g., keraaaki:backup_folder)." required:""`
	Yes    bool   `help:"Skip confirmation prompt." short:"y"`
}

type lsjsonEntry struct {
	Path    string `json:"Path"`
	Name    string `json:"Name"`
	IsDir   bool   `json:"IsDir"`
	Size    int64  `json:"Size"`
	ModTime string `json:"ModTime"`
}

func (c *CleanupCmd) Run() error {
	pterm.Info.Println("Scanning remote for backup directories...")

	backend := storage.NewRcloneBackend(c.Remote)

	raw, err := backend.ListFiles(nil)
	if err != nil {
		return fmt.Errorf("failed to list remote: %w", err)
	}

	var entries []lsjsonEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return fmt.Errorf("failed to parse lsjson output: %w", err)
	}

	// Collect top-level directories (potential backup folders)
	var dirs []lsjsonEntry
	for _, e := range entries {
		if e.IsDir {
			dirs = append(dirs, e)
		}
	}

	if len(dirs) == 0 {
		pterm.Success.Println("No backup directories found. Nothing to clean up.")
		return nil
	}

	pterm.Info.Printf("Found %d backup directories. Checking for manifest.age...\n", len(dirs))

	// Identify orphan directories (missing manifest.age)
	var orphans []string
	for _, d := range dirs {
		filesInDir, err := backend.ListFiles([]string{d.Path})
		if err != nil {
			pterm.Warning.Printf("  Could not list %s: %v — skipping\n", d.Path, err)
			continue
		}

		var innerEntries []lsjsonEntry
		if err := json.Unmarshal(filesInDir, &innerEntries); err != nil {
			pterm.Warning.Printf("  Could not parse listing for %s: %v — skipping\n", d.Path, err)
			continue
		}

		hasManifest := false
		for _, ie := range innerEntries {
			if !ie.IsDir && ie.Name == "manifest.age" {
				hasManifest = true
				break
			}
		}

		if !hasManifest {
			orphans = append(orphans, d.Path)
		}
	}

	if len(orphans) == 0 {
		pterm.Success.Println("All backup directories have a manifest.age. Nothing to clean up.")
		return nil
	}

	sort.Strings(orphans)

	pterm.Warning.Printf("Found %d orphan directorie(s) missing manifest.age:\n", len(orphans))
	for _, o := range orphans {
		pterm.DefaultBasicText.Printf("  - %s\n", o)
	}

	if !c.Yes {
		fmt.Println()
		result, _ := pterm.DefaultInteractiveConfirm.
			WithDefaultText("Delete these directories?").
			Show()
		if !result {
			pterm.Info.Println("Aborted. No directories deleted.")
			return nil
		}
	}

	// Batch delete in chunks of up to 500
	const batchSize = 500
	deleted := 0
	var deleteErrors []string

	for i := 0; i < len(orphans); i += batchSize {
		end := i + batchSize
		if end > len(orphans) {
			end = len(orphans)
		}
		batch := orphans[i:end]

		for _, path := range batch {
			if err := backend.Delete(path); err != nil {
				deleteErrors = append(deleteErrors, fmt.Sprintf("%s: %v", path, err))
				continue
			}
			deleted++
			pterm.Success.Printf("  Deleted: %s\n", path)
		}
	}

	fmt.Println()
	pterm.Success.Printf("Cleanup complete: %d of %d orphan directorie(s) deleted.\n", deleted, len(orphans))

	if len(deleteErrors) > 0 {
		pterm.Warning.Printf("%d deletion(s) failed:\n", len(deleteErrors))
		for _, de := range deleteErrors {
			pterm.Error.Printf("  %s\n", de)
		}
		return fmt.Errorf("%d deletion(s) failed", len(deleteErrors))
	}

	return nil
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
