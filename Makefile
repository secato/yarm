.PHONY: build test test-network lint cover snapshot clean

BINARY := yarm
LDFLAGS := -s -w \
	-X github.com/secato/yarm/internal/buildinfo.Version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev) \
	-X github.com/secato/yarm/internal/buildinfo.Commit=$(shell git rev-parse --short HEAD 2>/dev/null || echo none) \
	-X github.com/secato/yarm/internal/buildinfo.Date=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/yarm

test:
	go test -race -coverprofile=cover.out ./...

# The full suite plus the tests that hit the real internet (upstream
# catalog, reshade.me, GitHub). Kept out of `test` so CI and offline runs
# never depend on third-party servers.
test-network:
	YARM_NETWORK_TESTS=1 go test -race ./...

lint:
	golangci-lint run ./...

cover: test
	go tool cover -func=cover.out

snapshot:
	goreleaser release --snapshot --clean

clean:
	rm -rf bin cover.out
