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

GO      ?= go
NPM     ?= npm
APP      = GridPlanner

# Layout (all paths are relative to the project root).
# BIN_DIR  holds the compiled binaries (gitignored).
# DIST_DIR is the embed-staging copy of the built frontend (gitignored).
BACKEND  = backend
FRONTEND = frontend
BIN_DIR  = bin
DIST_DIR = $(BACKEND)/internal/web/dist

# Reproducible, dependency-free static binaries.
LDFLAGS  = -s -w
GOFLAGS  = -trimpath
export CGO_ENABLED = 0

.PHONY: all build frontend embed run test clean local \
        windows macos-amd64 macos-arm64 linux dirs

# ── Frontend build + embed ─────────────────────────────────────────────────

## frontend: build the Vite app and copy it into the embed-staging directory.
frontend:
	cd $(FRONTEND) && $(NPM) install && $(NPM) run build
	rm -rf $(DIST_DIR)
	mkdir -p $(DIST_DIR)
	cp -R $(FRONTEND)/dist/. $(DIST_DIR)/

embed: frontend

# ── Local build ────────────────────────────────────────────────────────────

## build: frontend + a binary for the current OS/arch.
build: frontend local

## local: compile for the current machine (assumes dist already built).
local: dirs
	cd $(BACKEND) && $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o ../$(BIN_DIR)/$(APP) .

## run: build everything and launch it.
run: build
	$(BIN_DIR)/$(APP)

# ── Cross-compilation ──────────────────────────────────────────────────────

## all: build the frontend once, then cross-compile every release binary.
all: frontend windows macos-amd64 macos-arm64 linux
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
