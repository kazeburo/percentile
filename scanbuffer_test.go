package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScanBufferProcessChunk(t *testing.T) {
	lines := []string{}
	cb := func(data []byte) error {
		lines = append(lines, string(data))
		return nil
	}
	sb := NewScanBuffer(WithStartBufSize(32))

	copy(sb.buf, "line1\nline2\nline3")
	err := sb.processChunk(17, cb)
	require.NoError(t, err)
	require.Equal(t, []string{"line1", "line2"}, lines)
	require.Equal(t, 5, sb.offset)
	require.Equal(t, "line3", string(sb.buf[0:sb.offset]))
}

func TestScanBufferFlushTrailingLine(t *testing.T) {
	lines := []string{}
	cb := func(data []byte) error {
		lines = append(lines, string(data))
		return nil
	}
	sb := NewScanBuffer(WithStartBufSize(32))

	copy(sb.buf, "no-newline")
	sb.offset = 10
	err := sb.flushTrailingLine(cb)
	require.NoError(t, err)
	require.Equal(t, []string{"no-newline"}, lines)
}

func TestScanBufferReadInto(t *testing.T) {
	sb := NewScanBuffer(WithStartBufSize(32))

	r := strings.NewReader("hello")
	n, eof, err := sb.readInto(r)
	require.NoError(t, err)
	require.False(t, eof)
	require.Equal(t, 5, n)
	require.Equal(t, "hello", string(sb.buf[0:5]))
}

func TestScanBufferReadIntoWithPrefix(t *testing.T) {
	sb := NewScanBuffer(WithStartBufSize(32))

	copy(sb.buf, "pre-")
	sb.offset = 4

	r := strings.NewReader("fix")
	n, eof, err := sb.readInto(r)
	require.NoError(t, err)
	require.False(t, eof)
	require.Equal(t, 3, n)
	require.Equal(t, "pre-fix", string(sb.buf[0:7]))
}

type errorReader struct {
	err error
}

func (e *errorReader) Read([]byte) (int, error) {
	return 0, e.err
}

func TestScanBufferReadIntoError(t *testing.T) {
	sb := NewScanBuffer(WithStartBufSize(32))

	errReader := &errorReader{err: io.ErrUnexpectedEOF}
	_, _, err := sb.readInto(errReader)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

func TestScanBufferExpand(t *testing.T) {
	sb := NewScanBuffer(WithStartBufSize(16))

	err := sb.expand(16)
	require.NoError(t, err)
	require.Equal(t, 32, len(sb.buf))
}

func TestScanBufferExpandMax(t *testing.T) {
	sb := NewScanBuffer(WithMaxBufSize(16))

	err := sb.expand(16)
	require.ErrorIs(t, err, ErrTokenTooLong)
}

func TestScanBufferExpandCappedByMax(t *testing.T) {
	sb := NewScanBuffer(WithStartBufSize(16), WithMaxBufSize(24))

	err := sb.expand(16)
	require.NoError(t, err)
	require.Equal(t, 24, len(sb.buf))
}

func TestScanBufferCompactPartial(t *testing.T) {
	sb := NewScanBuffer(WithStartBufSize(32))

	copy(sb.buf, "abc123")
	sb.compact(6, 3)

	require.Equal(t, 3, sb.offset)
	require.Equal(t, "123", string(sb.buf[0:3]))
}

func TestScanBufferCompactAllConsumed(t *testing.T) {
	sb := NewScanBuffer(WithStartBufSize(32))

	copy(sb.buf, "abc")
	sb.offset = 3
	sb.compact(3, 3)

	require.Equal(t, 0, sb.offset)
}

func TestScanBufferScanNewlinesWithMultipleLines(t *testing.T) {
	lines := []string{}
	cb := func(data []byte) error {
		lines = append(lines, string(data))
		return nil
	}
	sb := NewScanBuffer(WithStartBufSize(32))

	copy(sb.buf, "a\nb\nc")
	k, err := sb.scanNewlines(5, cb)

	require.Equal(t, 4, k)
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b"}, lines)
	require.Equal(t, 0, sb.offset)
}

func TestScanBufferCBError(t *testing.T) {
	cb := func(data []byte) error {
		return io.ErrClosedPipe
	}
	sb := NewScanBuffer(WithStartBufSize(32))

	copy(sb.buf, "no-newline")
	sb.offset = 10
	err := sb.flushTrailingLine(cb)
	require.ErrorIs(t, err, io.ErrClosedPipe)
}

func TestScanBufferScan(t *testing.T) {
	lines := []string{}
	cb := func(data []byte) error {
		lines = append(lines, string(data))
		return nil
	}
	r := strings.NewReader("a\nb\nc")
	err := Scan(r, cb, WithStartBufSize(16))
	require.ErrorIs(t, err, io.EOF)
	require.Equal(t, []string{"a", "b", "c"}, lines)
}

func TestScanBufferScanFileLongLine(t *testing.T) {
	lines := []string{}
	cb := func(data []byte) error {
		lines = append(lines, string(data))
		return nil
	}

	longLine := strings.Repeat("A", 100) + "\n"
	r := strings.NewReader(longLine)
	err := Scan(r, cb, WithStartBufSize(16), WithMaxBufSize(256))
	require.ErrorIs(t, err, io.EOF)
	require.Equal(t, []string{strings.TrimSuffix(longLine, "\n")}, lines)
}

func testFileBuilder(b testing.TB, count int) (*os.File, error) {
	dir := b.TempDir()
	filePath := filepath.Join(dir, "testfile.txt")
	file, err := os.Create(filePath)
	if err != nil {
		return nil, err
	}

	for _, f := range radixInput(count, "response_time") {
		_, err := fmt.Fprintf(file, "%.3f\n", f)
		if err != nil {
			_ = file.Close()
			return nil, err
		}
	}
	return file, nil
}

func BenchmarkScan_bufio(b *testing.B) {
	file, err := testFileBuilder(b, 10000)
	if err != nil {
		b.Fatal(err)
	}
	defer file.Close()
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		_, _ = file.Seek(0, io.SeekStart)
		scanner := bufio.NewScanner(file)
		total := 0
		for scanner.Scan() {
			b := scanner.Bytes()
			total += len(b)
		}
		if err := scanner.Err(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkScan_BufferScan(b *testing.B) {

	file, err := testFileBuilder(b, 10000)
	if err != nil {
		b.Fatal(err)
	}
	defer file.Close()
	b.ResetTimer()
	b.ReportAllocs()
	total := 0
	cb := func(data []byte) error {
		total += len(data)
		return nil
	}
	for b.Loop() {
		_, _ = file.Seek(0, io.SeekStart)
		total = 0
		err := Scan(file, cb, WithStartBufSize(4096), WithMaxBufSize(64*1024))
		if err != nil && !errors.Is(err, io.EOF) {
			b.Fatal(err)
		}
	}
}
