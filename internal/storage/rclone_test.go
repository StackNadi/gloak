package storage

import (
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
	got := rcloneArgs("/etc/rclone.conf", "rcat", "remote:file")
	want := []string{"rcat", "remote:file", "--config", "/etc/rclone.conf"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rcloneArgs() = %v, want %v", got, want)
	}
}
