package crypto

import (
	"strings"
	"testing"

	"filippo.io/age"
)

func TestGenerateKeyReturnsValidAgeKeys(t *testing.T) {
	pubKey, privKey, fileContent, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() returned error: %v", err)
	}

	if _, err := age.ParseX25519Recipient(pubKey); err != nil {
		t.Fatalf("public key is not a valid age recipient: %v", err)
	}

	if _, err := age.ParseX25519Identity(privKey); err != nil {
		t.Fatalf("private key is not a valid age identity: %v", err)
	}

	if !strings.Contains(fileContent, "# public key: "+pubKey) {
		t.Fatalf("identity file content does not include public key comment")
	}
	if !strings.Contains(fileContent, privKey) {
		t.Fatalf("identity file content does not include private key")
	}
}
