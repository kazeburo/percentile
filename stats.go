package main

import (
	"fmt"
	"math"
	"slices"
)

type Stats struct {
	points      []float64
	hasNegative bool
	sum         float64
}

type Sorted struct {
	points   []float64
	sum      float64
	sumValid bool
}

func NewStats() *Stats {
	return &Stats{
		points:      []float64{},
		hasNegative: false,
	}
}

func (t *Stats) Append(point ...float64) error {
	// Keep batch updates atomic while summing in the original input order.
	sum, hasNegative := t.sum, t.hasNegative
	for _, p := range point {
		if math.IsNaN(p) {
			return fmt.Errorf("invalid point: %v", p)
		}
		if math.Signbit(p) {
			hasNegative = true
		}
		sum += p
	}
	t.sum, t.hasNegative = sum, hasNegative
	t.points = append(t.points, point...)
	return nil
}

func (t *Stats) Sorted() (*Sorted, error) {
	if t == nil {
		return nil, fmt.Errorf("sorted is nil")
	}
	if len(t.points) == 0 {
		return nil, fmt.Errorf("no points to sort")
	}
	if t.hasNegative {
		return &Sorted{
			points: sliceSort(t.points),
			sum:    t.sum, sumValid: true,
		}, nil
	}
	// If all points are non-negative, use radix sort
	return &Sorted{
		points: radixSort(t.points),
		sum:    t.sum, sumValid: true,
	}, nil
}

func sliceSort(points []float64) []float64 {
	sortedPoints := make([]float64, len(points))
	copy(sortedPoints, points)
	slices.Sort(sortedPoints)
	return sortedPoints
}

// radixSort accepts non-negative float64 values, excluding NaN and negative zero.
// Stats.Append and Stats.Sorted enforce these preconditions.
func radixSort(points []float64) []float64 {
	// Small inputs do not amortize radix sort's histogram and scratch space.
	if len(points) < 512 {
		return sliceSort(points)
	}

	sorted := make([]float64, len(points))
	first := math.Float64bits(points[0])
	var varying uint64
	ordered := true
	previous := first
	for i, v := range points {
		key := math.Float64bits(v)
		varying |= key ^ first
		ordered = ordered && previous <= key
		previous = key
		sorted[i] = v
	}
	if ordered {
		return sorted
	}

	temp := make([]float64, len(points))
	for shift := uint(0); shift < 64; shift += 8 {
		// A byte shared by every key cannot change their order.
		if (varying>>shift)&0xff == 0 {
			continue
		}
		var count [256]int
		for _, v := range sorted {
			count[(math.Float64bits(v)>>shift)&0xff]++
		}
		sum := 0
		for i, n := range count {
			count[i] = sum
			sum += n
		}
		for _, v := range sorted {
			idx := (math.Float64bits(v) >> shift) & 0xff
			temp[count[idx]] = v
			count[idx]++
		}
		sorted, temp = temp, sorted
	}
	return sorted
}

func (s *Sorted) Count() int {
	return len(s.points)
}

func (s *Sorted) Max() float64 {
	if len(s.points) == 0 {
		return 0
	}
	return s.points[len(s.points)-1]
}

func (s *Sorted) Min() float64 {
	if len(s.points) == 0 {
		return 0
	}
	return s.points[0]
}

func (s *Sorted) Mean() float64 {
	if len(s.points) == 0 {
		return 0
	}
	if s.sumValid {
		return s.sum / float64(len(s.points))
	}
	sum := 0.0
	for _, v := range s.points {
		sum += v
	}
	return sum / float64(len(s.points))
}

func (s *Sorted) Median() float64 {
	if len(s.points) == 0 {
		return 0
	}
	mid := len(s.points) / 2
	if len(s.points)%2 == 0 {
		if math.IsInf(s.points[mid-1], 0) || math.IsInf(s.points[mid], 0) {
			return s.Percentile(50)
		}
		return (s.points[mid-1] + s.points[mid]) / 2
	}
	return s.points[mid]
}

func (s *Sorted) Percentile(p float64) float64 {
	if len(s.points) == 0 {
		return 0
	}
	if math.IsNaN(p) || p < 0 || p > 100 {
		return 0
	}
	k := (p / 100) * float64(len(s.points)-1)
	f := int(k)
	c := f + 1
	if c >= len(s.points) {
		return s.points[f]
	}
	fraction := k - float64(f)
	a, b := s.points[f], s.points[c]
	if fraction == 0 || a == b {
		return a
	}
	if math.IsInf(a, -1) && math.IsInf(b, 1) {
		return math.NaN()
	}
	if math.IsInf(a, 0) {
		return a
	}
	if math.IsInf(b, 0) {
		return b
	}
	return a + (b-a)*fraction
}
