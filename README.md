# Grid Planner

A desktop scheduling tool for teams that cover the streets of a city's downtown
grid. It divides the grid into four contiguous zones, assigns workers to
shifts and zones for a week, and renders an interactive street map plus
per-worker schedules that can be exported to PDF/JPEG.

The whole application ships as a **single static binary**: a small Go web server
with the React frontend embedded inside it. There is nothing to install — no
runtime, no database engine. Data is stored in one human-readable JSON file
under the OS application-data directory (switchable from the Settings page).

The city grid itself is **data-driven**: a JSON map file defines the streets,
the drawing geometry, and the central district. A generic example map is built
in, so the binary runs out of the box; point it at your own map file to cover a
different city (see [Custom maps](#custom-maps)).

## Layout

```
.
├── Makefile            # build / cross-compile (run from here)
├── map.example.json    # example map file (copy, edit, and load your own)
├── bin/                # compiled binaries (gitignored)
├── backend/            # Go module `gridplanner`
│   ├── main.go         # entry point: loads the map, starts the server
│   └── internal/
│       ├── api/        # HTTP handlers + static SPA serving (thin adapter)
│       ├── service/    # business logic (planning, DB switching) — the core
│       ├── store/      # JSON-file persistence
│       ├── planner/    # zone/shift assignment algorithm
│       ├── mapconfig/  # street-grid loading (+ the embedded example map)
│       ├── config/     # settings + data-path pointer file
│       ├── filedialog/ # native OS file picker (osascript/PowerShell/zenity)
│       └── web/        # //go:embed of the built frontend (dist/)
└── frontend/           # Vite + React single-page app
    └── src/
```

`backend/internal/web/dist/` is an **embed-staging copy** of the built frontend.
Go's `//go:embed` cannot reference files outside its own package, so this copy
must live next to the embedding code. It is gitignored except for a placeholder
`index.html`; `make` regenerates the real assets before compiling.

## Requirements

- **Go 1.26+**
- **Node.js + npm** (to build the frontend)

## Build & run

All targets run from the project root:

```sh
make build     # build the frontend, embed it, compile a binary for this machine
make run       # build everything and launch it
make all       # cross-compile release binaries (Windows/macOS/Linux) into bin/
make test      # run the Go test suite
make clean     # remove build artifacts
```

If `go` is not on your `PATH`, point the Makefile at it:

```sh
make build GO=$HOME/go-sdk/go/bin/go
```

The binary picks a free port, prints the URL, and opens your browser. Set `PORT`
to pin it (e.g. `PORT=8000 ./bin/GridPlanner`).

## Custom maps

The street grid is a JSON file. The app decides which map to use in this order:

1. the path in the `MAP_CONFIG` environment variable, if set;
2. a `map.json` next to the running binary (its working directory);
3. a `map.json` in the application-data directory;
4. the built-in example map (embedded), if none of the above exist.

To use your own city, copy [`map.example.json`](map.example.json), edit it, and
load it:

```sh
MAP_CONFIG=/path/to/your/map.json ./bin/GridPlanner
```

### Map file schema

```jsonc
{
  "label": "My City — Downtown",     // shown as the subtitle in the UI
  "geometry": {                       // drawing constants (pixels/degrees)
    "BW": 70, "BH": 70,               // block width / height
    "SW": 7,  "AVW": 12,              // street width / avenue width
    "HALF": 35,                       // half-block ring thickness
    "MT": 28, "ML": 28,               // margins: top / left
    "MR": 28, "MB": 52,               // margins: right / bottom
    "ANGLE": 35                       // grid bearing vs. true north (deg CW)
  },
  "central": {                        // the always-prioritised central district,
    "columns": [3, 4, 5, 6, 7, 8, 9, 10],  // by block column index (0-based)
    "rows":    [2, 3, 4]                    // by block row index (0-based)
  },
  "horizontal_streets": [             // west-east streets, north to south
    { "name": "Calle A", "av": false },
    { "name": "Av. B",   "av": true  }
    // ...
  ],
  "vertical_streets": [               // north-south streets, west to east
    { "name": "Calle 1", "av": false }
    // ...
  ]
}
```

Notes:

- The block grid is `(vertical_streets − 1)` columns by
  `(horizontal_streets − 1)` rows. The planner splits those blocks into four
  balanced, contiguous zones and keeps the `central` district covered by two
  zones at every hour, so `central` indices must fall inside the grid.
- `av: true` draws a street as a wider avenue.
- The planning rules (four zones, six workers, three shifts) are tuned for a
  grid roughly the size of the example. Very different grid sizes still produce
  a plan but may not hit every balance guarantee.

### Per-team builds (baking a map into the binary)

To hand a team a single self-contained executable with their city already
inside it, bake a map at build time with `MAP=` (and name the binary with
`APP=`):

```sh
# one binary for this machine
make build MAP=maps/teamA.json APP=Coverage-TeamA

# cross-compiled release binaries (Windows/macOS/Linux) for one team
make all   MAP=maps/teamB.json APP=Coverage-TeamB
```

`MAP` temporarily replaces the embedded example for that build only — the
committed example map is restored afterwards, so the working tree stays clean.
Without `MAP`, the built-in generic example is embedded. A baked binary can
still be pointed at a different map at runtime via `MAP_CONFIG` or a `map.json`
beside it (see the resolution order above), so the embedded map is just the
default.

## Development

Run the backend and frontend separately for hot-reload:

```sh
# terminal 1 — backend on :8000
cd backend && PORT=8000 go run .

# terminal 2 — Vite dev server on :5173 (proxies /api to :8000)
cd frontend && npm run dev
```

Open http://127.0.0.1:5173. The Vite dev server proxies `/api/*` to the Go
backend (see `frontend/vite.config.js`).
