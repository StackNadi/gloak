package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReadKeyFileFindsMatchingKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.txt")
	content := "# created: now\n# public key: age1abc\nAGE-SECRET-KEY-1EXAMPLE\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}

	got, err := readKeyFile(path, "AGE-SECRET-KEY-1")
	if err != nil {
		t.Fatalf("readKeyFile() returned error: %v", err)
	}
	if got != "AGE-SECRET-KEY-1EXAMPLE" {
		t.Fatalf("readKeyFile() = %q, want AGE-SECRET-KEY-1EXAMPLE", got)
	}
}

func TestReadKeyFileReportsMissingPrefix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recipient.txt")
	if err := os.WriteFile(path, []byte("age1example\n"), 0644); err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}

	_, err := readKeyFile(path, "AGE-SECRET-KEY-1")
	if err == nil {
		t.Fatalf("readKeyFile() succeeded with missing prefix")
	}
	if !strings.Contains(err.Error(), "key with prefix AGE-SECRET-KEY-1 not found") {
		t.Fatalf("readKeyFile() error = %q, want missing prefix", err)
	}
}

func TestCommandOptionShortTags(t *testing.T) {
	tests := []struct {
		name      string
		cmd       any
		fieldName string
		wantShort string
	}{
		{name: "keygen output dir", cmd: KeygenCmd{}, fieldName: "OutputDir", wantShort: "o"},
		{name: "keygen force", cmd: KeygenCmd{}, fieldName: "Force", wantShort: "f"},
		{name: "init remote", cmd: InitCmd{}, fieldName: "Remote", wantShort: "R"},
		{name: "init recipient", cmd: InitCmd{}, fieldName: "Recipient", wantShort: "r"},
		{name: "init force", cmd: InitCmd{}, fieldName: "Force", wantShort: "f"},
		{name: "upload recipient", cmd: UploadCmd{}, fieldName: "Recipient", wantShort: "r"},
		{name: "upload remote", cmd: UploadCmd{}, fieldName: "Remote", wantShort: "R"},
		{name: "backup identity", cmd: BackupCmd{}, fieldName: "Identity", wantShort: "i"},
		{name: "backup remote", cmd: BackupCmd{}, fieldName: "Remote", wantShort: "R"},
		{name: "restore identity", cmd: RestoreCmd{}, fieldName: "Identity", wantShort: "i"},
		{name: "restore remote", cmd: RestoreCmd{}, fieldName: "Remote", wantShort: "R"},
		{name: "restore output dir", cmd: RestoreCmd{}, fieldName: "OutputDir", wantShort: "o"},
		{name: "verify identity", cmd: VerifyCmd{}, fieldName: "Identity", wantShort: "i"},
		{name: "verify remote", cmd: VerifyCmd{}, fieldName: "Remote", wantShort: "R"},
		{name: "snapshots identity", cmd: SnapshotsCmd{}, fieldName: "Identity", wantShort: "i"},
		{name: "snapshots remote", cmd: SnapshotsCmd{}, fieldName: "Remote", wantShort: "R"},
		{name: "check identity", cmd: CheckCmd{}, fieldName: "Identity", wantShort: "i"},
		{name: "check remote", cmd: CheckCmd{}, fieldName: "Remote", wantShort: "R"},
		{name: "setup dir", cmd: SetupCmd{}, fieldName: "Dir", wantShort: "D"},
		{name: "setup update", cmd: SetupCmd{}, fieldName: "Update", wantShort: "u"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field, ok := reflect.TypeOf(tt.cmd).FieldByName(tt.fieldName)
			if !ok {
				t.Fatalf("field %s not found on %T", tt.fieldName, tt.cmd)
			}
			if got := field.Tag.Get("short"); got != tt.wantShort {
				t.Fatalf("short tag = %q, want %q", got, tt.wantShort)
			}
		})
	}
}

func TestUploadCmdFileIsExistingFileArg(t *testing.T) {
	field, ok := reflect.TypeOf(UploadCmd{}).FieldByName("File")
	if !ok {
		t.Fatal("UploadCmd.File missing")
	}
	if field.Tag.Get("type") != "existingfile" {
		t.Fatalf("type tag = %q, want existingfile", field.Tag.Get("type"))
	}
	if _, ok := field.Tag.Lookup("arg"); !ok {
		t.Fatal("File should be a positional arg")
	}
}

func TestResolveInitRecipientsReadsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recipient.txt")
	content := "# comment\n\nage1aaa\nage1bbb\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write recipient file: %v", err)
	}

	got, err := resolveInitRecipients(path)
	if err != nil {
		t.Fatalf("resolveInitRecipients() error: %v", err)
	}
	want := []string{"age1aaa", "age1bbb"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolveInitRecipients() = %v, want %v", got, want)
	}
}

func TestResolveInitRecipientsParsesInlineKeys(t *testing.T) {
	got, err := resolveInitRecipients("age1aaa, age1bbb")
	if err != nil {
		t.Fatalf("resolveInitRecipients() error: %v", err)
	}
	want := []string{"age1aaa", "age1bbb"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolveInitRecipients() = %v, want %v", got, want)
	}
}

func TestResolveInitRecipientsEmptyFileErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recipient.txt")
	if err := os.WriteFile(path, []byte("# only a comment\n\n"), 0644); err != nil {
		t.Fatalf("failed to write recipient file: %v", err)
	}

	_, err := resolveInitRecipients(path)
	if err == nil {
		t.Fatal("resolveInitRecipients() succeeded with empty file")
	}
	if !strings.Contains(err.Error(), "no recipients") {
		t.Fatalf("resolveInitRecipients() error = %q, want no recipients mention", err)
	}
}

func TestResolveInitRecipientsMissingFileErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.txt")

	_, err := resolveInitRecipients(path)
	if err == nil {
		t.Fatal("resolveInitRecipients() succeeded with missing file")
	}
	if !strings.Contains(err.Error(), "gloak keygen") {
		t.Fatalf("resolveInitRecipients() error = %q, want gloak keygen mention", err)
	}
}
