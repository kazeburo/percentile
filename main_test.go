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

func inputTestReader(t *testing.T, input string) {
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
}

func TestTallyingInterruptWhileWaitingForInput(t *testing.T) {
	for _, input := range []string{"1.5\n2.5\n3.0\n", ""} {
		t.Run(input, func(t *testing.T) {
			inputTestReader(t, input)
		})
	}
}

func TestTallyingLargeInput(t *testing.T) {
	const count = 100000
	var input strings.Builder
	for i := range count {
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
			switch mode {
			case "already canceled":
				cancel()
			case "cancel at EOF":
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

func mustParsePercentileSet(t *testing.T, s string) []percentile {
	t.Helper()
	percentiles, err := parsePercentileSet(s)
	if err != nil {
		t.Fatalf("failed to parse percentile set %q: %v", s, err)
	}
	return percentiles
}

func TestParsePercentileSet(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []percentile
		wantErr bool
	}{
		{
			name:  "default percentile set",
			input: "99,95,90,75",
			want: []percentile{
				{str: "99", float: 99},
				{str: "95", float: 95},
				{str: "90", float: 90},
				{str: "75", float: 75},
			},
		},
		{
			name:  "single value",
			input: "50",
			want:  []percentile{{str: "50", float: 50}},
		},
		{
			name:  "decimal value",
			input: "99.9",
			want:  []percentile{{str: "99.9", float: 99.9}},
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "invalid value",
			input:   "foo",
			wantErr: true,
		},
		{
			name:    "negative value",
			input:   "-1",
			wantErr: true,
		},
		{
			name:    "value over 100",
			input:   "101",
			wantErr: true,
		},
		{
			name:    "NaN",
			input:   "NaN",
			wantErr: true,
		},
		{
			name:    "positive infinity",
			input:   "+Inf",
			wantErr: true,
		},
		{
			name:    "negative infinity",
			input:   "-Inf",
			wantErr: true,
		},
		{
			name:    "infinity",
			input:   "Infinity",
			wantErr: true,
		},
		{
			name:    "mixed valid and invalid",
			input:   "50,foo",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePercentileSet(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestCalculatePercentiles(t *testing.T) {
	tests := []struct {
		name    string
		floats  []float64
		ptSet   string
		wantMin float64
		wantMax float64
		wantAvg float64
		wantPts []float64
		wantErr bool
	}{
		{
			name:    "basic values with default percentiles",
			floats:  []float64{8.0, 2.0, 10.0, 4.0, 6.0, 3.0, 7.0, 1.0, 9.0, 5.0},
			ptSet:   "99,95,90,75",
			wantMin: 1,
			wantMax: 10,
			wantAvg: 5.5,
			wantPts: []float64{9.91, 9.549999999999999, 9.1, 7.75},
		},
		{
			name:    "single value",
			floats:  []float64{42.0},
			ptSet:   "50",
			wantMin: 42,
			wantMax: 42,
			wantAvg: 42,
			wantPts: []float64{42},
		},
		{
			name:    "two values",
			floats:  []float64{1.0, 3.0},
			ptSet:   "50",
			wantMin: 1,
			wantMax: 3,
			wantAvg: 2,
			wantPts: []float64{2},
		},
		{
			name:    "empty percentile set",
			floats:  []float64{1.0, 2.0, 3.0},
			ptSet:   "",
			wantMin: 1,
			wantMax: 3,
			wantAvg: 2,
			wantPts: []float64{},
		},
		{
			name:    "unsorted input",
			floats:  []float64{5.0, 1.0, 3.0, 2.0, 4.0},
			ptSet:   "0,100,50",
			wantMin: 1,
			wantMax: 5,
			wantAvg: 3,
			wantPts: []float64{1, 5, 3},
		},
		{
			name:    "decimal percentiles",
			floats:  []float64{1.0, 2.0, 3.0, 4.0, 5.0},
			ptSet:   "12.5,87.5",
			wantMin: 1,
			wantMax: 5,
			wantAvg: 3,
			wantPts: []float64{1.5, 4.5},
		},
		{
			name:    "duplicate values",
			floats:  []float64{5.0, 5.0, 5.0, 5.0},
			ptSet:   "25,50,75",
			wantMin: 5,
			wantMax: 5,
			wantAvg: 5,
			wantPts: []float64{5, 5, 5},
		},
		{
			name:    "negative values",
			floats:  []float64{-5.0, -1.0, -3.0},
			ptSet:   "50",
			wantMin: -5,
			wantMax: -1,
			wantAvg: -3,
			wantPts: []float64{-3},
		},
		{
			name:    "empty input",
			floats:  []float64{},
			ptSet:   "50",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &Opt{}
			if tt.ptSet != "" {
				o.ptSet = mustParsePercentileSet(t, tt.ptSet)
			}
			min, max, avg, pts, err := o.calculatePercentiles(tt.floats)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.InDelta(t, tt.wantMin, min, 1e-9)
			require.InDelta(t, tt.wantMax, max, 1e-9)
			require.InDelta(t, tt.wantAvg, avg, 1e-9)
			require.Len(t, pts, len(tt.wantPts))
			for i := range pts {
				require.InDelta(t, tt.wantPts[i], pts[i], 1e-9)
			}
		})
	}
}

func TestParseFloat(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    float64
		wantErr bool
	}{
		{
			name:  "plain integer",
			input: "42",
			want:  42,
		},
		{
			name:  "plain decimal",
			input: "3.14",
			want:  3.14,
		},
		{
			name:  "ltsv value part",
			input: "1.5",
			want:  1.5,
		},
		{
			name:  "negative value",
			input: "-1.5",
			want:  -1.5,
		},
		{
			name:    "invalid value",
			input:   "foo:bar",
			wantErr: true,
		},
		{
			name:    "NaN",
			input:   "NaN",
			wantErr: true,
		},
		{
			name:    "positive infinity",
			input:   "+Inf",
			wantErr: true,
		},
		{
			name:    "negative infinity",
			input:   "-Inf",
			wantErr: true,
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseFloat([]byte(tt.input))
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.InDelta(t, tt.want, got, 1e-9)
		})
	}
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

	floats := []float64{8.0, 2.0, 10.0, 4.0, 6.0, 3.0, 7.0, 1.0, 9.0, 5.0}
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

	floats := []float64{8.0, 2.0, 10.0, 4.0, 6.0, 3.0, 7.0, 1.0, 9.0, 5.0}
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
