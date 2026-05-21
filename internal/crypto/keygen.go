package crypto

import (
	"fmt"
	"time"

	"filippo.io/age"
)

func GenerateKey() (string, string, string, error) {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return "", "", "", fmt.Errorf("failed to generate age key: %w", err)
	}

	pubKey := identity.Recipient().String()
	privKey := identity.String()
	timestamp := time.Now().Format(time.RFC3339)

	fileContent := fmt.Sprintf("# created: %s\n# public key: %s\n%s\n", timestamp, pubKey, privKey)

	return pubKey, privKey, fileContent, nil
}
