# multibrowser

Desktop app built with Tauri for launching and managing multiple isolated Google Chrome windows on macOS and Windows.

The app keeps the original project goal intact:

- launch multiple Chrome instances in parallel
- give each instance its own temporary `--user-data-dir`
- tile windows across the main display
- add more instances while the app is running
- close one managed session or stop all of them
- clean temporary profiles when browser processes exit

## Stack

- Tauri v2
- Rust backend for session management, Chrome launching, tiling, and cleanup
- React + TypeScript frontend for the desktop UI

## Requirements

- Node.js 24+
- npm 11+
- Rust/Cargo
- Tauri system prerequisites for your OS
- Google Chrome installed

Supported platforms in this version:

- macOS
- Windows

## Run in development

Install frontend dependencies:

```bash
npm install
```

Start the desktop app:

```bash
npm run tauri dev
```

Or through `make`:

```bash
make desktop-dev
```

## Build

Build the frontend bundle:

```bash
npm run build
```

Build the desktop app:

```bash
npm run tauri build
```

Or through `make`:

```bash
make desktop-build
```

## Desktop behavior

- The launch form accepts one URL for the current batch.
- Chrome is auto-detected from standard install locations.
- You can override the Chrome path and save it as a default.
- Each session is tracked with state, PID, tile bounds, and cleanup errors.
- Existing windows are re-tiled when you add sessions or when one exits.

## Tests

Frontend build check:

```bash
npm run build
```

Rust backend tests:

```bash
cargo test --manifest-path src-tauri/Cargo.toml
```

## Legacy Go code

The original Go CLI remains in the repo as migration reference. The active desktop product is the Tauri app under [`src-tauri`](/Users/ernesto/scripts/multibrowser/src-tauri) and [`src`](/Users/ernesto/scripts/multibrowser/src).
