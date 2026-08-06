.PHONY: build test lint clean build-all checksums catalog-assets

BINARY ?= plugin
PLUGIN_SLUG ?= silo-plugin-local-artwork
PLATFORMS = linux/amd64 linux/arm64 darwin/arm64
VERSION ?= $(shell git describe --tags --always 2>/dev/null | sed 's/^v//')
LDFLAGS = -s -w -X main.version=$(VERSION)

build:
	go build -trimpath -ldflags="$(LDFLAGS)" -o $(BINARY) .

test:
	go test ./...

lint:
	golangci-lint run ./...

clean:
	rm -rf dist $(BINARY) silo-plugin-local-artwork

build-all:
	mkdir -p dist
	@for platform in $(PLATFORMS); do \
		GOOS=$${platform%%/*} GOARCH=$${platform##*/} CGO_ENABLED=0 \
		go build -trimpath -ldflags="$(LDFLAGS)" -o dist/$(BINARY)-$${platform%%/*}-$${platform##*/} .; \
	done

checksums: build-all
	cd dist && shasum -a 256 $(BINARY)-* > checksums.txt

catalog-assets: build-all
	mkdir -p dist/catalog
	@for platform in $(PLATFORMS); do \
		src=dist/$(BINARY)-$${platform%%/*}-$${platform##*/}; \
		dst=dist/catalog/$(BINARY)-$${platform%%/*}-$${platform##*/}-$(PLUGIN_SLUG); \
		cp "$$src" "$$dst"; \
	done
	cd dist/catalog && shasum -a 256 $(BINARY)-* > checksums.txt
