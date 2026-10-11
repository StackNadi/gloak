package crypto

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"filippo.io/age"
	"filippo.io/age/agessh"
)

// ParseRecipient accepts age X25519 keys (age1...) and SSH public keys
// (ssh-ed25519 / ssh-rsa). It is the single entry point for repository
// recipients so init, upload, and sealing all accept the same formats.
func ParseRecipient(s string) (age.Recipient, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "ssh-ed25519 ") || strings.HasPrefix(s, "ssh-rsa ") {
		return agessh.ParseRecipient(s)
	}
	return age.ParseX25519Recipient(s)
}

// EncryptMulti encrypts plaintext to every recipient. Any matching identity
// can decrypt the result.
func EncryptMulti(recipientKeys []string, plaintext []byte) ([]byte, error) {
	if len(recipientKeys) == 0 {
		return nil, fmt.Errorf("no recipients given")
	}
	recipients := make([]age.Recipient, 0, len(recipientKeys))
	for _, key := range recipientKeys {
		r, err := ParseRecipient(key)
		if err != nil {
			return nil, fmt.Errorf("invalid recipient %q: %w", key, err)
		}
		recipients = append(recipients, r)
	}

	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, recipients...)
	if err != nil {
		return nil, fmt.Errorf("failed to init age encryptor: %w", err)
	}
	if _, err := w.Write(plaintext); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("failed to write plaintext: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("failed to finish age encryption: %w", err)
	}
	return buf.Bytes(), nil
}

// FingerprintRecipient returns a short stable identifier for a recipient key.
func FingerprintRecipient(recipient string) string {
	sum := sha256.Sum256([]byte(recipient))
	return hex.EncodeToString(sum[:])[:16]
}
