package storage

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

type RcloneBackend struct {
	Remote string
	// ConfigPath is optional. When empty, rclone uses its own default config.
	ConfigPath string
}

type Backend interface {
	Upload(remotePath string, in io.Reader) error
	Download(remotePath string, out io.Writer) error
	ListFiles(prefix []string) ([]byte, error)
	ListRecursive(prefix []string) ([]string, error)
	Delete(remotePath string) error
}

func NewRcloneBackend(remote string) *RcloneBackend {
	return &RcloneBackend{Remote: remote}
}

func rcloneArgs(configPath string, base ...string) []string {
	args := append([]string{}, base...)
	if configPath != "" {
		args = append(args, "--config", configPath)
	}
	return args
}

// IsMissingPath reports whether err was caused by rclone exiting with
// status 3, which rclone uses when the remote path does not exist.
// Callers that treat a missing directory as an empty listing must check
// this so a genuine listing failure is never swallowed.
func IsMissingPath(err error) bool {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode() == 3
	}
	return false
}

func (r *RcloneBackend) Upload(remotePath string, in io.Reader) error {
	fullDest := fmt.Sprintf("%s/%s", r.Remote, remotePath)

	cmd := exec.Command("rclone", rcloneArgs(r.ConfigPath, "rcat", fullDest)...)

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

	cmd := exec.Command("rclone", rcloneArgs(r.ConfigPath, "cat", fullSrc)...)

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

	cmd := exec.Command("rclone", rcloneArgs(r.ConfigPath, "lsjson", remotePath)...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to list %s via rclone lsjson: %w\nRclone Error: %s", remotePath, err, stderr.String())
	}

	return stdout.Bytes(), nil
}

// ListRecursive lists every file under prefix, recursively. Paths are
// returned in the same remote-relative shape as Upload keys.
func (r *RcloneBackend) ListRecursive(prefix []string) ([]string, error) {
	remotePath := r.Remote
	if len(prefix) > 0 {
		remotePath = fmt.Sprintf("%s/%s", r.Remote, strings.Join(prefix, "/"))
	}

	cmd := exec.Command("rclone", rcloneArgs(r.ConfigPath, "lsjson", "-R", remotePath)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to list %s via rclone lsjson: %w\nRclone Error: %s", remotePath, err, stderr.String())
	}

	entries, err := ParseList(stdout.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to parse recursive listing: %w", err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir {
			continue
		}
		if len(prefix) > 0 {
			out = append(out, strings.Join(append(append([]string{}, prefix...), e.Path), "/"))
		} else {
			out = append(out, e.Path)
		}
	}
	return out, nil
}

// Delete removes a single file at the given remote path using rclone
// deletefile. Purge is for directories; deletefile is for files.
func (r *RcloneBackend) Delete(remotePath string) error {
	fullPath := fmt.Sprintf("%s/%s", r.Remote, remotePath)

	cmd := exec.Command("rclone", rcloneArgs(r.ConfigPath, "deletefile", fullPath)...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to delete %s via rclone deletefile: %w\nRclone Error: %s", fullPath, err, stderr.String())
	}

	return nil
}
