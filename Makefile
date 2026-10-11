# citric — common tasks. `make` builds everything into ./bin.

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
GO      ?= go
export CGO_ENABLED=0

.PHONY: all web build clients test lint fmt docker docker-full clean

all: build

web:
	cd web && npm ci --no-audit --no-fund && npm run build

# Server + CLI for this machine (the web UI is embedded, so build it first).
build: web
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/citric ./cmd/citric

# The client binaries a server hands out at /dl. Copy bin/dist into
# <data_dir>/dist, or point dist_dir at it.
clients: web
	@for t in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do \
	  os=$${t%/*}; arch=$${t#*/}; ext=; [ "$$os" = windows ] && ext=.exe; \
	  echo "  $$os/$$arch"; \
	  GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/dist/citric-$$os-$$arch$$ext ./cmd/citric || exit 1; \
	done

test:
	mkdir -p internal/web/dist && touch internal/web/dist/index.html
	$(GO) vet ./...
	$(GO) test -race ./...

lint: test
	test -z "$$(gofmt -l .)"
	cd web && npx tsc --noEmit -p .

fmt:
	gofmt -w .

docker:
	docker build --target slim --build-arg VERSION=$(VERSION) -t ghcr.io/blaxstar/citric:$(VERSION) .

docker-full:
	docker build --target full --build-arg VERSION=$(VERSION) -t ghcr.io/blaxstar/citric:$(VERSION)-full .

clean:
	rm -rf bin internal/web/dist
