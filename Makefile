# Grid Planner — build & cross-compile.
#
# Run every target from the project root.
#
# Common targets:
#   make build        Build the frontend, embed it, compile for this machine.
#   make all          Cross-compile release binaries for Windows/macOS/Linux.
#   make run          Build everything and run locally.
#   make test         Run the Go test suite.
#   make clean        Remove build artifacts.
#
# Go is invoked via $(GO); override it if go is not on your PATH, e.g.:
#   make build GO=$$HOME/go-sdk/go/bin/go
#
# Per-team builds — bake a specific map into the binary and name it:
#   make build MAP=local/teamA/map.json APP=Coverage-TeamA
#   make all   MAP=local/teamB/map.json APP=Coverage-TeamB
# MAP is optional; without it the built-in example map (map.default.json) is
# embedded. A baked binary can still be overridden at runtime via MAP_CONFIG or
# a map.json next to it.

GO      ?= go
NPM     ?= npm
APP      = GridPlanner

# MAP: optional path to a map JSON to bake into the binary (see header). When
# set, it temporarily replaces the embedded example for the duration of a build.
MAP     ?=

# Layout (all paths are relative to the project root).
# BIN_DIR      holds the compiled binaries (gitignored).
# DIST_DIR     is the embed-staging copy of the built frontend (gitignored).
# EMBEDDED_MAP is the map baked into the binary (go:embed reads this file).
BACKEND      = backend
FRONTEND     = frontend
BIN_DIR      = bin
DIST_DIR     = $(BACKEND)/internal/web/dist
EMBEDDED_MAP = $(BACKEND)/internal/mapconfig/map.default.json

# Reproducible, dependency-free static binaries.
LDFLAGS  = -s -w
GOFLAGS  = -trimpath
export CGO_ENABLED = 0

.PHONY: all build frontend embed run test clean local stage-map restore-map \
        windows macos-amd64 macos-arm64 linux dirs

# ── Map baking ─────────────────────────────────────────────────────────────
# stage-map swaps a custom MAP into the embed slot (backing up the committed
# example); restore-map puts the example back so the working tree stays clean.
# Both are no-ops when MAP is empty.

stage-map:
	@if [ -n "$(MAP)" ]; then \
		if [ ! -f "$(MAP)" ]; then echo "error: MAP file not found: $(MAP)"; exit 1; fi; \
		if [ -f "$(EMBEDDED_MAP).bak" ]; then mv "$(EMBEDDED_MAP).bak" "$(EMBEDDED_MAP)"; fi; \
		cp "$(EMBEDDED_MAP)" "$(EMBEDDED_MAP).bak"; \
		cp "$(MAP)" "$(EMBEDDED_MAP)"; \
		echo ">> baking map from $(MAP)"; \
	fi

restore-map:
	@if [ -f "$(EMBEDDED_MAP).bak" ]; then \
		mv "$(EMBEDDED_MAP).bak" "$(EMBEDDED_MAP)"; \
		echo ">> restored built-in example map"; \
	fi

# ── Frontend build + embed ─────────────────────────────────────────────────

## frontend: build the Vite app and copy it into the embed-staging directory.
frontend:
	cd $(FRONTEND) && $(NPM) install && $(NPM) run build
	rm -rf $(DIST_DIR)
	mkdir -p $(DIST_DIR)
	cp -R $(FRONTEND)/dist/. $(DIST_DIR)/

embed: frontend

# ── Local build ────────────────────────────────────────────────────────────

## build: frontend + a binary for the current OS/arch (bakes MAP if given).
build: stage-map frontend local restore-map

## local: compile for the current machine (assumes dist + embedded map ready).
local: dirs
	cd $(BACKEND) && $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o ../$(BIN_DIR)/$(APP) .

## run: build everything and launch it.
run: build
	$(BIN_DIR)/$(APP)

# ── Cross-compilation ──────────────────────────────────────────────────────

## all: build the frontend once, then cross-compile every release binary.
all: stage-map frontend windows macos-amd64 macos-arm64 linux restore-map
	@echo "Release binaries are in $(BIN_DIR)/"

windows: dirs
	cd $(BACKEND) && GOOS=windows GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o ../$(BIN_DIR)/$(APP)-windows-amd64.exe .

macos-amd64: dirs
	cd $(BACKEND) && GOOS=darwin GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o ../$(BIN_DIR)/$(APP)-macos-amd64 .

macos-arm64: dirs
	cd $(BACKEND) && GOOS=darwin GOARCH=arm64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o ../$(BIN_DIR)/$(APP)-macos-arm64 .

linux: dirs
	cd $(BACKEND) && GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o ../$(BIN_DIR)/$(APP)-linux-amd64 .

# ── Utility ────────────────────────────────────────────────────────────────

test:
	cd $(BACKEND) && $(GO) test ./...

dirs:
	mkdir -p $(BIN_DIR)

clean:
	rm -rf $(BIN_DIR) $(DIST_DIR)/assets
	@if [ -f "$(EMBEDDED_MAP).bak" ]; then mv "$(EMBEDDED_MAP).bak" "$(EMBEDDED_MAP)"; fi
