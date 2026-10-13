package crypto

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func generateTestKey(t *testing.T) (string, string) {
	t.Helper()
	pub, _, fileContent, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error: %v", err)
	}
	path := filepath.Join(t.TempDir(), "identity.txt")
	if err := os.WriteFile(path, []byte(fileContent), 0600); err != nil {
		t.Fatalf("write identity: %v", err)
	}
	return pub, path
}

func TestEncryptMultiOpensWithAnyRecipientIdentity(t *testing.T) {
	pubA, identA := generateTestKey(t)
	pubB, identB := generateTestKey(t)
	_, identC := generateTestKey(t)

	plaintext := []byte(`{"hello":"world"}`)
	sealed, err := EncryptMulti([]string{pubA, pubB}, plaintext)
	if err != nil {
		t.Fatalf("EncryptMulti() error: %v", err)
	}

	for _, identPath := range []string{identA, identB} {
		r, err := DecryptReader(identPath, bytes.NewReader(sealed))
		if err != nil {
			t.Fatalf("DecryptReader(%s) error: %v", identPath, err)
		}
		got, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("ReadAll error: %v", err)
		}
		if !bytes.Equal(got, plaintext) {
			t.Fatalf("decrypted = %q, want %q", got, plaintext)
		}
	}

	if r, err := DecryptReader(identC, bytes.NewReader(sealed)); err == nil {
		if _, err := io.ReadAll(r); err == nil {
			t.Fatal("DecryptReader succeeded with unrelated identity")
		}
	}
}

func TestEncryptMultiRejectsNoRecipients(t *testing.T) {
	if _, err := EncryptMulti(nil, []byte("x")); err == nil {
		t.Fatal("EncryptMulti() succeeded with no recipients")
	}
}

func TestEncryptMultiRejectsInvalidRecipient(t *testing.T) {
	if _, err := EncryptMulti([]string{"age1notavalidkey"}, []byte("x")); err == nil {
		t.Fatal("EncryptMulti() succeeded with invalid recipient")
	}
}

func TestFingerprintRecipientIsStableAndShort(t *testing.T) {
	pub, _ := generateTestKey(t)
	fp := FingerprintRecipient(pub)
	if fp != FingerprintRecipient(pub) {
		t.Fatal("FingerprintRecipient() is not stable")
	}
	if len(fp) != 16 {
		t.Fatalf("FingerprintRecipient() = %q, want 16 hex chars", fp)
	}
}
