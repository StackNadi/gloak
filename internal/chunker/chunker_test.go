package chunker

import (
	"bytes"
	"io"
	"math/rand"
	"testing"
)

func mustCDC(t *testing.T, min, avg, max int64) *CDC {
	t.Helper()
	c, err := NewCDC(min, avg, max)
	if err != nil {
		t.Fatalf("NewCDC(%d,%d,%d) error: %v", min, avg, max, err)
	}
	return c
}

func splitAll(t *testing.T, c *CDC, data []byte) [][]byte {
	t.Helper()
	var chunks [][]byte
	err := c.Split(bytes.NewReader(data), func(chunk []byte) error {
		cp := make([]byte, len(chunk))
		copy(cp, chunk)
		chunks = append(chunks, cp)
		return nil
	})
	if err != nil {
		t.Fatalf("Split() error: %v", err)
	}
	return chunks
}

func TestNewCDCRejectsBadBounds(t *testing.T) {
	cases := []struct{ min, avg, max int64 }{
		{0, 1, 2},
		{2, 1, 4},
		{2, 4, 3},
		{2, 2, 1},
	}
	for _, c := range cases {
		if _, err := NewCDC(c.min, c.avg, c.max); err == nil {
			t.Fatalf("NewCDC(%d,%d,%d) succeeded", c.min, c.avg, c.max)
		}
	}
}

func TestSplitEmptyReader(t *testing.T) {
	c := mustCDC(t, 4, 8, 16)
	chunks := splitAll(t, c, nil)
	if len(chunks) != 0 {
		t.Fatalf("chunks = %d, want 0", len(chunks))
	}
}

func TestSplitSmallerThanMinIsOneChunk(t *testing.T) {
	c := mustCDC(t, 64, 128, 256)
	data := []byte("short")
	chunks := splitAll(t, c, data)
	if len(chunks) != 1 || !bytes.Equal(chunks[0], data) {
		t.Fatalf("chunks = %d, want 1 equal to input", len(chunks))
	}
}

func TestSplitRespectsMax(t *testing.T) {
	c := mustCDC(t, 4, 64, 64)
	data := bytes.Repeat([]byte{0xFF}, 200)
	chunks := splitAll(t, c, data)
	for i, ch := range chunks {
		if int64(len(ch)) > 64 {
			t.Fatalf("chunk %d length %d > max 64", i, len(ch))
		}
	}
	var total int
	for _, ch := range chunks {
		total += len(ch)
	}
	if total != len(data) {
		t.Fatalf("total %d != %d", total, len(data))
	}
}

func TestSplitDeterminism(t *testing.T) {
	data := make([]byte, 50_000)
	rng := rand.New(rand.NewSource(42))
	rng.Read(data)
	a := splitAll(t, mustCDC(t, 512, 2048, 8192), data)
	b := splitAll(t, mustCDC(t, 512, 2048, 8192), data)
	if len(a) != len(b) {
		t.Fatalf("chunk counts %d vs %d", len(a), len(b))
	}
	for i := range a {
		if !bytes.Equal(a[i], b[i]) {
			t.Fatalf("chunk %d differs", i)
		}
	}
	if len(a) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(a))
	}
}

func TestSplitRoundTripBytes(t *testing.T) {
	data := make([]byte, 100_000)
	rng := rand.New(rand.NewSource(7))
	rng.Read(data)
	chunks := splitAll(t, mustCDC(t, 1024, 4096, 16384), data)
	var out []byte
	for _, ch := range chunks {
		out = append(out, ch...)
	}
	if !bytes.Equal(out, data) {
		t.Fatal("concatenated chunks != input")
	}
}

func TestSplitCallbackErrorPropagates(t *testing.T) {
	c := mustCDC(t, 4, 8, 16)
	sentinel := io.ErrClosedPipe
	err := c.Split(bytes.NewReader([]byte("abcdefghij")), func([]byte) error {
		return sentinel
	})
	if err != sentinel {
		t.Fatalf("err = %v, want sentinel", err)
	}
}

func TestMaxRoundedUpToWindowMultiple(t *testing.T) {
	c := mustCDC(t, 4, 8, 100)
	if c.max%64 != 0 {
		t.Fatalf("max = %d, want multiple of 64", c.max)
	}
	if c.max < 100 {
		t.Fatalf("max = %d, want >= 100", c.max)
	}
}
