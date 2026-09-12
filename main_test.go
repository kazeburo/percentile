package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type testReader struct {
	input    *strings.Reader
	close    context.CancelFunc
	closed   chan struct{}
	readDone chan struct{}
}

func (r *testReader) Read(p []byte) (int, error) {
	if r.input.Len() > 0 {
		return r.input.Read(p)
	}
	r.close()
	<-r.closed
	close(r.readDone)
	return 0, io.ErrClosedPipe
}

func (r *testReader) Close() error {
	close(r.closed)
	return nil
}

func TestTallyingInterruptWhileWaitingForInput(t *testing.T) {
	for _, input := range []string{"1.5\n2.5\n3.0\n", ""} {
		t.Run(input, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r := &testReader{
				input:    strings.NewReader(input),
				close:    cancel,
				closed:   make(chan struct{}),
				readDone: make(chan struct{}),
			}
			o := &Opt{input: r}
			result := make(chan []float64, 1)
			go func() { result <- o.tallyingContext(ctx) }()
			select {
			case got := <-result:
				select {
				case <-r.closed:
				default:
					t.Error("input was not closed after cancellation")
				}
				select {
				case <-r.readDone:
				default:
					t.Error("read is still blocked after cancellation")
				}
				if input == "" {
					require.Empty(t, got)
				} else {
					require.Equal(t, []float64{1.5, 2.5, 3.0}, got)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("SIGINT did not interrupt the blocked read")
			}
		})
	}
}

func TestTallyingLargeInput(t *testing.T) {
	const count = 100000
	var input strings.Builder
	for i := 0; i < count; i++ {
		fmt.Fprintln(&input, i)
	}
	o := &Opt{input: io.NopCloser(strings.NewReader(input.String()))}
	got := o.tallyingContext(context.Background())
	require.Len(t, got, count)
	for i, value := range got {
		if value != float64(i) {
			t.Fatalf("value[%d]: expected %d, got %v", i, i, value)
		}
	}
}

type closeCountingReader struct {
	io.Reader
	cancelOnEOF context.CancelFunc
	closeCalls  int
}

func (r *closeCountingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == io.EOF && r.cancelOnEOF != nil {
		r.cancelOnEOF()
	}
	return n, err
}

func (r *closeCountingReader) Close() error {
	r.closeCalls++
	return nil
}

func TestTallyingClosesInputOnce(t *testing.T) {
	for _, mode := range []string{"normal", "already canceled", "cancel at EOF"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r := &closeCountingReader{Reader: strings.NewReader("1\n2\n")}
			if mode == "already canceled" {
				cancel()
			} else if mode == "cancel at EOF" {
				r.cancelOnEOF = cancel
			}
			o := &Opt{input: r}
			got := o.tallyingContext(ctx)
			if mode == "already canceled" {
				require.Empty(t, got)
			} else {
				require.Equal(t, []float64{1, 2}, got)
			}
			require.Equal(t, 1, r.closeCalls)
		})
	}
}

func BenchmarkTallying(b *testing.B) {
	input := strings.Repeat("123.456\n", 10000)
	b.ReportAllocs()
	b.SetBytes(int64(len(input)))
	for b.Loop() {
		o := &Opt{input: io.NopCloser(strings.NewReader(input))}
		if got := o.tallyingContext(context.Background()); len(got) != 10000 {
			b.Fatalf("expected 10000 values, got %d", len(got))
		}
	}
}

func mustParsePercentileSet(t *testing.T, s string) []percentile {
	t.Helper()
	percentiles, err := parsePercentileSet(s)
	if err != nil {
		t.Fatalf("failed to parse percentile set %q: %v", s, err)
	}
	return percentiles
}

func TestTallying(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []float64
	}{
		{
			name:     "valid values",
			input:    "1.5\n2.5\n3.0\n",
			expected: []float64{1.5, 2.5, 3.0},
		},
		{
			name:     "ignores invalid lines",
			input:    "1.5\nfoo:bar\n2.5\n",
			expected: []float64{1.5, 2.5},
		},
		{
			name:     "empty input",
			input:    "",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &Opt{
				input: io.NopCloser(strings.NewReader(tt.input)),
			}
			got := o.tallying()
			if len(got) != len(tt.expected) {
				t.Fatalf("expected %v, got %v", tt.expected, got)
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("expected %v, got %v", tt.expected, got)
					break
				}
			}
		})
	}
}

type testJSONOutput struct {
	Count int     `json:"count"`
	Max   float64 `json:"max"`
	Min   float64 `json:"min"`
	Avg   float64 `json:"avg"`
	P75   float64 `json:"75pt"`
	P90   float64 `json:"90pt"`
	P95   float64 `json:"95pt"`
	P99   float64 `json:"99pt"`
}

func TestDisplayJSONPercentiles(t *testing.T) {
	o := &Opt{
		ptSet: mustParsePercentileSet(t, "99,95,90,75"),
	}

	floats := []float64{1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0, 9.0, 10.0}
	output, err := o.displayJSONPercentiles(floats)
	if err != nil {
		t.Fatalf("displayJSONPercentiles returned error: %v", err)
	}
	// Expected JSON output
	if len(output) == 0 {
		t.Fatalf("displayJSONPercentiles returned empty output")
	}
	r := &testJSONOutput{}
	err = json.Unmarshal([]byte(output), r)
	if err != nil {
		t.Fatalf("failed to unmarshal JSON output: %v", err)
	}
	// Deep comparison of the unmarshaled JSON with expected values
	require.Equal(t, &testJSONOutput{
		Count: 10,
		Max:   10,
		Min:   1,
		Avg:   5.5,
		P75:   7.75,
		P90:   9.1,
		P95:   9.549999999999999,
		P99:   9.91,
	}, r)
}

func TestDisplayPercentiles(t *testing.T) {
	o := &Opt{
		ptSet: mustParsePercentileSet(t, "99,95,90,75"),
	}

	floats := []float64{1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0, 9.0, 10.0}
	output, err := o.displayPercentiles(floats)
	if err != nil {
		t.Fatalf("displayPercentiles returned error: %v", err)
	}
	expected := `count: 10
max: 10.0000
min: 1.0000
avg: 5.5000
99pt: 9.9100
95pt: 9.5500
90pt: 9.1000
75pt: 7.7500
`
	require.Equal(t, expected, output)
}

func TestDisplayPercentilesEmpty(t *testing.T) {
	o := &Opt{
		ptSet: mustParsePercentileSet(t, "99,95,90,75"),
	}

	_, err := o.displayPercentiles([]float64{})
	if err == nil {
		t.Fatal("expected error for empty input, got nil")
	}
}
