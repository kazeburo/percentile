package main

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStatsAppend(t *testing.T) {
	t.Run("valid points", func(t *testing.T) {
		s := NewStats()
		require.NoError(t, s.Append(1.5, 2.5, 3.0))
		require.Equal(t, []float64{1.5, 2.5, 3.0}, s.points)
		require.False(t, s.hasNegative)
	})

	t.Run("negative points", func(t *testing.T) {
		s := NewStats()
		require.NoError(t, s.Append(-1.5, 2.5))
		require.Equal(t, []float64{-1.5, 2.5}, s.points)
		require.True(t, s.hasNegative)
	})

	t.Run("rejects NaN", func(t *testing.T) {
		s := NewStats()
		err := s.Append(math.NaN())
		require.Error(t, err)
	})

	t.Run("accepts negative zero using comparison sort", func(t *testing.T) {
		s := NewStats()
		err := s.Append(math.Copysign(0, -1))
		require.NoError(t, err)
		require.True(t, s.hasNegative)
	})
}

func TestStatsSorted(t *testing.T) {
	t.Run("sorts non-negative points with radix sort", func(t *testing.T) {
		s := NewStats()
		require.NoError(t, s.Append(3.0, 1.0, 2.0))
		sorted, err := s.Sorted()
		require.NoError(t, err)
		require.Equal(t, []float64{1.0, 2.0, 3.0}, sorted.points)
	})

	t.Run("sorts negative points with slice sort", func(t *testing.T) {
		s := NewStats()
		require.NoError(t, s.Append(-3.0, -1.0, -2.0))
		sorted, err := s.Sorted()
		require.NoError(t, err)
		require.Equal(t, []float64{-3.0, -2.0, -1.0}, sorted.points)
	})

	t.Run("nil receiver", func(t *testing.T) {
		var s *Stats
		_, err := s.Sorted()
		require.Error(t, err)
	})

	t.Run("empty points", func(t *testing.T) {
		s := NewStats()
		_, err := s.Sorted()
		require.Error(t, err)
	})
}

func TestStatsSummary(t *testing.T) {
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
			data := NewStats()
			require.NoError(t, data.Append(tt.floats...))
			sorted, err := data.Sorted()
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.InDelta(t, tt.wantMin, sorted.Min(), 1e-9)
			require.InDelta(t, tt.wantMax, sorted.Max(), 1e-9)
			require.InDelta(t, tt.wantAvg, sorted.Mean(), 1e-9)
			require.Len(t, o.ptSet, len(tt.wantPts))
			for i, ps := range o.ptSet {
				require.InDelta(t, tt.wantPts[i], sorted.Percentile(ps.float), 1e-9)
			}
		})
	}
}

func TestStatsMeanPreservesInputOrder(t *testing.T) {
	data := NewStats()
	require.NoError(t, data.Append(1e16, 1, -1e16))
	sorted, err := data.Sorted()
	require.NoError(t, err)
	require.Equal(t, 0.0, sorted.Mean())
	require.Error(t, data.Append(-2, math.NaN()))
	sorted, err = data.Sorted()
	require.NoError(t, err)
	require.Equal(t, 3, sorted.Count())
	require.Equal(t, 0.0, sorted.Mean())
}

func TestStatsFrozenAppend(t *testing.T) {
	s := NewStats()
	require.NoError(t, s.Append(3.0, 1.0, 2.0))
	sorted, err := s.Sorted()
	require.NoError(t, err)
	require.Equal(t, []float64{1.0, 2.0, 3.0}, sorted.points)

	// Appending after Sorted must not mutate the already-sorted slice.
	require.NoError(t, s.Append(0.5))
	require.Equal(t, []float64{1.0, 2.0, 3.0}, sorted.points)

	sorted2, err := s.Sorted()
	require.NoError(t, err)
	require.Equal(t, []float64{0.5, 1.0, 2.0, 3.0}, sorted2.points)
}

func TestSortedCount(t *testing.T) {
	s := &Sorted{points: []float64{1.0, 2.0, 3.0}}
	require.Equal(t, 3, s.Count())

	s = &Sorted{points: []float64{}}
	require.Equal(t, 0, s.Count())
}

func TestSortedMin(t *testing.T) {
	t.Run("non-empty", func(t *testing.T) {
		s := &Sorted{points: []float64{1.0, 2.0, 3.0}}
		require.InDelta(t, 1.0, s.Min(), 1e-9)
	})

	t.Run("empty", func(t *testing.T) {
		s := &Sorted{points: []float64{}}
		require.InDelta(t, 0.0, s.Min(), 1e-9)
	})
}

func TestSortedMax(t *testing.T) {
	t.Run("non-empty", func(t *testing.T) {
		s := &Sorted{points: []float64{1.0, 2.0, 3.0}}
		require.InDelta(t, 3.0, s.Max(), 1e-9)
	})

	t.Run("empty", func(t *testing.T) {
		s := &Sorted{points: []float64{}}
		require.InDelta(t, 0.0, s.Max(), 1e-9)
	})
}

func TestSortedMean(t *testing.T) {
	t.Run("non-empty", func(t *testing.T) {
		s := &Sorted{points: []float64{1.0, 2.0, 3.0, 4.0}}
		require.InDelta(t, 2.5, s.Mean(), 1e-9)
	})

	t.Run("empty", func(t *testing.T) {
		s := &Sorted{points: []float64{}}
		require.InDelta(t, 0.0, s.Mean(), 1e-9)
	})
}

