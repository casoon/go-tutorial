VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: run test check build build-all docker clean

run:
	UPWATCH_LOG_LEVEL=debug go run ./cmd/upwatch

test:
	go test -race ./...

# Everything CI runs, in one command.
check:
	test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)
	go vet ./...
	go test -race ./...
	go test ./beispiele/...

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/upwatch ./cmd/upwatch

# Cross-compilation: one machine, every target platform.
build-all:
	GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/upwatch-linux-amd64   ./cmd/upwatch
	GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/upwatch-linux-arm64   ./cmd/upwatch
	GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/upwatch-darwin-arm64  ./cmd/upwatch
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/upwatch-windows-amd64.exe ./cmd/upwatch

docker:
	docker build --build-arg VERSION=$(VERSION) -t upwatch:$(VERSION) .

clean:
	rm -rf bin/ *.db *.db-shm *.db-wal
