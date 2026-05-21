package main

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/pterm/pterm"
)

type SetupCmd struct {
	Dir    string `help:"Custom install directory (default: ~/.securebackup/bin)." short:"D"`
	Update bool   `help:"Force re-download even if rclone already exists." short:"u"`
}

func (s *SetupCmd) Run() error {
	rcloneDir := s.Dir
	if rcloneDir == "" {
		rcloneDir = filepath.Join(getDefaultDir(), "bin")
	}
	rclonePath := filepath.Join(rcloneDir, "rclone")

	// Check if already installed
	if !s.Update {
		if _, err := os.Stat(rclonePath); err == nil {
			pterm.Success.Printf("rclone already installed at %s\n", rclonePath)
			pterm.DefaultBasicText.Printf("  Use --update to re-download.\n")
			return nil
		}
	}

	if err := os.MkdirAll(rcloneDir, 0700); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", rcloneDir, err)
	}

	osName := runtime.GOOS
	archName := runtime.GOARCH

	// Map Go arch names to rclone download conventions
	switch archName {
	case "amd64", "arm64":
		// supported as-is
	case "arm":
		archName = "arm-v7"
	default:
		return fmt.Errorf("unsupported architecture: %s", archName)
	}

	if osName != "linux" && osName != "darwin" {
		return fmt.Errorf("setup supports linux and darwin only; on %s install rclone manually: https://rclone.org/install/", osName)
	}

	// Rclone uses static download URLs, no version API
	archiveName := fmt.Sprintf("rclone-current-%s-%s.zip", osName, archName)
	downloadURL := fmt.Sprintf("https://downloads.rclone.org/%s", archiveName)

	pterm.Info.Printf("Downloading rclone (%s/%s)...\n", osName, archName)

	resp, err := http.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("failed to download rclone: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d for %s", resp.StatusCode, downloadURL)
	}

	// Write to temp file first (zip needs random access)
	tmpFile, err := os.CreateTemp("", "rclone-*.zip")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	pterm.Info.Println("Saving archive...")
	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to save download: %w", err)
	}
	tmpFile.Close()

	// Open zip and extract rclone binary
	zip, err := zip.OpenReader(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to open zip: %w", err)
	}
	defer zip.Close()

	// Look for the rclone binary (no .exe on linux/mac)
	binName := "rclone"
	if osName == "windows" {
		binName = "rclone.exe"
	}

	found := false
	for _, f := range zip.File {
		if filepath.Base(f.Name) == binName && !f.FileInfo().IsDir() {
			rc, err := f.Open()
			if err != nil {
				return fmt.Errorf("failed to open %s in zip: %w", f.Name, err)
			}

			outFile, err := os.OpenFile(rclonePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
			if err != nil {
				rc.Close()
				return fmt.Errorf("failed to create %s: %w", rclonePath, err)
			}

			if _, err := io.Copy(outFile, rc); err != nil {
				rc.Close()
				outFile.Close()
				return fmt.Errorf("failed to write rclone binary: %w", err)
			}
			rc.Close()
			outFile.Close()
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("rclone binary not found in zip archive")
	}

	// Verify it runs
	verify := exec.Command(rclonePath, "version")
	verifyOut, err := verify.Output()
	if err != nil {
		return fmt.Errorf("rclone downloaded but failed to run: %w", err)
	}

	versionLine := strings.SplitN(string(verifyOut), "\n", 2)[0]
	pterm.Success.Printf("Installed: %s\n", versionLine)
	pterm.Success.Printf("Location:  %s\n", rclonePath)
	pterm.DefaultBasicText.Printf("\nAdd to PATH:\n  export PATH=\"%s:$PATH\"\n", rcloneDir)

	return nil
}
