package main

import (
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
	"github.com/monitoring-forge/linebuf"
	"github.com/monitoring-forge/ltsvparser"
	"github.com/monitoring-forge/sampdo"
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
	Output        string `short:"o" long:"output" description:"Output format" choice:"text" choice:"json" default:"text"` //nolint:staticdelete
	ptSet         []percentile
	input         io.ReadCloser
}

func (o *Opt) tallying() *sampdo.Sampdo {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return o.tallyingContext(ctx)
}

func (o *Opt) tallyingContext(ctx context.Context) *sampdo.Sampdo {
	t := sampdo.New()
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
	// Ensure the input is closed if the context has an error.
	if err := context.Cause(ctx); err != nil {
		<-closed
	}

	cb := func(data []byte) error {
		value, errCB := ltsvparser.ParseFloat(data)
		if errCB == nil {
			errCB = t.Append(value)
		}
		if errCB != nil {
			fmt.Fprintf(os.Stderr, "%v\n", errCB)
		}
		return nil
	}
	errScan := linebuf.Scan(o.input, cb, linebuf.WithStartBufSize(4096), linebuf.WithMaxBufSize(65536))
	if err := context.Cause(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "%s; stopping input and calculating statistics from data read so far.\n", err)
	} else if errScan != nil {
		fmt.Fprintf(os.Stderr, "scan error: %v\n", errScan)
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

func (o *Opt) displayPercentiles(sorted *sampdo.Sorted) (string, error) {
	var buf bytes.Buffer
	max, err := sorted.Max()
	if err != nil {
		return "", err
	}
	min, err := sorted.Min()
	if err != nil {
		return "", err
	}
	mean, err := sorted.Mean()
	if err != nil {
		return "", err
	}
	fmt.Fprintf(&buf, "count: %d\n", sorted.Count())
	fmt.Fprintf(&buf, "max: %.4f\n", max)
	fmt.Fprintf(&buf, "min: %.4f\n", min)
	fmt.Fprintf(&buf, "avg: %.4f\n", mean)
	for _, ps := range o.ptSet {
		p, err := sorted.Percentile(ps.float)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&buf, "%spt: %.4f\n", ps.str, p)
	}
	return buf.String(), nil
}

func (o *Opt) displayJSONPercentiles(sorted *sampdo.Sorted) (string, error) {
	max, err := sorted.Max()
	if err != nil {
		return "", err
	}
	min, err := sorted.Min()
	if err != nil {
		return "", err
	}
	mean, err := sorted.Mean()
	if err != nil {
		return "", err
	}
	r := map[string]any{
		"count": sorted.Count(),
		"min":   jsonNumber(min),
		"max":   jsonNumber(max),
		"avg":   jsonNumber(mean),
	}
	for _, ps := range o.ptSet {
		p, err := sorted.Percentile(ps.float)
		if err != nil {
			return "", err
		}
		r[fmt.Sprintf("%spt", ps.str)] = jsonNumber(p)
	}
	jsonBytes, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("failed to marshal JSON: %w", err)
	}
	return string(jsonBytes), nil
}

func (o *Opt) Run(_ []string) (any, int) {
	data := o.tallying()
	sorted, err := data.Sorted()
	if err != nil {
		return fmt.Errorf("no valid floats to calculate percentiles"), flagrun.CRITICAL
	}
	if o.Output == "json" {
		output, err := o.displayJSONPercentiles(sorted)
		if err != nil {
			return err, flagrun.CRITICAL
		}
		return output, flagrun.OK
	}
	output, err := o.displayPercentiles(sorted)
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
