# CLAUDE.md — working rules for this repo

Guidance for AI agents (and humans) working on **Grid Planner**. Read this
before making changes.

## What this is

A single-binary desktop app for scheduling workers who cover the streets of a
city's downtown grid: a Go web server (`backend/`, Go module `gridplanner`) with
an embedded Vite + React frontend (`frontend/`). Data is a single JSON file; no
database engine, no cgo, static binaries. The street grid is data-driven — a
JSON map file (embedded example + external override), so the app is not tied to
any one city. See `README.md` for the full picture.

## Toolchain

- **Go lives at `$HOME/go-sdk/go/bin/go` and is NOT on `PATH`.** Invoke it with
  the full path, or via the Makefile's override: `make <target> GO=$HOME/go-sdk/go/bin/go`.
- **Always run `make` from the project root.** The Makefile is at the root;
  `backend/` has no Makefile of its own.
- Frontend needs Node + npm.

## Build / test / run

```sh
make build GO=$HOME/go-sdk/go/bin/go   # frontend + embed + compile -> bin/
make test  GO=$HOME/go-sdk/go/bin/go   # go test ./... inside backend/
make all   GO=$HOME/go-sdk/go/bin/go   # cross-compile release binaries -> bin/
```

Go-only checks (fast, no frontend):
```sh
cd backend && $HOME/go-sdk/go/bin/go build ./... && $HOME/go-sdk/go/bin/go vet ./... && $HOME/go-sdk/go/bin/go test ./...
```

## Architecture — respect the layers

- `internal/api` — **HTTP only**. Decode requests, call `service`, encode
  responses, map `service` sentinel errors to status codes. No business logic.
- `internal/service` — **the core**. Owns the active store (swappable at runtime),
  the RNG, plan orchestration, DB-path resolution and switching. Returns typed
  sentinel errors (`ErrEmptyName`, `ErrTooFewWorkers`, `ErrInvalidWeekStart`,
  `ErrWeekExists`); it does not know about HTTP.
- `internal/store` — JSON-file persistence (thread-safe).
- `internal/planner` — zone/shift assignment algorithm (deterministic per seed;
  covered by tests). Reads grid dimensions + the central district from
  `mapconfig`; call `planner.Configure()` after the map is loaded.
- `internal/mapconfig` — loads the street grid from a JSON map file (embedded
  `map.default.json` example, overridable via `MAP_CONFIG` / a `map.json`).
  Populates package-level vars once at startup; keep it read-only afterwards.
- `internal/config` — settings + the data-path pointer file.
- `internal/filedialog` — native OS file pickers (osascript / PowerShell / zenity).
- `internal/web` — `//go:embed all:dist` of the built frontend.

Keep new business logic in `service`, not in `api` handlers.

## The embed constraint (important)

`//go:embed` **cannot** reference files outside its package directory (no `../`).
So the built frontend must be staged at `backend/internal/web/dist/` — it cannot
be moved elsewhere. That folder is gitignored except for a placeholder
`index.html` (kept so the package always compiles). `make frontend`/`make build`/
`make all` regenerate the real assets; `make windows` alone does NOT refresh the
embed.

## Conventions

- **Explicit, self-documenting names** throughout the Go code (e.g.
  `retrieveIdFromURLPath`, `responseWriter`, `frontendFS`) — not idiomatic short
  names like `w`/`r`/`err`. Match the surrounding style when editing.
- No cgo; keep binaries static (`CGO_ENABLED=0`).
- User-facing strings are Spanish (the operators are Spanish-speaking); dev docs
  and code comments are English.
- The map/city is data-only. Keep the code generic — no city-specific street
  names or place references in tracked source. Real, deployment-specific maps
  live under `local/` (gitignored); ship only the generic example map.

## Git / commit rules

- **Only commit when the user explicitly asks.** Do not be proactive about it.
- **Do NOT add a `Co-Authored-By` trailer.**
- Never commit secrets, runtime data (the JSON data file), build artifacts
  (`bin/`, `dist/` assets), `node_modules/`, or `.DS_Store`. These are gitignored.
- Prefer staging specific files over `git add -A`.

## Don't reintroduce

The project was rewritten from a Python/FastAPI + SQLite + React stack into this
Go single binary. There is no Python, no SQLite, no `venv` anymore. If you see
references to those, they are stale.
