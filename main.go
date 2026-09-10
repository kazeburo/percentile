package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"

	"errors"

	"github.com/monitoring-forge/flagrun"
	"github.com/monitoring-forge/ltsvparser"
	"github.com/montanaflynn/stats"
	"golang.org/x/term"
)

var version string

type percentile struct {
	str   string
	float float64
}

type Opt struct {
	Version       bool   `short:"v" long:"version" description:"Show version"`
	PercentileSet string `short:"p" long:"percentile-set" description:"Percentiles to display" default:"99,95,90,75"`
	ptileSet      []percentile
	bufioScanner  *bufio.Scanner
	defers        []func()
}

func (o *Opt) tallying() []float64 {
	var t []float64
	s := o.bufioScanner
	for s.Scan() {
		b := s.Bytes()
		if len(b) == 0 {
			continue
		}
		f, err := ltsvparser.ParseFloat(b)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			continue
		}
		t = append(t, f)
	}
	if err := s.Err(); err != nil && !errors.Is(err, io.EOF) {
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
	for _, ps := range o.ptileSet {
		value, err := stats.Percentile(floats, ps.float)
		if err != nil {
			return "", fmt.Errorf("failed to calculate percentile %s: %w", ps.str, err)
		}
		fmt.Fprintf(&buf, "%spt: %.4f\n", ps.str, value)
	}
	return buf.String(), nil
}

func (o *Opt) Run(_ []string) (any, int) {

	floats := o.tallying()
	if len(floats) == 0 {
		return fmt.Errorf("no valid floats to calculate percentiles"), flagrun.CRITICAL
	}

	output, err := o.displayPercentiles(floats)
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

var usage = "`cat <filename> | percentile` or `percentile <filename>`"

func (o *Opt) Validate(args []string) error {
	if o.PercentileSet == "" {
		return fmt.Errorf("--percentile-set is required")
	}
	percentiles, err := parsePercentileSet(o.PercentileSet)
	if err != nil {
		return fmt.Errorf("could not parse --percentile-set: %w", err)
	}
	o.ptileSet = percentiles

	filename := ""
	if len(args) > 0 {
		filename = args[0]
	}
	var r *bufio.Scanner
	switch filename {
	case "":
		if term.IsTerminal(syscall.Stdin) {
			return fmt.Errorf("usage: %s", usage)
		}
		r = bufio.NewScanner(os.Stdin)
	case "-":
		r = bufio.NewScanner(os.Stdin)
	default:
		file, err := os.Open(filename)
		if err != nil {
			return fmt.Errorf("failed to open file: %w", err)
		}
		o.defers = append(o.defers, func() {
			_ = file.Close()
		})
		r = bufio.NewScanner(file)
	}
	o.bufioScanner = r
	return nil
}

func main() {
	opt := &Opt{
		defers: make([]func(), 0),
	}
	defer func() {
		for _, d := range opt.defers {
			d()
		}
	}()
	os.Exit(flagrun.Go(opt, flagrun.Version(version), flagrun.Validator(opt.Validate), flagrun.Usage(usage)))
}
