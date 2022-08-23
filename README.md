# percentile

Display percentile.

## Usage

```
Usage of percentile:

cat file | percentile

Options
  -version
        show version
```

## Example

```
% cat sample.txt
130111.400000
137671.500000
136399.200000
135166.800000
135148.600000
137568.400000
% cat sample.txt|percentile
count: 6
max: 137671.5000
avg: 135344.3167
min: 130111.4000
99pt: 137671.5000
95pt: 137671.5000
90pt: 137568.4000
75pt: 137568.4000
```

## Installation

### homebrew

Use homebrew tap

```
$ brew install kazeburo/tap/percentile
```

### Download from GitHub Releases

Download from GitHub Releases and copy it to your $PATH.
