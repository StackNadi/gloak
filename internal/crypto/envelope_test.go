package crypto

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestSealOpenChunkRoundTrip(t *testing.T) {
	master := make([]byte, 32)
	if _, err := rand.Read(master); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	plain := []byte("payload with enough bytes to compress meaningfully, payload payload payload")
	sum := sha256Hex(plain)

	sealed, err := SealChunk(master, sum, plain)
	if err != nil {
		t.Fatalf("SealChunk() error: %v", err)
	}
	got, err := OpenChunk(master, sum, sealed)
	if err != nil {
		t.Fatalf("OpenChunk() error: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("OpenChunk() = %q, want %q", got, plain)
	}
}

func TestOpenChunkRejectsTamperedCiphertext(t *testing.T) {
	master := []byte("0123456789abcdef0123456789abcdef")
	plain := []byte("important data")
	sum := sha256Hex(plain)
	sealed, err := SealChunk(master, sum, plain)
	if err != nil {
		t.Fatalf("SealChunk() error: %v", err)
	}
	sealed[len(sealed)-1] ^= 0xff
	if _, err := OpenChunk(master, sum, sealed); err == nil {
		t.Fatal("OpenChunk() succeeded with tampered ciphertext")
	}
}

func TestOpenChunkRejectsWrongChunkHash(t *testing.T) {
	master := []byte("0123456789abcdef0123456789abcdef")
	plain := []byte("important data")
	sealed, err := SealChunk(master, sha256Hex(plain), plain)
	if err != nil {
		t.Fatalf("SealChunk() error: %v", err)
	}
	wrongSum := sha256Hex([]byte("other data"))
	if _, err := OpenChunk(master, wrongSum, sealed); err == nil {
		t.Fatal("OpenChunk() succeeded with wrong chunk hash")
	}
}

func TestOpenChunkRejectsWrongMasterKey(t *testing.T) {
	master := []byte("0123456789abcdef0123456789abcdef")
	other := []byte("fedcba9876543210fedcba9876543210")
	plain := []byte("important data")
	sum := sha256Hex(plain)
	sealed, err := SealChunk(master, sum, plain)
	if err != nil {
		t.Fatalf("SealChunk() error: %v", err)
	}
	if _, err := OpenChunk(other, sum, sealed); err == nil {
		t.Fatal("OpenChunk() succeeded with wrong master key")
	}
}

func TestSealChunkRejectsInvalidChunkHash(t *testing.T) {
	master := []byte("0123456789abcdef0123456789abcdef")
	_, err := SealChunk(master, "not-hex", []byte("x"))
	if err == nil {
		t.Fatal("SealChunk() succeeded with invalid chunk hash")
	}
	if !strings.Contains(err.Error(), "invalid chunk hash") {
		t.Fatalf("SealChunk() error = %v, want invalid chunk hash", err)
	}
}
