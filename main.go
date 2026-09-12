package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"errors"

	"encoding/json"

	"github.com/monitoring-forge/flagrun"
	"github.com/monitoring-forge/ltsvparser"
	"golang.org/x/term"
)

var version string
var usage = "`cat <filename> | percentile` or `percentile <filename>`"

type percentile struct {
	str   string
	float float64
}

type Opt struct {
	Version       bool   `short:"v" long:"version" description:"Show version"`
	PercentileSet string `short:"p" long:"percentile-set" description:"Percentiles to display" default:"99,95,90,75"`
	Output        string `short:"o" long:"output" description:"Output format" choice:"text" choice:"json" default:"text"` //nolint:staticcheck
	ptSet         []percentile
	input         io.ReadCloser
}

func (o *Opt) tallying() []float64 {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return o.tallyingContext(ctx)
}

func parseFloat(s []byte) (float64, error) {
	value, err := ltsvparser.ParseFloat(s)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(value) {
		return 0, fmt.Errorf("NaN value encountered")
	}
	if math.IsInf(value, 0) {
		return 0, fmt.Errorf("Infinite value encountered")
	}
	return value, nil
}

func (o *Opt) tallyingContext(ctx context.Context) []float64 {
	closed := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		_ = o.input.Close()
		close(closed)
	})
	defer func() {
		if stop() {
			// The AfterFunc was stopped before it could run, so we need to close the input ourselves.
			_ = o.input.Close()
		} else {
			// The AfterFunc has run, so the input has already been closed.
			<-closed
		}
	}()

	var t []float64
	s := bufio.NewScanner(o.input)
	for ctx.Err() == nil && s.Scan() {
		if ctx.Err() != nil {
			break
		}
		b := s.Bytes()
		if len(b) == 0 {
			continue
		}
		value, err := parseFloat(b)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			continue
		}
		t = append(t, value)
	}
	if err := context.Cause(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "%s; stopping input and calculating statistics from data read so far.\n", err)
	} else if err := s.Err(); err != nil && !errors.Is(err, io.EOF) {
		fmt.Fprintf(os.Stderr, "scanner error: %v\n", err)
	}
	return t
}

// percentileSorted uses the same type-7 interpolation and bounds as stats.Percentile.
// The caller must supply sorted data.
func percentileSorted(sorted []float64, percent float64) float64 {
	if len(sorted) == 0 {
		return math.NaN()
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	rank := (percent / 100) * float64(len(sorted)-1)
	k := int(rank)
	f := rank - float64(k)
	if k+1 < len(sorted) {
		return sorted[k] + f*(sorted[k+1]-sorted[k])
	}
	return sorted[k]
}

// calculatePercentiles calculates the specified percentiles for the given slice of floats.
// It returns the minimum, maximum, average, and the calculated percentiles in the order specified by o.ptSet.
func (o *Opt) calculatePercentiles(floats []float64) (float64, float64, float64, []float64, error) {
	if len(floats) == 0 {
		return math.NaN(), math.NaN(), math.NaN(), nil, fmt.Errorf("no floats to calculate percentiles")
	}
	// Share one sorted copy across all percentiles, improving performance by avoiding repeated sorting.
	sorted := slices.Clone(floats)
	slices.Sort(sorted)

	minValue := sorted[0]
	maxValue := sorted[len(sorted)-1]
	avgValue := floatsum(floats) / float64(len(floats))

	if len(o.ptSet) == 0 {
		return minValue, maxValue, avgValue, []float64{}, nil
	}

	values := make([]float64, len(o.ptSet))
	for i, ps := range o.ptSet {
		values[i] = percentileSorted(sorted, ps.float)
	}

	return minValue, maxValue, avgValue, values, nil
}

func floatsum(floats []float64) float64 {
	sum := 0.0
	for _, v := range floats {
		sum += v
	}
	return sum
}

func (o *Opt) displayPercentiles(floats []float64) (string, error) {
	var buf bytes.Buffer
	// Count
	fmt.Fprintf(&buf, "count: %d\n", len(floats))

	// Percentiles
	minValue, maxValue, avgValue, values, err := o.calculatePercentiles(floats)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(&buf, "max: %.4f\n", maxValue)
	fmt.Fprintf(&buf, "min: %.4f\n", minValue)
	fmt.Fprintf(&buf, "avg: %.4f\n", avgValue)
	for i, ps := range o.ptSet {
		fmt.Fprintf(&buf, "%spt: %.4f\n", ps.str, values[i])
	}
	return buf.String(), nil
}

func (o *Opt) displayJSONPercentiles(floats []float64) (string, error) {
	r := make(map[string]any)
	// Count
	r["count"] = len(floats)

	minValue, maxValue, avgValue, values, err := o.calculatePercentiles(floats)
	if err != nil {
		return "", err
	}
	r["min"] = minValue
	r["max"] = maxValue
	r["avg"] = avgValue
	for i, ps := range o.ptSet {
		r[fmt.Sprintf("%spt", ps.str)] = values[i]
	}

	jsonBytes, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("failed to marshal JSON: %w", err)
	}
	return string(jsonBytes), nil

}

func (o *Opt) Run(_ []string) (any, int) {

	floats := o.tallying()
	if len(floats) == 0 {
		return fmt.Errorf("no valid floats to calculate percentiles"), flagrun.CRITICAL
	}

	var output string
	var err error
	if o.Output == "json" {
		output, err = o.displayJSONPercentiles(floats)
	} else {
		output, err = o.displayPercentiles(floats)
	}
	if err != nil {
		return err, flagrun.CRITICAL
	}
	return output, flagrun.OK
}

func parsePercentileSet(s string) ([]percentile, error) {
	percentiles := []percentile{}
	for part := range strings.SplitSeq(s, ",") {
		f, err := strconv.ParseFloat(part, 64)
		if err != nil {
			return nil, err
		}
		if f < 0 || f > 100 {
			return nil, fmt.Errorf("percentile must be between 0 and 100: %v", f)
		}
		if math.IsNaN(f) {
			return nil, fmt.Errorf("percentile must not be NaN")
		}
		if math.IsInf(f, 0) {
			return nil, fmt.Errorf("percentile must not be infinity")
		}
		percentiles = append(percentiles, percentile{str: part, float: f})
	}
	return percentiles, nil
}

func buildInput(filename string) (io.ReadCloser, error) {
	switch filename {
	case "":
		if term.IsTerminal(0) {
			return nil, fmt.Errorf("usage: %s", usage)
		}
		return os.Stdin, nil
	case "-":
		return os.Stdin, nil
	default:
		file, err := os.Open(filename)
		if err != nil {
			return nil, fmt.Errorf("failed to open file: %w", err)
		}
		return file, nil
	}
}

func (o *Opt) Validate(args []string) error {
	if o.PercentileSet == "" {
		return fmt.Errorf("--percentile-set is required")
	}
	percentiles, err := parsePercentileSet(o.PercentileSet)
	if err != nil {
		return fmt.Errorf("could not parse --percentile-set: %w", err)
	}
	o.ptSet = percentiles

	filename := ""
	if len(args) > 0 {
		filename = args[0]
	}
	o.input, err = buildInput(filename)
	if err != nil {
		return err
	}
	return nil
}

func main() {
	opt := &Opt{}
	code := flagrun.Go(opt, flagrun.Version(version), flagrun.Validator(opt.Validate), flagrun.Usage(usage))
	os.Exit(code)
}
