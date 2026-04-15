.PHONY: build vet test install clean release release-snapshot

VERSION ?= dev

build:
	go build -ldflags "-s -w -X main.version=$(VERSION)" -o gsc ./cmd/gsc

vet:
	go vet ./...

test:
	go test ./...

install:
	go install -ldflags "-s -w -X main.version=$(VERSION)" ./cmd/gsc

clean:
	rm -rf gsc dist/

release-snapshot:
	goreleaser release --snapshot --clean

release:
	goreleaser release --clean
