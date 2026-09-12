package main

import (
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"strings"
	"testing"
)

func testInputBuilder(count int) string {
	r := rand.New(rand.NewPCG(1, 2))
	var inputBuilder strings.Builder
	for range count {
		fmt.Fprintf(&inputBuilder, "%.2f\n", float64(r.IntN(1000000))/100)
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
		if got := o.tallyingContext(context.Background()); len(got) != count {
			b.Fatalf("expected %d values, got %d", count, len(got))
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.ReportAllocs()
	b.SetBytes(int64(len(input)))
	for b.Loop() {
		o := &Opt{ptSet: ps, input: io.NopCloser(strings.NewReader(input))}
		values := o.tallyingContext(ctx)
		if len(values) != count {
			b.Fatalf("expected %d values, got %d", count, len(values))
		}
		if _, err := o.displayPercentiles(values); err != nil {
			b.Fatal(err)
		}
	}
}
