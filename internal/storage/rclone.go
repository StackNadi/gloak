package storage

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

type RcloneBackend struct {
	Remote     string
	ConfigPath string
}

func NewRcloneBackend(remote string) *RcloneBackend {
	return &RcloneBackend{
		Remote:     remote,
		ConfigPath: "/home/keraki/.config/rclone/rclone.conf",
	}
}

func (r *RcloneBackend) Upload(remotePath string, in io.Reader) error {
	fullDest := fmt.Sprintf("%s/%s", r.Remote, remotePath)

	cmd := exec.Command("rclone", "rcat", "--config", r.ConfigPath, fullDest)

	cmd.Stdin = in

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to upload to %s via rclone rcat: %w\nRclone Error: %s", fullDest, err, stderr.String())
	}

	return nil
}

func (r *RcloneBackend) Download(remotePath string, out io.Writer) error {
	fullSrc := fmt.Sprintf("%s/%s", r.Remote, remotePath)

	cmd := exec.Command("rclone", "cat", "--config", r.ConfigPath, fullSrc)

	cmd.Stdout = out

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to download from %s via rclone cat: %w\nRclone Error: %s", fullSrc, err, stderr.String())
	}

	return nil
}

// ListFiles lists files at the given remote path prefix using rclone lsjson.
// Each prefix segment is joined with "/" to form the remote directory path.
// Returns the raw JSON output from rclone lsjson.
func (r *RcloneBackend) ListFiles(prefix []string) ([]byte, error) {
	remotePath := r.Remote
	if len(prefix) > 0 {
		remotePath = fmt.Sprintf("%s/%s", r.Remote, strings.Join(prefix, "/"))
	}

	cmd := exec.Command("rclone", "lsjson", "--config", r.ConfigPath, remotePath)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to list %s via rclone lsjson: %w\nRclone Error: %s", remotePath, err, stderr.String())
	}

	return stdout.Bytes(), nil
}

// Delete removes a file or directory at the given remote path using rclone purge.
func (r *RcloneBackend) Delete(remotePath string) error {
	fullPath := fmt.Sprintf("%s/%s", r.Remote, remotePath)

	cmd := exec.Command("rclone", "purge", "--config", r.ConfigPath, fullPath)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to delete %s via rclone purge: %w\nRclone Error: %s", fullPath, err, stderr.String())
	}

	return nil
}
