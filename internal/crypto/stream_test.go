package crypto

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEncryptWriterDecryptReaderRoundTrip(t *testing.T) {
	pubKey, _, identityFile, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() returned error: %v", err)
	}
	identityPath := writeIdentityFile(t, identityFile)

	plaintext := []byte("backup payload\nwith multiple lines")
	var encrypted bytes.Buffer

	w, err := EncryptWriter(pubKey, &encrypted)
	if err != nil {
		t.Fatalf("EncryptWriter() returned error: %v", err)
	}
	if _, err := w.Write(plaintext); err != nil {
		t.Fatalf("encrypted writer Write() returned error: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("encrypted writer Close() returned error: %v", err)
	}

	r, err := DecryptReader(identityPath, &encrypted)
	if err != nil {
		t.Fatalf("DecryptReader() returned error: %v", err)
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("failed to read decrypted payload: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("decrypted payload mismatch: got %q want %q", got, plaintext)
	}
}

func TestEncryptWriterRejectsInvalidRecipient(t *testing.T) {
	_, err := EncryptWriter("not-an-age-recipient", io.Discard)
	if err == nil {
		t.Fatalf("EncryptWriter() succeeded with invalid recipient")
	}
	if !strings.Contains(err.Error(), "invalid public key") {
		t.Fatalf("EncryptWriter() error = %q, want invalid public key", err)
	}
}

func TestDecryptReaderRejectsMissingIdentityFile(t *testing.T) {
	_, err := DecryptReader(filepath.Join(t.TempDir(), "missing.txt"), bytes.NewReader(nil))
	if err == nil {
		t.Fatalf("DecryptReader() succeeded with missing identity file")
	}
	if !strings.Contains(err.Error(), "failed to open key file") {
		t.Fatalf("DecryptReader() error = %q, want failed to open key file", err)
	}
}

func writeIdentityFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "identity.txt")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write identity file: %v", err)
	}
	return path
}
