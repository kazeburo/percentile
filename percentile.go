package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/crypto/ssh/terminal"
)

var (
	// Version wsgate-server version
	Version       string
	showVersion   = flag.Bool("version", false, "show version")
	percentileSet = flag.String("percentile-set", "99,95,90,75", "percentiles to dispaly")
)

type percentile struct {
	str   string
	float float64
}

func printVersion() {
	fmt.Printf(`percentile %s
Compiler: %s %s
`,
		Version,
		runtime.Compiler,
		runtime.Version())
}

func round(f float64) int64 {
	return int64(math.Round(f)) - 1
}

func tallying(r *bufio.Scanner, percentiles []percentile) {
	var l sort.Float64Slice
	var t float64
	for r.Scan() {
		line := r.Text()
		f, err := strconv.ParseFloat(line, 64)
		if err != nil {
			log.Printf("Failed to parse float:%v", err)
			continue
		}
		l = append(l, f)
		t = t + f
	}
	sort.Sort(l)
	fl := float64(len(l))
	fmt.Printf("count: %d\n", len(l))
	fmt.Printf("max: %.4f\n", l[len(l)-1])
	fmt.Printf("avg: %.4f\n", t/fl)
	fmt.Printf("min: %.4f\n", l[0])
	for _, p := range percentiles {
		fmt.Printf("%spt: %.4f\n", p.str, l[round(fl*(p.float))])
	}
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage of percentile:

cat file | percentile

Options
`)
		flag.PrintDefaults()
	}

	flag.Parse()

	if *showVersion {
		printVersion()
		return
	}

	percentiles := []percentile{}
	percentileStrings := strings.Split(*percentileSet, ",")
	for _, s := range percentileStrings {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			log.Fatalf("Could not parse --percentile-set: %v", err)
		}
		f = f / 100
		percentiles = append(percentiles, percentile{s, f})
	}

	if terminal.IsTerminal(0) {
		flag.Usage()
		os.Exit(1)
	}

	tallying(bufio.NewScanner(os.Stdin), percentiles)
}
