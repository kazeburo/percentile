VERSION=0.0.1
LDFLAGS=-ldflags "-X main.Version=${VERSION}"
all: percentile

.PHONY: percentile

bundle:
	dep ensure

update:
	dep ensure -update

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
