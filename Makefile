# PLATFORM: e.g. linux/amd64 to cross-build
IMAGE ?= greyman:latest
PLATFORM ?=
# Stamped into the binary and baked into the image, which the CLI compares at runtime to
# catch a container older than the binary. Committed sources give the commit (+ -dirty);
# outside a git checkout it is dev.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
PLATFORMS = darwin/amd64 darwin/arm64 linux/amd64 linux/arm64
INSTALL_SH ?= docs/placeholder_install.sh

.PHONY: build test image deps dist

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

# Release assets for the install script: a binary per platform, checksums.txt, and
# install.sh with the version filled in. Release tags only (same pattern as version.IsRelease).
dist:
	@echo "$(VERSION)" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?$$' \
		|| { echo "dist needs a clean release tag (v1.2.3 or v1.2.3-rc.1), got $(VERSION)" >&2; exit 1; }
	rm -rf dist && mkdir dist
	for p in $(PLATFORMS); do \
		CGO_ENABLED=0 GOOS=$${p%/*} GOARCH=$${p#*/} go build -trimpath \
			-ldflags "-X greyman/internal/version.Version=$(VERSION)" \
			-o dist/greyman-$${p%/*}-$${p#*/} ./cmd/greyman || exit 1; \
	done
	sed 's/__VERSION__/$(VERSION)/' $(INSTALL_SH) > dist/install.sh
	cd dist && shasum -a 256 greyman-* > checksums.txt
