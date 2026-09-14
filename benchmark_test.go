package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
)

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
		if got := o.tallyingContext(context.Background()); len(got.points) != count {
			b.Fatalf("expected %d values, got %d", count, len(got.points))
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
		values := o.tallyingContext(ctx)
		if len(values.points) != count {
			b.Fatalf("expected %d values, got %d", count, len(values.points))
		}
		sorted, err := values.Sorted()
		if err != nil {
			b.Fatal(err)
		}
		o.displayPercentiles(sorted)
	}
}

/*
func BenchmarkSliceSort(b *testing.B) {
	points := radixInput(100000, "random")

	b.ResetTimer()
	for b.Loop() {
		b.StopTimer()
		rand.Shuffle(len(points), func(i, j int) { points[i], points[j] = points[j], points[i] })
		b.StartTimer()
		slices.Sort(points)
	}
}

func BenchmarkRadixSort(b *testing.B) {
	points := radixInput(100000, "random")

	b.ResetTimer()
	for b.Loop() {
		b.StopTimer()
		rand.Shuffle(len(points), func(i, j int) { points[i], points[j] = points[j], points[i] })
		b.StartTimer()
		radixSort(points)
	}
}
*/

func BenchmarkSortDistributions(b *testing.B) {
	for _, algorithm := range []struct {
		name string
		sort func([]float64) []float64
	}{
		{"slices", aliasSlicesSort}, {"radix", radixSort},
	} {
		for _, n := range []int{1000, 10000, 100000} {
			for _, distribution := range []string{"duplicates", "random", "wide", "sorted", "response_time"} {
				{
					b.Run(fmt.Sprintf("%s/%d/%s", algorithm.name, n, distribution), func(b *testing.B) {
						b.ReportAllocs()
						for b.Loop() {
							b.StopTimer()
							points := radixInput(n, distribution)
							b.StartTimer()
							algorithm.sort(points)
						}
					})
				}
			}
		}
	}
}
