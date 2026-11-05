package demo

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func compress(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func readAll(t *testing.T, r io.Reader, limit int64) ([]byte, error) {
	t.Helper()
	s, err := decompress(r, limit)
	if err != nil {
		return nil, err
	}
	defer s.close()
	return io.ReadAll(s)
}

func TestDecompress(t *testing.T) {
	data := bytes.Repeat([]byte("PBDEMS2 frames of a demo "), 4000) // 100 000 bytes
	zst := compress(t, data)

	got, err := readAll(t, bytes.NewReader(zst), int64(len(data)))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("at the limit: %d bytes, %v; want all %d bytes", len(got), err, len(data))
	}
	if _, err := readAll(t, bytes.NewReader(zst), int64(len(data))-1); !errors.Is(err, ErrTooLarge) {
		t.Errorf("a byte over the limit: %v, want ErrTooLarge", err)
	}
	// A bytes.Buffer is what the decoder would decode at once, skipping
	// the limit of Read, if it were allowed to.
	if _, err := readAll(t, bytes.NewBuffer(zst), 1000); !errors.Is(err, ErrTooLarge) {
		t.Errorf("a bytes.Buffer over the limit: %v, want ErrTooLarge", err)
	}
}

func TestDecompressFrameDeclaresTooMuch(t *testing.T) {
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	// EncodeAll writes the content size into the frame header.
	zst := enc.EncodeAll(make([]byte, 64<<10), nil)
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readAll(t, bytes.NewReader(zst), 1<<10); !errors.Is(err, ErrTooLarge) {
		t.Errorf("err = %v, want ErrTooLarge", err)
	}
}

func TestDecompressNotZstd(t *testing.T) {
	_, err := readAll(t, strings.NewReader("PBDEMS2\x00 a demo that was not compressed"), DefaultMaxSize)
	if !errors.Is(err, ErrNotZstd) {
		t.Errorf("err = %v, want ErrNotZstd", err)
	}
}

func TestDecompressWindowTooLarge(t *testing.T) {
	// A frame header that asks for a 64 MiB window, twice maxWindow: the
	// magic number, a descriptor with no flags, the window descriptor
	// 1<<(10+16), then an empty raw block that ends the frame.
	frame := []byte{0x28, 0xb5, 0x2f, 0xfd, 0x00, 16 << 3, 0x01, 0x00, 0x00}
	_, err := readAll(t, bytes.NewReader(frame), DefaultMaxSize)
	if !errors.Is(err, zstd.ErrWindowSizeExceeded) {
		t.Errorf("err = %v, want zstd.ErrWindowSizeExceeded", err)
	}
	// The same frame with a 1 MiB window decodes to nothing.
	frame[5] = 10 << 3
	if got, err := readAll(t, bytes.NewReader(frame), DefaultMaxSize); err != nil || len(got) != 0 {
		t.Errorf("1 MiB window: %d bytes, %v; want an empty stream", len(got), err)
	}
}

func TestDecompressTruncated(t *testing.T) {
	zst := compress(t, bytes.Repeat([]byte("demo"), 100000))
	_, err := readAll(t, bytes.NewReader(zst[:len(zst)/2]), DefaultMaxSize)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("err = %v, want io.ErrUnexpectedEOF, which the parser takes for a truncated demo", err)
	}
}
