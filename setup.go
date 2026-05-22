package main

import (
	"archive/zip"
	"bufio"
	"crypto/sha256"
	"encoding/hex"
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
	Dir    string `help:"Custom install directory (default: ~/.gloak/bin)." short:"D"`
	Update bool   `help:"Force re-download even if rclone already exists." short:"u"`
}

func (s *SetupCmd) Run() error {
	rcloneDir := s.Dir
	if rcloneDir == "" {
		rcloneDir = filepath.Join(getDefaultDir(), "bin")
	}
	rclonePath := filepath.Join(rcloneDir, "rclone")

	// 1. System-wide check: is rclone already on PATH?
	if !s.Update {
		if sysPath, err := exec.LookPath("rclone"); err == nil {
			pterm.Success.Printf("rclone already installed on system: %s\n", sysPath)
			pterm.DefaultBasicText.Printf("  Use --update to install a local copy anyway.\n")
			return nil
		}
	}

	// 2. Local check: already in gloak bin dir?
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

	// Progress bar using Content-Length
	totalSize := resp.ContentLength

	pb, _ := pterm.DefaultProgressbar.
		WithTotal(int(totalSize)).
		WithTitle("Downloading").
		WithRemoveWhenDone(false).
		Start()

	progressReader := &progressBarReader{r: resp.Body, pb: pb}

	// Write to temp file first (zip needs random access)
	tmpFile, err := os.CreateTemp("", "rclone-*.zip")
	if err != nil {
		pb.Stop()
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmpFile, progressReader); err != nil {
		pb.Stop()
		tmpFile.Close()
		return fmt.Errorf("failed to save download: %w", err)
	}
	tmpFile.Close()
	pb.Stop()

	pterm.Info.Println("Verifying rclone archive checksum...")
	if err := verifyRcloneArchiveChecksum(tmpPath, archiveName); err != nil {
		return err
	}

	// Open zip and extract rclone binary
	zipFile, err := zip.OpenReader(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to open zip: %w", err)
	}
	defer zipFile.Close()

	// Look for the rclone binary
	binName := "rclone"
	if osName == "windows" {
		binName = "rclone.exe"
	}

	found := false
	for _, f := range zipFile.File {
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

func verifyRcloneArchiveChecksum(archivePath string, archiveName string) error {
	expected, err := fetchRcloneArchiveChecksum(archiveName)
	if err != nil {
		return err
	}

	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open downloaded archive for checksum verification: %w", err)
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return fmt.Errorf("failed to hash downloaded archive: %w", err)
	}

	actual := hex.EncodeToString(hasher.Sum(nil))
	if actual != expected {
		return fmt.Errorf("checksum verification failed for %s: expected %s, got %s", archiveName, expected, actual)
	}

	return nil
}

func fetchRcloneArchiveChecksum(archiveName string) (string, error) {
	checksumsURL := "https://downloads.rclone.org/SHA256SUMS"
	resp, err := http.Get(checksumsURL)
	if err != nil {
		return "", fmt.Errorf("failed to download rclone checksums: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksum download failed: HTTP %d for %s", resp.StatusCode, checksumsURL)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}

		hash := fields[0]
		filename := strings.TrimPrefix(fields[1], "*")
		if filepath.Base(filename) == archiveName {
			if len(hash) != sha256.Size*2 {
				return "", fmt.Errorf("invalid SHA256 checksum length for %s", archiveName)
			}
			if _, err := hex.DecodeString(hash); err != nil {
				return "", fmt.Errorf("invalid SHA256 checksum for %s: %w", archiveName, err)
			}
			return strings.ToLower(hash), nil
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to read rclone checksums: %w", err)
	}

	return "", fmt.Errorf("checksum for %s not found in SHA256SUMS", archiveName)
}

// progressBarReader wraps an io.Reader and updates a pterm progress bar.
type progressBarReader struct {
	r  io.Reader
	pb *pterm.ProgressbarPrinter
}

func (pr *progressBarReader) Read(p []byte) (int, error) {
	n, err := pr.r.Read(p)
	if n > 0 {
		pr.pb.Add(n)
	}
	return n, err
}
