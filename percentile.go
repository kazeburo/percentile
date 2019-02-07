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

	"golang.org/x/crypto/ssh/terminal"
)

var (
	// Version wsgate-server version
	Version     string
	showVersion = flag.Bool("version", false, "show version")
)

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

func percentile(r *bufio.Scanner) {
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
	fmt.Printf("avg: %.4f\n", t/fl)
	fmt.Printf("95pt: %.4f\n", l[round(fl*0.95)])
	fmt.Printf("90pt: %.4f\n", l[round(fl*0.90)])
	fmt.Printf("75pt: %.4f\n", l[round(fl*0.75)])
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

	if terminal.IsTerminal(0) {
		flag.Usage()
		os.Exit(1)
	}

	percentile(bufio.NewScanner(os.Stdin))
}
