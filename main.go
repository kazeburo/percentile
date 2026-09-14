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
	"strconv"
	"strings"
	"syscall"

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
	Version        bool   `short:"v" long:"version" description:"Show version"`
	PercentileSet  string `short:"p" long:"percentile-set" description:"Percentiles to display" default:"99,95,90,75"`
	Output         string `short:"o" long:"output" description:"Output format" choice:"text" choice:"json" default:"text"` //nolint:staticcheck
	LowCardinality bool   `short:"l" long:"low-cardinality" description:"Optimize for low cardinality data"`
	ptSet          []percentile
	input          io.ReadCloser
}

func (o *Opt) tallying() *Stats {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return o.tallyingContext(ctx)
}

func (o *Opt) tallyingContext(ctx context.Context) *Stats {
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
	t := NewStats(WithPreferSlicesSort(o.LowCardinality))
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
		if err == nil {
			err = t.Append(value)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
		}
	}
	if err := context.Cause(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "%s; stopping input and calculating statistics from data read so far.\n", err)
	} else if err := s.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "scanner error: %v\n", err)
	}
	return t
}

// JSON numbers cannot represent infinities or NaN.
func jsonNumber(value float64) any {
	if math.IsInf(value, 0) || math.IsNaN(value) {
		return strconv.FormatFloat(value, 'g', -1, 64)
	}
	return value
}

func (o *Opt) displayPercentiles(sorted *Sorted) string {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "count: %d\n", sorted.Count())
	fmt.Fprintf(&buf, "max: %.4f\n", sorted.Max())
	fmt.Fprintf(&buf, "min: %.4f\n", sorted.Min())
	fmt.Fprintf(&buf, "avg: %.4f\n", sorted.Mean())
	for _, ps := range o.ptSet {
		fmt.Fprintf(&buf, "%spt: %.4f\n", ps.str, sorted.Percentile(ps.float))
	}
	return buf.String()
}

func (o *Opt) displayJSONPercentiles(sorted *Sorted) (string, error) {
	r := map[string]any{
		"count": sorted.Count(),
		"min":   jsonNumber(sorted.Min()),
		"max":   jsonNumber(sorted.Max()),
		"avg":   jsonNumber(sorted.Mean()),
	}
	for _, ps := range o.ptSet {
		r[fmt.Sprintf("%spt", ps.str)] = jsonNumber(sorted.Percentile(ps.float))
	}
	jsonBytes, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("failed to marshal JSON: %w", err)
	}
	return string(jsonBytes), nil
}

func (o *Opt) Run(_ []string) (any, int) {
	data := o.tallying()
	if len(data.points) == 0 {
		return fmt.Errorf("no valid floats to calculate percentiles"), flagrun.CRITICAL
	}
	sorted, err := data.Sorted()
	if err != nil {
		return err, flagrun.CRITICAL
	}
	if o.Output == "json" {
		output, err := o.displayJSONPercentiles(sorted)
		if err != nil {
			return err, flagrun.CRITICAL
		}
		return output, flagrun.OK
	}
	return o.displayPercentiles(sorted), flagrun.OK
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
		return openStdin()
	case "-":
		return openStdin()
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
