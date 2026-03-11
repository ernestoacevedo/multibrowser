# multibrowser

A Go CLI for launching multiple Google Chrome instances on macOS, each with its own isolated temporary profile and all opening the same URL.

`multibrowser` is built for local workflows where you need several clean browser sessions at once without manually creating or reusing Chrome profiles.

## Features

- Launches multiple Chrome instances in parallel.
- Creates a separate temporary profile for each instance with `--user-data-dir`.
- Tiles windows across the main display so they remain visible at the same time.
- Displays a terminal status panel powered by `bubbletea`.
- Keeps the CLI in the foreground to manage process lifecycle and cleanup.
- Removes temporary profile directories when browser instances exit.

## Requirements

- macOS
- Go 1.21 or newer
- Google Chrome installed

Default Chrome path used by the CLI:

```text
/Applications/Google Chrome.app/Contents/MacOS/Google Chrome
```

If Chrome is installed somewhere else, use `--chrome-path`.

## Installation

To run it locally:

```bash
go run . open --url https://example.com --count 3
```

To build the binary:

```bash
go build -o multibrowser .
```

Then run:

```bash
./multibrowser open --url https://example.com --count 3
```

To install it into your `PATH` during development:

```bash
go install .
```

## Usage

```bash
multibrowser open --url <url> [--count 3] [--base-name session] [--chrome-path /path/to/chrome]
```

### Flags

- `--url`: URL to open in all Chrome instances. Required.
- `--count`: Number of instances to launch. Default: `3`.
- `--base-name`: Prefix used to name temporary profile directories. Default: `session`.
- `--chrome-path`: Path to the Chrome binary. Default: the standard macOS path.

### Examples

Open three Chrome instances:

```bash
multibrowser open --url https://example.com --count 3
```

Open five instances with a custom base name:

```bash
multibrowser open --url https://news.ycombinator.com --count 5 --base-name work
```

Use a non-standard Chrome installation:

```bash
multibrowser open \
  --url https://example.com \
  --count 2 \
  --chrome-path "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
```

## Behavior

- Each instance uses its own temporary profile.
- Windows are automatically arranged in a grid on the main screen.
- All instances open the same URL in this version.
- The CLI stays running while the managed browser windows are active.
- Press `q` or use `Ctrl+C` to stop the managed session.
- When a process exits, the CLI attempts to delete its temporary profile directory.

## Development

Run tests:

```bash
env GOCACHE=/tmp/gocache GOMODCACHE=/tmp/gomodcache go test ./...
```

Run the race detector for the runner:

```bash
env GOCACHE=/tmp/gocache GOMODCACHE=/tmp/gomodcache go test -race ./internal/runner
```

## Current limitations

- macOS only
- Google Chrome only
- One global URL per execution
- Temporary profiles only; no persistent profiles yet

## Roadmap

- Support additional browsers such as Firefox and Safari
- Add per-instance URL support
- Add persistent profile mode
- Add configuration file support for reusable launch sets
