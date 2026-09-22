# percentile

Display percentile statistics from numeric input.

`percentile` reads one floating-point number per line and outputs count, max, min, average, and configurable percentiles. It supports both text and JSON output formats.

## Why `percentile` is fast / 高速な理由

`percentile` is designed to be fast on large numeric inputs.

- **Radix sort for non-negative values**: Non-negative data is sorted with an optimized radix sort instead of a comparison sort. On many distributions, this is significantly faster than Go's default `slices.Sort`.
- **SIMD pre-scan on supported architectures**: On amd64 (SSE2) and ARM64 (NEON), the radix sort pre-scan checks ordering and finds varying key bits using SIMD. Race builds and `-tags=purego` fall back to a portable Go implementation.
- **Optimized sorted/ descending inputs**: Already-sorted or all-equal inputs are detected and handled with minimal work, often avoiding a scratch allocation. Descending inputs are detected and reversed in place.
- **Single sort for multiple statistics**: Count, min, max, average, and all requested percentiles are computed from one sorted view, so the data is not traversed repeatedly.
- **Buffered line input**: Reading uses `linebuf`, which processes input in buffered chunks and avoids creating a separate string for each line. This reduces allocations and copying while scanning large files or piped streams.

On an Apple M3 MacBook Air, reading 1,000,000 lines and producing the default statistics takes about **0.04 seconds**.

## Usage

```
Usage:
  percentile `cat <filename> | percentile` or `percentile <filename>`

Application Options:
  -v, --version            Show version
  -p, --percentile-set=    Percentiles to display (default: 99,95,90,75)
  -o, --output=[text|json] Output format (default: text)

Help Options:
  -h, --help               Show this help message
```

## Options

| Option | Short | Description | Default |
|--------|-------|-------------|---------|
| `--version` | `-v` | Show version and exit. | - |
| `--percentile-set` | `-p` | Comma-separated list of percentiles to display. | `99,95,90,75` |
| `--output` | `-o` | Output format. Choose `text` or `json`. | `text` |

## Examples

### Text output

```
% cat sample.txt
130111.400000
137671.500000
136399.200000
135166.800000
135148.600000
137568.400000
% cat sample.txt | percentile
count: 6
max: 137671.5000
min: 130111.4000
avg: 135344.3167
99pt: 137666.3450
95pt: 137645.7250
90pt: 137619.9500
75pt: 137276.1000
```

### JSON output

```
% cat sample.txt | percentile -o json
{"75pt":137276.1,"90pt":137619.95,"95pt":137645.725,"99pt":137666.345,"avg":135344.31666666668,"count":6,"max":137671.5,"min":130111.4}
```

### Custom percentile set

```
% cat sample.txt | percentile -p 99,90,50
count: 6
max: 137671.5000
min: 130111.4000
avg: 135344.3167
99pt: 137666.3450
90pt: 137619.9500
50pt: 135783.0000
```

## Input format

Each line should contain a single numeric value. Empty lines are ignored. Lines that cannot be parsed as a float are skipped with a warning written to stderr.

Press Ctrl-C (send SIGINT) while reading a file or stdin to stop reading and output statistics for the valid values read so far. A message is written to stderr, and statistics use the selected text or JSON format. If no valid values have been read, the command reports an error as it does for empty input.

## Installation

### Homebrew

```
$ brew install kazeburo/tap/percentile
```

### Download from GitHub Releases

Download the latest release from GitHub Releases and copy the binary to a directory in your `$PATH`.

## License

[MIT](LICENSE)

### Infinite values

Input accepts `Inf`, `+Inf`, `-Inf`, `Infinity`, and signed `Infinity` values. NaN input is skipped with a warning. Percentile positions must still be finite numbers from 0 to 100.

Text output displays non-finite results as `+Inf`, `-Inf`, or `NaN`. JSON represents these results as strings with the same spelling; finite results remain JSON numbers. An average containing both signs of infinity, or interpolation between negative and positive infinity, produces NaN. Exact percentile ranks return the selected value; interpolation between equal infinities returns that infinity.
