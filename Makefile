# BROWSER: chromium (arm64 + amd64) | chrome (amd64 only)
# PLATFORM: e.g. linux/amd64 to cross-build
IMAGE ?= greyman:latest
BROWSER ?= chromium
PLATFORM ?=
# Stamped into the binary and recorded by `greyman install`.
VERSION ?= dev

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
	docker build -f docker/Dockerfile --build-arg BROWSER=$(BROWSER) \
		$(if $(PLATFORM),--platform $(PLATFORM)) -t $(IMAGE) .
