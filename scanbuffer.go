package main

import (
	"bytes"
	"errors"
	"io"
)

var ErrTokenTooLong = errors.New("reader: token too long")

type CB func([]byte) error

type ScanBuffer struct {
	StartBufSize int
	MaxBufSize   int
	buf          []byte
	offset       int
}

type Option func(*ScanBuffer)

func WithStartBufSize(size int) Option {
	return func(s *ScanBuffer) {
		s.StartBufSize = size
	}
}

func WithMaxBufSize(size int) Option {
	return func(s *ScanBuffer) {
		s.MaxBufSize = size
	}
}

func Scan(r io.Reader, cb CB, opts ...Option) error {
	s := NewScanBuffer(opts...)
	return s.Scan(r, cb)
}

func NewScanBuffer(opts ...Option) *ScanBuffer {
	s := &ScanBuffer{
		StartBufSize: 4096,
		MaxBufSize:   65536,
		offset:       0,
	}
	for _, opt := range opts {
		opt(s)
	}
	s.buf = make([]byte, s.StartBufSize)
	return s
}

func (s *ScanBuffer) scanInternal(r io.Reader, cb CB) error {
	nRead, eof, err := s.readInto(r)
	if err != nil {
		return err
	}
	if nRead == 0 && eof {
		if err := s.flushTrailingLine(cb); err != nil {
			return err
		}
		return io.EOF
	}
	n := nRead + s.offset
	if err := s.processChunk(n, cb); err != nil {
		return err
	}

	if eof {
		if err := s.flushTrailingLine(cb); err != nil {
			return err
		}
		return io.EOF
	}

	if s.offset == n {
		if expandErr := s.expand(n); expandErr != nil {
			return expandErr
		}
	}
	return nil
}

func callCB(cb CB, line []byte) error {
	l := len(line)
	if l == 0 {
		return nil
	}
	if line[l-1] == '\r' {
		line = line[:l-1]
	}
	if len(line) == 0 {
		return nil
	}
	return cb(line)
}

func (s *ScanBuffer) Scan(r io.Reader, cb CB) error {
	for {
		err := s.scanInternal(r, cb)
		if err != nil && err == io.EOF { //nolint:staticcheck,errorlint
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// flushTrailingLine invokes the callback with any remaining partial line in the buffer.
func (s *ScanBuffer) flushTrailingLine(cb CB) error {
	if s.offset == 0 {
		return nil
	}
	return callCB(cb, s.buf[0:s.offset])
}

// scanNewlines scans the buffer for newline characters and invokes the callback for each complete line. It returns the number of bytes processed and any error encountered.
func (s *ScanBuffer) scanNewlines(n int, cb CB) (int, error) {
	k := 0
	for {
		idx := bytes.IndexByte(s.buf[k:n], '\n')
		if idx < 0 {
			break
		}
		// found newline at k+idx
		if err := callCB(cb, s.buf[k:k+idx]); err != nil {
			return k, err
		}
		k += idx + 1
	}
	return k, nil
}

// compact moves any remaining partial line to the head of the buffer. It updates the offset accordingly.
func (s *ScanBuffer) compact(n, k int) {
	if k < n {
		copy(s.buf[0:], s.buf[k:n])
		s.offset = n - k
	} else {
		s.offset = 0
	}
}

// expand grows the buffer when it is full and contains no newlines. It returns an error if the buffer exceeds the maximum allowed size.
func (s *ScanBuffer) expand(n int) error {
	if n >= s.MaxBufSize {
		return ErrTokenTooLong
	}
	if n == len(s.buf) {
		newSize := len(s.buf) * 2
		newSize = min(newSize, s.MaxBufSize)
		newBuf := make([]byte, newSize)
		copy(newBuf, s.buf)
		s.buf = newBuf
	}
	return nil
}

// readInto performs a single read into the buffer and returns the number of
// bytes read and whether EOF was reached. Non-EOF errors are returned as-is.
func (s *ScanBuffer) readInto(f io.Reader) (int, bool, error) {
	nRead, err := f.Read(s.buf[s.offset:])
	if err == nil {
		return nRead, false, nil
	}
	if err == io.EOF {
		return nRead, true, nil
	}
	return nRead, false, err
}

// processChunk parses all complete lines from the current buffer contents and
// compacts any remaining partial line to the head of the buffer.
func (s *ScanBuffer) processChunk(n int, cb CB) error {
	k, err := s.scanNewlines(n, cb)
	s.compact(n, k)
	return err
}
