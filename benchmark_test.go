package main

import (
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
)

func sliceSort(points []float64) []float64 {
	sortedPoints := make([]float64, len(points))
	copy(sortedPoints, points)
	slices.Sort(sortedPoints)
	return sortedPoints
}

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

func BenchmarkSliceSort(b *testing.B) {
	r := rand.New(rand.NewPCG(1, 2))
	points := make([]float64, 100000)
	for i := range points {
		points[i] = float64(i%100) + 0.5
	}

	b.ResetTimer()
	for b.Loop() {
		r.Shuffle(len(points), func(i, j int) { points[i], points[j] = points[j], points[i] })
		sliceSort(points)
	}
}

func BenchmarkRadixSort(b *testing.B) {
	r := rand.New(rand.NewPCG(1, 2))
	points := make([]float64, 100000)
	for i := range points {
		points[i] = float64(i%100) + 0.5
	}

	b.ResetTimer()
	for b.Loop() {
		r.Shuffle(len(points), func(i, j int) { points[i], points[j] = points[j], points[i] })
		radixSort(points)
	}
}

func BenchmarkSortDistributions(b *testing.B) {
	for _, n := range []int{128, 1000, 100000} {
		for _, distribution := range []string{"duplicates", "random", "wide", "sorted"} {
			for _, algorithm := range []struct {
				name string
				sort func([]float64) []float64
			}{
				{"slices", sliceSort}, {"radix", radixSort},
			} {
				b.Run(fmt.Sprintf("%s/%d/%s", distribution, n, algorithm.name), func(b *testing.B) {
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
