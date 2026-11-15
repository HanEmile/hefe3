# amalthea verifier

Playwright-driven smoke harness for the WASM build of amalthea.

## Build

```sh
go build -o ./verify ./cmd/verify
```

## Run

```sh
# from anywhere — pass absolute paths
./verify \
  -backend /path/to/result-backend/bin/amalthea-backend \
  -dist    /path/to/result-web
```

`amalthea-backend` is started on a free port with `STATIC_DIR` pointing
at `dist`, then a headless Chromium driven by Playwright runs the checks.

## Checks

| name | what it asserts | hard/soft |
|---|---|---|
| `page_loads` | `Goto(url)` succeeds | hard |
| `title_is_amalthea` | `<title>` is "Amalthea" | hard |
| `canvas_present` | `<canvas id="amalthea-canvas">` exists | hard |
| `wasm_attaches_no_console_error` | only GPU/favicon errors logged | hard |
| `canvas_has_content` | at least one non-fallback pixel | soft |
| `theme_chrome_color` | header stripe matches `theme::CHROME` | soft |

Soft checks are GPU-dependent. Headless Chromium without a real GPU
cannot initialise wgpu/WebGL, so the canvas paints transparent. The
hard checks still verify the WASM binary loads and attaches.

For real-GPU verification run with `-headless=false` (or run the full
`amalthea-web` build behind a browser of your own and inspect manually).

## Adding checks

Each check is a closure inside `cmd/verify/main.go`:

```go
check("name", func() error {
    // page.* operations; return nil on pass, error on fail
    return nil
})
```

Use `softCheck(...)` for assertions that depend on GPU or other
environment quirks.
