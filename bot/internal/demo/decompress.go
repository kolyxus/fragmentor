package demo

import (
	"errors"
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"
)

// maxWindow bounds the zstd window, and with it the memory of the decoder.
// FACEIT compresses demos with a 2 MiB window.
const maxWindow = 32 << 20

// stream decompresses a .dem.zst on the fly, nothing goes to disk, and fails
// with ErrTooLarge once more than limit bytes come out.
type stream struct {
	dec   *zstd.Decoder
	limit int64
	read  int64
}

// decompress starts decompressing r. The decoder works synchronously, in
// the goroutine that reads, and must be released with close.
func decompress(r io.Reader, limit int64) (*stream, error) {
	dec, err := zstd.NewReader(r,
		zstd.WithDecoderConcurrency(1),
		zstd.WithDecoderLowmem(true),
		zstd.WithDecoderMaxWindow(maxWindow),
		// Bounds what a frame may declare, so a lying header fails at once.
		zstd.WithDecoderMaxMemory(uint64(limit)),
		// Never decode a whole in-memory input at once: that would skip
		// the limit of Read.
		zstd.WithDecodeBuffersBelow(0),
	)
	if err != nil {
		return nil, zstdError(err)
	}
	return &stream{dec: dec, limit: limit}, nil
}

func (s *stream) Read(p []byte) (int, error) {
	if s.read > s.limit {
		return 0, ErrTooLarge
	}
	// One byte over the limit is enough to tell a demo of exactly the
	// limit from a larger one.
	if room := s.limit - s.read + 1; int64(len(p)) > room {
		p = p[:room]
	}
	n, err := s.dec.Read(p)
	s.read += int64(n)
	if s.read > s.limit {
		return 0, ErrTooLarge
	}
	// io.EOF and io.ErrUnexpectedEOF stay as they are: the parser tells a
	// truncated demo by them.
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		err = zstdError(err)
	}
	return n, err
}

func (s *stream) close() { s.dec.Close() }

// zstdError adds context to a decoder error and maps the ones a caller cares
// about to our errors.
func zstdError(err error) error {
	switch {
	case errors.Is(err, zstd.ErrMagicMismatch):
		return fmt.Errorf("%w: %w", ErrNotZstd, err)
	case errors.Is(err, zstd.ErrDecoderSizeExceeded):
		return fmt.Errorf("%w: %w", ErrTooLarge, err)
	}
	return fmt.Errorf("demo: decompress: %w", err)
}
