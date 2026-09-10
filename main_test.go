package main

import (
	"bufio"
	"strings"
	"testing"
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

func TestDisplayPercentiles(t *testing.T) {
	o := &Opt{
		ptileSet: mustParsePercentileSet(t, "99,95,90,75"),
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
	if output != expected {
		t.Errorf("unexpected output.\nexpected:\n%s\ngot:\n%s", expected, output)
	}
}

func TestDisplayPercentilesEmpty(t *testing.T) {
	o := &Opt{
		ptileSet: mustParsePercentileSet(t, "99,95,90,75"),
	}

	_, err := o.displayPercentiles([]float64{})
	if err == nil {
		t.Fatal("expected error for empty input, got nil")
	}
}
