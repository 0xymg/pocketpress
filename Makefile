# PocketPress — build tooling
#
# Common targets:
#   make dev          Run backend + admin dev servers together (Ctrl-C stops both)
#   make build        Build the admin SPA and compile the single ./pocketpress binary
#   make build-admin  Build only the admin SPA into admin/dist
#   make test         Run Go and admin test suites
#   make lint         Check gofmt, go vet and the admin type-check/lint
#   make clean        Remove build artifacts
#
# Override the embedded version at build time:
#   make build VERSION=0.2.0

# Version stamped into the binary via -ldflags. Override on the command line.
VERSION ?= 0.1.0

# The exact symbol the Go code exposes for link-time override.
VERSION_PKG := github.com/pocketpress/pocketpress/internal/app.Version
LDFLAGS     := -X $(VERSION_PKG)=$(VERSION)

BINARY   := pocketpress
CMD_PKG  := ./cmd/pocketpress
ADMIN    := admin
SDK      := sdk

.DEFAULT_GOAL := help

.PHONY: help dev build build-admin ensure-dist test lint clean

help: ## Show this help
	@echo "PocketPress make targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

dev: ensure-dist ## Run backend and admin dev servers concurrently
	@echo "Starting PocketPress backend (go run) and admin dev server (vite)."
	@echo "Press Ctrl-C to stop both."
	@trap 'kill 0' EXIT INT TERM; \
		go run $(CMD_PKG) serve & \
		npm run dev --prefix $(ADMIN) & \
		wait

build-admin: ## Build the admin SPA into admin/dist
	@if [ ! -d $(ADMIN)/node_modules ]; then \
		echo "admin/node_modules missing — running npm ci..."; \
		npm ci --prefix $(ADMIN); \
	else \
		echo "Reusing existing admin/node_modules"; \
	fi
	npm run build --prefix $(ADMIN)

ensure-dist: ## Create a placeholder admin/dist so a backend-only build works
	@if [ ! -f $(ADMIN)/dist/index.html ]; then \
		echo "Creating placeholder $(ADMIN)/dist/index.html (run 'make build-admin' for the real SPA)"; \
		mkdir -p $(ADMIN)/dist; \
		printf '%s\n' \
			'<!doctype html>' \
			'<html lang="en">' \
			'  <head>' \
			'    <meta charset="UTF-8" />' \
			'    <title>PocketPress</title>' \
			'  </head>' \
			'  <body>' \
			'    <div id="root">PocketPress admin build placeholder. Run `make build-admin`.</div>' \
			'  </body>' \
			'</html>' \
			> $(ADMIN)/dist/index.html; \
	fi

build: build-admin ## Build the admin SPA and compile the single binary
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(CMD_PKG)
	@echo "Built ./$(BINARY) (version $(VERSION))"

test: ensure-dist ## Run Go, admin, and SDK test suites
	go test ./...
	npm test --prefix $(ADMIN)
	@if [ ! -d $(SDK)/node_modules ]; then \
		echo "$(SDK)/node_modules missing — running npm ci..."; \
		npm ci --prefix $(SDK); \
	fi
	npm test --prefix $(SDK)

lint: ensure-dist ## Check gofmt, go vet, admin type-check, and SDK type-check
	@echo "Checking gofmt..."
	@unformatted=$$(gofmt -l .); \
		if [ -n "$$unformatted" ]; then \
			echo "gofmt found unformatted files:"; \
			echo "$$unformatted"; \
			exit 1; \
		fi
	go vet ./...
	npm run lint --prefix $(ADMIN)
	@if [ ! -d $(SDK)/node_modules ]; then \
		echo "$(SDK)/node_modules missing — running npm ci..."; \
		npm ci --prefix $(SDK); \
	fi
	npm run lint --prefix $(SDK)

clean: ## Remove build artifacts
	rm -f $(BINARY)
	rm -rf $(ADMIN)/dist
	rm -rf $(SDK)/dist
	rm -rf bin dist
