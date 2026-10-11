// Package chunker implements content-defined chunking with a gear hash.
package chunker

import (
	"bufio"
	"fmt"
	"io"
)

const windowSize = 64

// gear is filled once at init from a fixed seed so every binary builds the
// same table and CDC boundaries stay stable across runs and machines.
var gear [256]uint64

func init() {
	x := uint64(0x9E3779B97F4A7C15)
	for i := range gear {
		x += 0x9E3779B97F4A7C15
		z := x
		z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
		z = (z ^ (z >> 27)) * 0x94D049BB133111EB
		gear[i] = z ^ (z >> 31)
	}
}

type CDC struct {
	min int64
	avg int64
	max int64
}

// NewCDC validates the bounds and rounds max up to a multiple of windowSize.
func NewCDC(min, avg, max int64) (*CDC, error) {
	if min <= 0 || avg <= 0 || max <= 0 {
		return nil, fmt.Errorf("chunk size must be positive (min=%d avg=%d max=%d)", min, avg, max)
	}
	if max < avg || avg < min {
		return nil, fmt.Errorf("chunk bounds require max >= avg >= min (min=%d avg=%d max=%d)", min, avg, max)
	}
	if max%windowSize != 0 {
		max = ((max + windowSize - 1) / windowSize) * windowSize
	}
	return &CDC{min: min, avg: avg, max: max}, nil
}

// Split reads r and calls fn once per chunk. Each chunk is a fresh slice the
// caller may retain. An empty reader produces no calls.
func (c *CDC) Split(r io.Reader, fn func(chunk []byte) error) error {
	br := bufio.NewReaderSize(r, 64*1024)
	chunk := make([]byte, 0, c.max)
	var h uint64
	for {
		b, err := br.ReadByte()
		if err == io.EOF {
			if len(chunk) > 0 {
				return fn(chunk)
			}
			return nil
		}
		if err != nil {
			return err
		}
		chunk = append(chunk, b)
		h = (h << 1) + gear[b]
		if int64(len(chunk)) >= c.max {
			if err := fn(chunk); err != nil {
				return err
			}
			chunk = make([]byte, 0, c.max)
			h = 0
			continue
		}
		if int64(len(chunk)) >= c.min && h%uint64(c.avg) == 0 {
			if err := fn(chunk); err != nil {
				return err
			}
			chunk = make([]byte, 0, c.max)
			h = 0
		}
	}
}