func TestSortedMedian(t *testing.T) {
	tests := []struct {
		name string
		pts  []float64
		want float64
	}{
		{
			name: "odd",
			pts:  []float64{1.0, 2.0, 3.0},
			want: 2.0,
		},
		{
			name: "even",
			pts:  []float64{1.0, 2.0, 3.0, 4.0},
			want: 2.5,
		},
		{
			name: "single",
			pts:  []float64{5.0},
			want: 5.0,
		},
		{
			name: "empty",
			pts:  []float64{},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Sorted{points: tt.pts}
			require.InDelta(t, tt.want, s.Median(), 1e-9)
		})
	}
}

func TestSortedPercentile(t *testing.T) {
	tests := []struct {
		name string
		pts  []float64
		p    float64
		want float64
	}{
		{
			name: "0th percentile",
			pts:  []float64{1.0, 2.0, 3.0, 4.0, 5.0},
			p:    0,
			want: 1.0,
		},
		{
			name: "100th percentile",
			pts:  []float64{1.0, 2.0, 3.0, 4.0, 5.0},
			p:    100,
			want: 5.0,
		},
		{
			name: "50th percentile odd",
			pts:  []float64{1.0, 2.0, 3.0, 4.0, 5.0},
			p:    50,
			want: 3.0,
		},
		{
			name: "50th percentile even",
			pts:  []float64{1.0, 2.0, 3.0, 4.0},
			p:    50,
			want: 2.5,
		},
		{
			name: "out of range negative",
			pts:  []float64{1.0, 2.0, 3.0},
			p:    -1,
			want: 0,
		},
		{
			name: "out of range positive",
			pts:  []float64{1.0, 2.0, 3.0},
			p:    101,
			want: 0,
		},
		{
			name: "empty",
			pts:  []float64{},
			p:    50,
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Sorted{points: tt.pts}
			require.InDelta(t, tt.want, s.Percentile(tt.p), 1e-9)
		})
	}
}

func TestInfinityPercentiles(t *testing.T) {
	tests := []struct {
		input   []float64
		percent float64
		want    float64
	}{
		{[]float64{1, math.Inf(1)}, 0, 1},
		{[]float64{1, math.Inf(1)}, 50, math.Inf(1)},
		{[]float64{math.Inf(-1), 1}, 50, math.Inf(-1)},
		{[]float64{math.Inf(1), math.Inf(1)}, 50, math.Inf(1)},
		{[]float64{math.Inf(-1), math.Inf(-1)}, 50, math.Inf(-1)},
		{[]float64{math.Inf(-1), math.Inf(1)}, 50, math.NaN()},
		{[]float64{math.Inf(-1), math.Inf(1)}, 100, math.Inf(1)},
	}
	for _, tt := range tests {
		data := NewStats()
		require.NoError(t, data.Append(tt.input...))
		sorted, err := data.Sorted()
		require.NoError(t, err)
		got := sorted.Percentile(tt.percent)
		require.True(t, got == tt.want || math.IsNaN(got) && math.IsNaN(tt.want), "got %v want %v", got, tt.want)
	}
}

func TestRadixSort(t *testing.T) {
	tests := []struct {
		name  string
		input []float64
		want  []float64
	}{
		{
			name:  "basic",
			input: []float64{3.0, 1.0, 2.0},
			want:  []float64{1.0, 2.0, 3.0},
		},
		{
			name:  "with decimals",
			input: []float64{3.14, 1.59, 2.65},
			want:  []float64{1.59, 2.65, 3.14},
		},
		{
			name:  "duplicates",
			input: []float64{5.0, 5.0, 1.0, 1.0},
			want:  []float64{1.0, 1.0, 5.0, 5.0},
		},
		{
			name:  "empty",
			input: []float64{},
			want:  []float64{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := slices.Clone(tt.input)
			got := radixSort(input)
			require.Equal(t, tt.want, got)
		})
	}
}

func aliasSlicesSort(points []float64) []float64 {
	slices.Sort(points)
	return points
}

func TestRadixSortMatchesSlices(t *testing.T) {
	for _, n := range []int{0, 1, 127, 128, 129, 511, 512, 513, 1000, 10000} {
		for _, distribution := range []string{"duplicates", "random", "wide", "sorted", "equal"} {
			t.Run(fmt.Sprintf("%s/%d", distribution, n), func(t *testing.T) {
				points := radixInput(n, distribution)
				want := aliasSlicesSort(slices.Clone(points))
				got := radixSort(slices.Clone(points))
				require.Equal(t, want, got)
			})
		}
	}
}

func TestRadixSortExtremeValues(t *testing.T) {
	points := make([]float64, 1024)
	extremes := []float64{math.Inf(1), math.MaxFloat64, math.SmallestNonzeroFloat64, 0, 1}
	for i := range points {
		points[i] = extremes[i%len(extremes)]
	}
	require.Equal(t, aliasSlicesSort(points), radixSort(points))
}

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

func mustSorted(t *testing.T, values []float64) *Sorted {
	t.Helper()
	data := NewStats()
	require.NoError(t, data.Append(values...))
	sorted, err := data.Sorted()
	require.NoError(t, err)
	return sorted
}
