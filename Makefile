# PLATFORM: e.g. linux/amd64 to cross-build
IMAGE ?= greyman:latest
PLATFORM ?=
# Stamped into the binary and baked into the image, which the CLI compares at runtime to
# catch a container older than the binary. Committed sources give the commit (+ -dirty);
# outside a git checkout it is dev.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build test image deps

build:
	go build -ldflags "-X greyman/internal/version.Version=$(VERSION)" -o bin/greyman ./cmd/greyman

deps:
	cd daemon && npm ci

test:
	gofmt -l . | (! grep .)
	go vet ./...
	go test ./...
	cd daemon && npm test

image:
	docker build -f docker/Dockerfile --build-arg VERSION=$(VERSION) \
		$(if $(PLATFORM),--platform $(PLATFORM)) -t $(IMAGE) .
