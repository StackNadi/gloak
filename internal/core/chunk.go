package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
)

const ChunkSize = 20 * 1024 * 1024

type ChunkMeta struct {
	Index  int    `json:"index"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

func StreamChunker(in io.Reader, uploadFn func(chunkReader io.Reader, index int) error) ([]ChunkMeta, error) {
	return StreamChunkerWithCallback(in, uploadFn, nil)
}

func StreamChunkerWithCallback(in io.Reader, uploadFn func(chunkReader io.Reader, index int) error, afterChunkFn func(ChunkMeta) error) ([]ChunkMeta, error) {
	return StreamChunkerWithSize(in, ChunkSize, uploadFn, afterChunkFn)
}

func StreamChunkerWithSize(in io.Reader, chunkSize int64, uploadFn func(chunkReader io.Reader, index int) error, afterChunkFn func(ChunkMeta) error) ([]ChunkMeta, error) {
	if chunkSize <= 0 {
		return nil, fmt.Errorf("chunk size must be positive")
	}

	var metadata []ChunkMeta
	chunkIndex := 0

	for {
		limitedReader := io.LimitReader(in, chunkSize)

		hasher := sha256.New()
		tee := io.TeeReader(limitedReader, hasher)

		counter := &byteCounter{Reader: tee}

		err := uploadFn(counter, chunkIndex)
		if err != nil {
			return nil, fmt.Errorf("chunk %d failed to process: %w", chunkIndex, err)
		}

		if counter.BytesRead == 0 {
			break
		}

		hashString := hex.EncodeToString(hasher.Sum(nil))
		chunk := ChunkMeta{
			Index:  chunkIndex,
			Name:   fmt.Sprintf("chunk_%05d", chunkIndex),
			Size:   counter.BytesRead,
			SHA256: hashString,
		}
		metadata = append(metadata, chunk)
		if afterChunkFn != nil {
			if err := afterChunkFn(chunk); err != nil {
				return nil, fmt.Errorf("chunk %d callback failed: %w", chunkIndex, err)
			}
		}

		if counter.BytesRead < chunkSize {
			break
		}

		chunkIndex++
	}

	return metadata, nil
}

type byteCounter struct {
	io.Reader
	BytesRead int64
}

func (b *byteCounter) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	b.BytesRead += int64(n)
	return n, err
}
