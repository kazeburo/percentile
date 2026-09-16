package main

import (
	"context"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"strings"
	"testing"
)

func radixInput(n int, distribution string) []float64 {
	r := rand.New(rand.NewPCG(1, 2))
	points := make([]float64, n)
	for i := range points {
		switch distribution {
		case "duplicates":
			points[i] = float64(i%100) + 0.5
		case "random":
			points[i] = r.Float64() * 10000
		case "wide":
			points[i] = math.Float64frombits(r.Uint64() & 0x7fefffffffffffff)
		case "sorted":
			points[i] = float64(i)
		case "equal":
			points[i] = 42
		case "response_time":
			points[i] = float64(r.IntN(500)) / 1000
		}
	}
	return points
}

func testInputBuilder(count int) string {
	var inputBuilder strings.Builder
	for _, f := range radixInput(count, "response_time") {
		fmt.Fprintf(&inputBuilder, "%.3f\n", f)
	}
	return inputBuilder.String()
}

func BenchmarkPercentile_Tallying(b *testing.B) {
	const count = 100000
	input := testInputBuilder(count)
	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(int64(len(input)))
	for b.Loop() {
		o := &Opt{input: io.NopCloser(strings.NewReader(input))}
		sampdo := o.tallyingContext(context.Background())
		sorted, err := sampdo.Sorted()
		if err != nil {
			b.Fatal(err)
		}
		if sorted.Count() != count {
			b.Fatalf("expected %d values, got %d", count, sorted.Count())
		}
	}
}

func BenchmarkPercentile_Full(b *testing.B) {
	// Generate reproducible, unsorted input outside the timed loop.
	const count = 100000
	input := testInputBuilder(count)
	ps, err := parsePercentileSet("99,95,90,75")
	if err != nil {
		b.Fatal(err)
	}
	ctx, cancel := context.WithCancel(b.Context())
	defer cancel()
	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(int64(len(input)))
	for b.Loop() {
		o := &Opt{ptSet: ps, input: io.NopCloser(strings.NewReader(input))}
		sampdo := o.tallyingContext(ctx)
		sorted, err := sampdo.Sorted()
		if err != nil {
			b.Fatal(err)
		}
		if sorted.Count() != count {
			b.Fatalf("expected %d values, got %d", count, sorted.Count())
		}
		_, _ = o.displayPercentiles(sorted)
	}
}
