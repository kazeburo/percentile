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
		f, err := ltsvparser.ParseFloat(b)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to parsefloat `%s`: %v", string(b), err)
			continue
		}
		t = append(t, f)
	}
	if err := s.Err(); err != nil && !errors.Is(err, io.EOF) {
		fmt.Fprintf(os.Stderr, "scanner error: %v", err)
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
		return "", fmt.Errorf("failed to calculate max: %v", err)
	}
	fmt.Fprintf(&buf, "max: %.4f\n", maxValue)

	// Min
	minValue, err := stats.Min(floats)
	if err != nil {
		return "", fmt.Errorf("failed to calculate min: %v", err)
	}
	fmt.Fprintf(&buf, "min: %.4f\n", minValue)

	// Average
	avgValue, err := stats.Mean(floats)
	if err != nil {
		return "", fmt.Errorf("failed to calculate average: %v", err)
	}
	fmt.Fprintf(&buf, "avg: %.4f\n", avgValue)

	// Percentiles
	for _, ps := range o.ptileSet {
		value, err := stats.Percentile(floats, ps.float)
		if err != nil {
			return "", fmt.Errorf("failed to calculate percentile %s: %v", ps.str, err)
		}
		fmt.Fprintf(&buf, "%spt: %.4f\n", ps.str, value)
	}
	return buf.String(), nil
}

func (o *Opt) Run(_ []string) (any, int) {

	floats := o.tallying()
	if len(floats) == 0 {
		return fmt.Errorf("No valid floats to calculate percentiles"), flagrun.CRITICAL
	}

	output, err := o.displayPercentiles(floats)
	if err != nil {
		return err, flagrun.CRITICAL
	}
	return output, flagrun.OK
}

func parsePercentileSet(s string) ([]percentile, error) {
	percentiles := []percentile{}
	for _, part := range strings.Split(s, ",") {
		f, err := strconv.ParseFloat(part, 64)
		if err != nil {
			return nil, err
		}
		percentiles = append(percentiles, percentile{str: part, float: f})
	}
	return percentiles, nil
}

func (o *Opt) Validate(args []string) error {
	if o.PercentileSet == "" {
		return fmt.Errorf("--percentile-set is required")
	}
	percentiles, err := parsePercentileSet(o.PercentileSet)
	if err != nil {
		return fmt.Errorf("Could not parse --percentile-set: %v", err)
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
			return fmt.Errorf("Usage: `cat <filename> | percentile` or `percentile <filename>`")
		}
		r = bufio.NewScanner(os.Stdin)
	case "-":
		r = bufio.NewScanner(os.Stdin)
	default:
		file, err := os.Open(filename)
		if err != nil {
			return fmt.Errorf("Failed to open file: %v", err)
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
	os.Exit(flagrun.Go(opt, flagrun.Version(version), flagrun.Validator(opt.Validate)))
}
