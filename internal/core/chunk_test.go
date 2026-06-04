package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestStreamChunkerBuildsChunkMetadata(t *testing.T) {
	payload := bytes.Repeat([]byte("a"), ChunkSize+3)
	var uploaded [][]byte

	chunks, err := StreamChunker(bytes.NewReader(payload), func(chunkReader io.Reader, index int) error {
		data, err := io.ReadAll(chunkReader)
		if err != nil {
			return fmt.Errorf("read chunk %d: %w", index, err)
		}
		uploaded = append(uploaded, data)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamChunker() returned error: %v", err)
	}

	if len(chunks) != 2 {
		t.Fatalf("chunk count = %d, want 2", len(chunks))
	}
	if len(uploaded) != 2 {
		t.Fatalf("upload call count = %d, want 2", len(uploaded))
	}

	wantSizes := []int64{ChunkSize, 3}
	for i, chunk := range chunks {
		if chunk.Index != i {
			t.Fatalf("chunk %d index = %d, want %d", i, chunk.Index, i)
		}
		wantName := fmt.Sprintf("chunk_%05d", i)
		if chunk.Name != wantName {
			t.Fatalf("chunk %d name = %q, want %q", i, chunk.Name, wantName)
		}
		if chunk.Size != wantSizes[i] {
			t.Fatalf("chunk %d size = %d, want %d", i, chunk.Size, wantSizes[i])
		}
		wantHash := sha256.Sum256(uploaded[i])
		if chunk.SHA256 != hex.EncodeToString(wantHash[:]) {
			t.Fatalf("chunk %d sha256 = %q, want %q", i, chunk.SHA256, hex.EncodeToString(wantHash[:]))
		}
	}
}

func TestStreamChunkerPropagatesUploadError(t *testing.T) {
	wantErr := errors.New("upload failed")
	_, err := StreamChunker(bytes.NewReader([]byte("payload")), func(io.Reader, int) error {
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("StreamChunker() error = %v, want wrapped %v", err, wantErr)
	}
}

func TestStreamChunkerWithCallbackReportsChunkMetadata(t *testing.T) {
	payload := []byte("payload")
	var seen []ChunkMeta

	chunks, err := StreamChunkerWithCallback(bytes.NewReader(payload), func(chunkReader io.Reader, index int) error {
		_, err := io.Copy(io.Discard, chunkReader)
		return err
	}, func(chunk ChunkMeta) error {
		seen = append(seen, chunk)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamChunkerWithCallback() returned error: %v", err)
	}
	if len(chunks) != 1 || len(seen) != 1 {
		t.Fatalf("chunks = %d, callback chunks = %d, want 1 and 1", len(chunks), len(seen))
	}
	if seen[0] != chunks[0] {
		t.Fatalf("callback chunk = %+v, want %+v", seen[0], chunks[0])
	}
}

func TestStreamChunkerWithCallbackPropagatesCallbackError(t *testing.T) {
	wantErr := errors.New("save state failed")
	_, err := StreamChunkerWithCallback(bytes.NewReader([]byte("payload")), func(chunkReader io.Reader, index int) error {
		_, err := io.Copy(io.Discard, chunkReader)
		return err
	}, func(ChunkMeta) error {
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("StreamChunkerWithCallback() error = %v, want wrapped %v", err, wantErr)
	}
}

func TestStreamChunkerWithSizeUsesCustomChunkSize(t *testing.T) {
	chunks, err := StreamChunkerWithSize(bytes.NewReader([]byte("abcdefghij")), 4, func(chunkReader io.Reader, index int) error {
		_, err := io.Copy(io.Discard, chunkReader)
		return err
	}, nil)
	if err != nil {
		t.Fatalf("StreamChunkerWithSize() returned error: %v", err)
	}

	wantSizes := []int64{4, 4, 2}
	if len(chunks) != len(wantSizes) {
		t.Fatalf("chunk count = %d, want %d", len(chunks), len(wantSizes))
	}
	for i, chunk := range chunks {
		if chunk.Size != wantSizes[i] {
			t.Fatalf("chunk %d size = %d, want %d", i, chunk.Size, wantSizes[i])
		}
	}
}

func TestStreamChunkerWithSizeRejectsInvalidChunkSize(t *testing.T) {
	_, err := StreamChunkerWithSize(bytes.NewReader([]byte("payload")), 0, func(io.Reader, int) error {
		return nil
	}, nil)
	if err == nil {
		t.Fatalf("StreamChunkerWithSize() succeeded with zero chunk size")
	}
	if !strings.Contains(err.Error(), "chunk size must be positive") {
		t.Fatalf("StreamChunkerWithSize() error = %q, want chunk size error", err)
	}
}

func TestStreamChunkerEmptyInput(t *testing.T) {
	called := 0
	chunks, err := StreamChunker(bytes.NewReader(nil), func(chunkReader io.Reader, index int) error {
		called++
		_, err := io.Copy(io.Discard, chunkReader)
		return err
	})
	if err != nil {
		t.Fatalf("StreamChunker() returned error: %v", err)
	}
	if len(chunks) != 0 {
		t.Fatalf("chunk count = %d, want 0", len(chunks))
	}
	if called != 1 {
		t.Fatalf("upload callback count = %d, want 1 empty read", called)
	}
}
