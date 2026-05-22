package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pterm/pterm"

	"gloak/internal/crypto"
)

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
