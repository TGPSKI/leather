.PHONY: build install test test-race check lint ci integration \
		bench bench-save bench-compare \
		cover clean help \
        build-shell-mcp \
        examples examples-smoke examples-all examples-reset example-%

VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT   ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
LDFLAGS  := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -extldflags '-static'
GO_ENV   := CGO_ENABLED=0

build: 
	$(GO_ENV) go build -tags 'netgo osusergo' -ldflags "$(LDFLAGS)" -o ./leather ./cmd/leather

install:
	$(GO_ENV) go install -tags 'netgo osusergo' -ldflags "$(LDFLAGS)" ./cmd/leather ./cmd/shell-mcp

build-shell-mcp:
	$(GO_ENV) go build -tags 'netgo osusergo' -ldflags "$(LDFLAGS)" -o ./shell-mcp ./cmd/shell-mcp

test:
	go test ./...

test-race:
	go test -race ./...

check:
	@out=$$(gofmt -l $$(find . -name '*.go' -not -path './.agents/*')); \
		if [ -n "$$out" ]; then echo "gofmt: unformatted files:"; echo "$$out"; exit 1; fi
	go vet ./...
	go mod verify

lint:
	golangci-lint run

integration:
	go test -tags integration -race ./cmd/leather/...

ci: check test-race lint integration

bench:
	go test -bench=. -benchmem -benchtime=3s ./...

bench-save:
	go test -bench=. -benchmem -benchtime=3s ./... | tee bench-baseline.txt

bench-compare:
	go test -bench=. -benchmem -benchtime=3s ./... | benchstat bench-baseline.txt /dev/stdin

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

clean:
	rm -f leather shell-mcp coverage.out bench-baseline.txt
	rm -f cli.out config.out logging.out
	$(MAKE) -C examples clean

# Delegate to examples/Makefile. See examples/README.md for the full list.
examples:
	@$(MAKE) -C examples help

examples-smoke: build
	@$(MAKE) -C examples smoke

examples-all: build
	@$(MAKE) -C examples all

examples-reset:
	@$(MAKE) -C examples reset

# Shortcut: `make example-NN` runs example NN (delegates to examples/Makefile).
# Examples: make example-01, make example-08-requeue, make example-09-live
example-%: build
	@$(MAKE) -C examples $*

help:
	@echo "Targets:"
	@echo "  build        compile leather binary"
	@echo "  install      install leather and shell-mcp to GOBIN/GOPATH bin"
	@echo "  test         go test ./..."
	@echo "  test-race    go test -race ./..."
	@echo "  check        gofmt + go vet"
	@echo "  lint         golangci-lint run"
	@echo "  integration  integration tests (build tag: integration)"
	@echo "  ci           check + test-race + lint + integration (full gate)"
	@echo "  bench        all benchmarks"
	@echo "  bench-save   save benchmark baseline"
	@echo "  bench-compare compare against saved baseline (requires benchstat)"
	@echo "  cover        coverage report (opens browser)"
	@echo "  clean                           remove build artifacts"
	@echo "  build-shell-mcp                 compile shell-mcp binary"
	@echo "  examples                        list runnable examples (see examples/README.md)"
	@echo "  example-NN                      run a single example (e.g. example-02, example-09-live)"
	@echo "  examples-smoke                	 validate every example config (no LLM)"
	@echo "  examples-all               	 validate every example config with LLM"
	@echo "  examples-reset             	 wipe per-example runtime state (.state/ dirs)"
	
