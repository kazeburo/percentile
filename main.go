package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"errors"

	"encoding/json"

	"github.com/monitoring-forge/flagrun"
	"github.com/monitoring-forge/ltsvparser"
	"github.com/montanaflynn/stats"
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
		value, err := ltsvparser.ParseFloat(b)
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

func (o *Opt) displayPercentiles(floats []float64) (string, error) {
	var buf bytes.Buffer
	// Count
	fmt.Fprintf(&buf, "count: %d\n", len(floats))

	// Max
	maxValue, err := stats.Max(floats)
	if err != nil {
		return "", fmt.Errorf("failed to calculate max: %w", err)
	}
	fmt.Fprintf(&buf, "max: %.4f\n", maxValue)

	// Min
	minValue, err := stats.Min(floats)
	if err != nil {
		return "", fmt.Errorf("failed to calculate min: %w", err)
	}
	fmt.Fprintf(&buf, "min: %.4f\n", minValue)

	// Average
	avgValue, err := stats.Mean(floats)
	if err != nil {
		return "", fmt.Errorf("failed to calculate average: %w", err)
	}
	fmt.Fprintf(&buf, "avg: %.4f\n", avgValue)

	// Percentiles
	for _, ps := range o.ptSet {
		value, err := stats.Percentile(floats, ps.float)
		if err != nil {
			return "", fmt.Errorf("failed to calculate percentile %s: %w", ps.str, err)
		}
		fmt.Fprintf(&buf, "%spt: %.4f\n", ps.str, value)
	}
	return buf.String(), nil
}

func (o *Opt) displayJSONPercentiles(floats []float64) (string, error) {
	r := make(map[string]any)
	// Count
	r["count"] = len(floats)

	// Max
	maxValue, err := stats.Max(floats)
	if err != nil {
		return "", fmt.Errorf("failed to calculate max: %w", err)
	}
	r["max"] = maxValue

	// Min
	minValue, err := stats.Min(floats)
	if err != nil {
		return "", fmt.Errorf("failed to calculate min: %w", err)
	}
	r["min"] = minValue

	// Average
	avgValue, err := stats.Mean(floats)
	if err != nil {
		return "", fmt.Errorf("failed to calculate average: %w", err)
	}
	r["avg"] = avgValue

	// Percentiles
	for _, ps := range o.ptSet {
		value, err := stats.Percentile(floats, ps.float)
		if err != nil {
			return "", fmt.Errorf("failed to calculate percentile %s: %w", ps.str, err)
		}
		r[fmt.Sprintf("%spt", ps.str)] = value
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
