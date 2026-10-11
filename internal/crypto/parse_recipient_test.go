package crypto

import (
	"testing"

	"filippo.io/age"
)

func TestParseRecipientAcceptsX25519(t *testing.T) {
	pub, _, _, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	r, err := ParseRecipient(pub)
	if err != nil {
		t.Fatalf("ParseRecipient() error: %v", err)
	}
	if _, ok := r.(*age.X25519Recipient); !ok {
		t.Fatalf("type = %T", r)
	}
}

func TestParseRecipientAcceptsSSHEd25519(t *testing.T) {
	// A well-formed ssh-ed25519 public key line (RFC 8709).
	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl test@host"
	r, err := ParseRecipient(key)
	if err != nil {
		t.Fatalf("ParseRecipient() error: %v", err)
	}
	if r == nil {
		t.Fatal("nil recipient")
	}
}

func TestParseRecipientRejectsGarbage(t *testing.T) {
	if _, err := ParseRecipient("not-a-key"); err == nil {
		t.Fatal("ParseRecipient() accepted garbage")
	}
}
