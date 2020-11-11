VERSION=0.1.0
LDFLAGS=-ldflags "-w -s -X main.Version=${VERSION}"
all: percentile

.PHONY: percentile

percentile: percentile.go
	go build $(LDFLAGS) -o percentile

linux: percentile.go
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o percentile

fmt:
	go fmt ./...

clean:
	rm -rf percentile

tag:
	git tag v${VERSION}
	git push origin v${VERSION}
	git push origin master
	goreleaser --rm-dist
