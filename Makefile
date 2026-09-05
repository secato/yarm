.PHONY: build test lint cover snapshot clean

BINARY := yarm
LDFLAGS := -s -w \
	-X github.com/secato/yarm/internal/buildinfo.Version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev) \
	-X github.com/secato/yarm/internal/buildinfo.Commit=$(shell git rev-parse --short HEAD 2>/dev/null || echo none) \
	-X github.com/secato/yarm/internal/buildinfo.Date=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/yarm

test:
	go test -race -coverprofile=cover.out ./...

lint:
	golangci-lint run ./...

cover: test
	go tool cover -func=cover.out

snapshot:
	goreleaser release --snapshot --clean

clean:
	rm -rf bin cover.out
