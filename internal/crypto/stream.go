package crypto

import (
	"fmt"
	"io"
	"os"

	"filippo.io/age"
)

func EncryptWriter(pubKey string, out io.Writer) (io.WriteCloser, error) {
	recipient, err := age.ParseX25519Recipient(pubKey)
	if err != nil {
		return nil, fmt.Errorf("invalid public key: %w", err)
	}

	ageWriter, err := age.Encrypt(out, recipient)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize age encryptor: %w", err)
	}

	return ageWriter, nil
}

func DecryptReader(identityPath string, in io.Reader) (io.Reader, error) {
	identityFile, err := os.Open(identityPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open key file: %w", err)
	}
	defer identityFile.Close()

	identities, err := age.ParseIdentities(identityFile)
	if err != nil {
		return nil, fmt.Errorf("invalid private key format: %w", err)
	}

	ageReader, err := age.Decrypt(in, identities...)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt (invalid key or corrupted file): %w", err)
	}

	return ageReader, nil
}
