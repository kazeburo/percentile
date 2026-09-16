package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/monitoring-forge/sampdo"
	"github.com/stretchr/testify/require"
)

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
			sorted, err := o.tallying().Sorted()
			if tt.expected == nil {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, len(tt.expected), sorted.Count())
			for i, want := range tt.expected {
				got, err := getSortedPoint(sorted, i)
				require.NoError(t, err)
				require.Equal(t, want, got)
			}
		})
	}
}

func getSortedPoint(sorted *sampdo.Sorted, i int) (float64, error) {
	if sorted.Count() == 1 {
		return sorted.Percentile(0)
	}
	return sorted.Percentile(float64(i) / float64(sorted.Count()-1) * 100)
}

func TestTallyingAcceptsInfinity(t *testing.T) {
	o := &Opt{input: io.NopCloser(strings.NewReader("Infinity\n-Infinity\nNaN\n1\n"))}
	sorted, err := o.tallyingContext(context.Background()).Sorted()
	require.NoError(t, err)
	require.Equal(t, 3, sorted.Count())
	min, err := sorted.Min()
	require.NoError(t, err)
	require.Equal(t, math.Inf(-1), min)
	max, err := sorted.Max()
	require.NoError(t, err)
	require.Equal(t, math.Inf(1), max)
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
	sorted, err := o.tallyingContext(context.Background()).Sorted()
	require.NoError(t, err)
	require.Equal(t, count, sorted.Count())
	for i := range count {
		got, err := getSortedPoint(sorted, i)
		require.NoError(t, err)
		require.InDelta(t, float64(i), got, 1e-9)
	}
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
			sorted, err := o.tallyingContext(ctx).Sorted()
			if mode == "already canceled" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, 2, sorted.Count())
				for i, want := range []float64{1, 2} {
					got, err := getSortedPoint(sorted, i)
					require.NoError(t, err)
					require.Equal(t, want, got)
				}
			}
			require.Equal(t, 1, r.closeCalls)
		})
	}
}

func TestDisplayPercentiles(t *testing.T) {
	o := &Opt{
		ptSet: mustParsePercentileSet(t, "99,95,90,75"),
	}

	floats := []float64{8.0, 2.0, 10.0, 4.0, 6.0, 3.0, 7.0, 1.0, 9.0, 5.0}
	output, err := o.displayPercentiles(mustSorted(t, floats))
	require.NoError(t, err)
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
	output, err := o.displayJSONPercentiles(mustSorted(t, floats))
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

func TestCLIJSONNonFiniteValues(t *testing.T) {
	o := &Opt{ptSet: mustParsePercentileSet(t, "0,50,100")}
	output, err := o.displayJSONPercentiles(mustSorted(t, []float64{math.Inf(-1), math.Inf(1)}))
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(output), &got))
	require.Equal(t, map[string]any{"count": float64(2), "min": "-Inf", "max": "+Inf", "avg": "NaN", "0pt": "-Inf", "50pt": "NaN", "100pt": "+Inf"}, got)
	text, err := o.displayPercentiles(mustSorted(t, []float64{1, math.Inf(1)}))
	require.NoError(t, err)
	require.Contains(t, text, "max: +Inf")
	require.Contains(t, text, "0pt: 1.0000")
}

func TestCLIInfinity(t *testing.T) {
	for _, input := range []string{"Infinity\n1\n+Inf\n", "-Infinity\n1\n-Infinity\n", "-Inf\nInfinity\n"} {
		t.Run(input, func(t *testing.T) {
			o := &Opt{Output: "json", ptSet: mustParsePercentileSet(t, "0,50,100"), input: io.NopCloser(strings.NewReader(input))}
			output, code := o.Run(nil)
			require.Zero(t, code)
			var got map[string]any
			require.NoError(t, json.Unmarshal([]byte(output.(string)), &got))
			require.Equal(t, float64(strings.Count(input, "\n")), got["count"])
			require.NotNil(t, got["avg"])
		})
	}
}

func TestRunEmptyInput(t *testing.T) {
	o := &Opt{input: io.NopCloser(strings.NewReader(""))}
	_, code := o.Run(nil)
	require.NotZero(t, code)
}

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
	result := make(chan *sampdo.Sampdo, 1)
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
			_, err := got.Sorted()
			require.Error(t, err)
		} else {
			sorted, err := got.Sorted()
			require.NoError(t, err)
			require.Equal(t, 3, sorted.Count())
			for i, want := range []float64{1.5, 2.5, 3.0} {
				got, err := getSortedPoint(sorted, i)
				require.NoError(t, err)
				require.Equal(t, want, got)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SIGINT did not interrupt the blocked read")
	}
}

type closeCountingReader struct {
	io.Reader
	cancelOnEOF context.CancelFunc
	closeCalls  int
}

func (r *closeCountingReader) Read(p []byte) (int, error) {
	if r.closeCalls > 0 {
		return 0, io.ErrClosedPipe
	}
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

func mustParsePercentileSet(t *testing.T, s string) []percentile {
	t.Helper()
	percentiles, err := parsePercentileSet(s)
	if err != nil {
		t.Fatalf("failed to parse percentile set %q: %v", s, err)
	}
	return percentiles
}

func mustSorted(t *testing.T, values []float64) *sampdo.Sorted {
	t.Helper()
	data := sampdo.New()
	require.NoError(t, data.Append(values...))
	sorted, err := data.Sorted()
	require.NoError(t, err)
	return sorted
}
