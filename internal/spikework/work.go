// Package spikework implements the same deterministic workload in native Go and WASI.
package spikework

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

type Output struct {
	InputSHA256 string `json:"input_sha256"`
	Checksum    uint64 `json:"checksum"`
	ReadNS      int64  `json:"read_ns"`
	ComputeNS   int64  `json:"compute_ns"`
}

func Run(ctx context.Context, input []byte, rounds uint64) (Output, error) {
	start := time.Now()
	digest := sha256.Sum256(input)
	x := uint64(digest[0]) + 1
	for done := uint64(0); done < rounds; {
		if err := ctx.Err(); err != nil {
			return Output{}, err
		}
		chunk := min(rounds-done, 1<<18)
		for i := uint64(0); i < chunk; i++ {
			x ^= x << 13
			x ^= x >> 7
			x ^= x << 17
		}
		done += chunk
	}
	return Output{InputSHA256: hex.EncodeToString(digest[:]), Checksum: x, ComputeNS: time.Since(start).Nanoseconds()}, nil
}
