VERSION=0.1.5
LDFLAGS=-ldflags "-w -s -X main.version=${VERSION}"
all: percentile

.PHONY: percentile

percentile: *.go
	go build $(LDFLAGS) -o percentile

linux: *.go
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o percentile

check: *.go
	go test -v ./...
	go test -race ./...

lint:
	golangci-lint run --timeout 5m ./...
