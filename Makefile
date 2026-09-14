# cub-demo — fleet-scale demo data seeder, published as a cub plugin
#
# Common targets (run `make help` for the full list):
#   make build              host binary into bin/
#   make check              fmt-check + vet + test (the CI gate)
#   make plugin             install into cub from this checkout, via cub itself
#   make dist GOOS=.. GOARCH=..   cross-compile one binary + checksum

BINARY   := cub-demo
BIN_DIR  := bin
DIST_DIR := dist
GO       ?= go
CUB      ?= cub

# Version stamps cmd.version. Derived from git (leading "v" stripped so the
# release asset and `cub demo version` read e.g. 0.1.5); override on the
# release build with `make dist VERSION=0.1.5`.
GIT_VERSION := $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//')
VERSION ?= $(if $(GIT_VERSION),$(GIT_VERSION),dev)

LDFLAGS         := -X github.com/confighub/cub-demo/cmd.version=$(VERSION)
RELEASE_LDFLAGS := -s -w $(LDFLAGS)

# Cross-compile target (defaults to the host).
GOOS   ?= $(shell $(GO) env GOOS)
GOARCH ?= $(shell $(GO) env GOARCH)
ASSET  := cub-demo-$(GOOS)-$(GOARCH)

.PHONY: all build dist vet fmt fmt-check test test-race tidy check run plugin plugin-uninstall e2e clean help

all: build

build: ## Build the cub-demo binary into bin/
	@mkdir -p $(BIN_DIR)
	$(GO) build -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(BINARY) .

dist: ## Cross-compile one release asset (set GOOS/GOARCH) + checksum into dist/
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(RELEASE_LDFLAGS)' -o $(DIST_DIR)/$(ASSET) .
	@cd $(DIST_DIR) && (sha256sum $(ASSET) 2>/dev/null || shasum -a 256 $(ASSET)) > $(ASSET).sha256
	@echo "built $(DIST_DIR)/$(ASSET) (version $(VERSION))"

vet: ## Run go vet
	$(GO) vet ./...

fmt: ## Format the sources in place
	gofmt -w .

fmt-check: ## Fail if any source is not gofmt-clean
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "not gofmt-clean:"; echo "$$unformatted"; exit 1; \
	fi

test: ## Run the tests
	$(GO) test ./...

test-race: ## Run the tests under the race detector
	$(GO) test -race ./...

tidy: ## Tidy go.mod / go.sum
	$(GO) mod tidy

check: fmt-check vet test ## The CI gate: fmt-check + vet + test

run: ## Run cub-demo directly (pass args via ARGS="...")
	$(GO) run . $(ARGS)

# Installs through cub's own local-path install rather than by dropping a binary
# into the plugin directory, so local development exercises the same code a
# release does — including the install hook that writes cub-plugin.yaml.
plugin: build ## Install this build into cub as the "demo" plugin
	@if $(CUB) plugin list 2>/dev/null | grep -q '^demo[[:space:]]'; then \
		$(CUB) plugin upgrade demo; \
	else \
		$(CUB) plugin install ./$(BIN_DIR)/$(BINARY); \
	fi
	@echo "run 'cub demo version'"

plugin-uninstall: ## Remove the plugin from cub
	$(CUB) plugin uninstall demo

# The script drives the INSTALLED plugin (cub demo), so install what was just
# built first -- depending only on build once let e2e pass against a stale plugin.
e2e: plugin ## Run the end-to-end test against the active cub context (see hack/e2e.sh)
	./hack/e2e.sh

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) $(DIST_DIR)

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'
