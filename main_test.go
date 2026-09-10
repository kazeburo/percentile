package main

import (
	"bufio"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

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
				bufioScanner: bufio.NewScanner(strings.NewReader(tt.input)),
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
