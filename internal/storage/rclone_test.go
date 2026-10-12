package storage

import (
	"fmt"
	"os/exec"
	"reflect"
	"testing"
)

func TestRcloneArgsOmitsConfigWhenEmpty(t *testing.T) {
	got := rcloneArgs("", "cat", "remote:file")
	want := []string{"cat", "remote:file"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rcloneArgs() = %v, want %v", got, want)
	}
}

func TestRcloneArgsAppendsConfigWhenSet(t *testing.T) {
	got := rcloneArgs("/etc/rclone.conf", "cat", "remote:file")
	want := []string{"cat", "remote:file", "--config", "/etc/rclone.conf"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rcloneArgs() = %v, want %v", got, want)
	}
}

func TestIsMissingPathMatchesExitCodeThree(t *testing.T) {
	// rclone exits 3 when the remote path is missing.
	exitErr := exec.Command("sh", "-c", "exit 3").Run()
	if exitErr == nil {
		t.Fatal("sh exited 0, want non-zero")
	}
	if !IsMissingPath(fmt.Errorf("wrapped: %w", exitErr)) {
		t.Fatal("IsMissingPath(wrapped exit 3) = false, want true")
	}
}

func TestIsMissingPathRejectsOtherErrors(t *testing.T) {
	other := exec.Command("sh", "-c", "exit 1").Run()
	if IsMissingPath(fmt.Errorf("wrapped: %w", other)) {
		t.Fatal("IsMissingPath(exit 1) = true, want false")
	}
	if IsMissingPath(fmt.Errorf("plain failure")) {
		t.Fatal("IsMissingPath(plain) = true, want false")
	}
	if IsMissingPath(nil) {
		t.Fatal("IsMissingPath(nil) = true, want false")
	}
}
