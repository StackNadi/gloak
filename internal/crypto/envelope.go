package crypto

import (
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/klauspost/compress/zstd"
	"golang.org/x/crypto/chacha20poly1305"
)

const chunkKeyInfo = "gloak-chunk-v1"

// deriveChunkKey derives a 32-byte chunk key from masterKey using the decoded
// plaintext SHA-256 hash as salt. It rejects malformed hex or hashes that do
// not decode to 32 bytes and returns key derivation errors.
func deriveChunkKey(masterKey []byte, plainSHA256Hex string) ([]byte, error) {
	salt, err := hex.DecodeString(plainSHA256Hex)
	if err != nil || len(salt) != sha256.Size {
		return nil, fmt.Errorf("invalid chunk hash: %q", plainSHA256Hex)
	}
	return hkdf.Key(sha256.New, masterKey, salt, chunkKeyInfo, chacha20poly1305.KeySize)
}

// SealChunk compresses plain with zstd, then encrypts it with XChaCha20-Poly1305
// under a key derived from masterKey. Output layout: 24-byte nonce || ciphertext.
// plainSHA256Hex must encode 32 bytes; its exact text is authenticated, but its
// value is not checked against plain. Hash decoding, key derivation, compression,
// and encryption setup errors are returned.
func SealChunk(masterKey []byte, plainSHA256Hex string, plain []byte) ([]byte, error) {
	key, err := deriveChunkKey(masterKey, plainSHA256Hex)
	if err != nil {
		return nil, err
	}
	compressed, err := zstdEncode(plain)
	if err != nil {
		return nil, fmt.Errorf("failed to compress chunk: %w", err)
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("failed to init cipher: %w", err)
	}
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}
	sealed := make([]byte, 0, len(nonce)+len(compressed)+aead.Overhead())
	sealed = append(sealed, nonce...)
	sealed = aead.Seal(sealed, nonce, compressed, []byte(plainSHA256Hex))
	return sealed, nil
}

// OpenChunk authenticates and decrypts a SealChunk envelope, then decompresses
// it. masterKey and the exact plainSHA256Hex string must match those used to
// seal it. It returns errors for malformed hashes, short envelopes, key or
// cipher setup failures, authentication failures, and invalid compressed data.
// Callers must check the returned plaintext's hash themselves.
func OpenChunk(masterKey []byte, plainSHA256Hex string, sealed []byte) ([]byte, error) {
	key, err := deriveChunkKey(masterKey, plainSHA256Hex)
	if err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("failed to init cipher: %w", err)
	}
	if len(sealed) < chacha20poly1305.NonceSizeX+aead.Overhead() {
		return nil, fmt.Errorf("chunk too short: %d bytes", len(sealed))
	}
	nonce := sealed[:chacha20poly1305.NonceSizeX]
	ciphertext := sealed[chacha20poly1305.NonceSizeX:]
	compressed, err := aead.Open(nil, nonce, ciphertext, []byte(plainSHA256Hex))
	if err != nil {
		return nil, fmt.Errorf("chunk authentication failed: %w", err)
	}
	plain, err := zstdDecode(compressed)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress chunk: %w", err)
	}
	return plain, nil
}

// zstdEncode returns src compressed as a zstd frame, or an encoder setup error.
func zstdEncode(src []byte) ([]byte, error) {
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return nil, err
	}
	defer enc.Close()
	return enc.EncodeAll(src, nil), nil
}

// zstdDecode decompresses zstd data, returning decoder setup or decoding errors.
func zstdDecode(src []byte) ([]byte, error) {
	dec, err := zstd.NewReader(nil)
	if err != nil {
		return nil, err
	}
	defer dec.Close()
	return dec.DecodeAll(src, nil)
}
