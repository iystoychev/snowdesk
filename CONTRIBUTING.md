# Contributing to CokeEverywhere

Thanks for helping make it snow on more Macs! Bug reports, ideas and pull requests are all welcome.

## Reporting a bug

Open an [issue](https://github.com/iystoychev/coke-everywhere/issues/new/choose) with:

- your macOS version and Mac (Apple Silicon or Intel),
- what you did, what you expected, and what happened,
- a screenshot or short screen recording if it is visual.

## Suggesting a feature

Open a feature request issue. Describe what you would like to see and why. Small, delightful effects that stay light on CPU fit best.

## Working on the code

You need macOS, [Go](https://go.dev/dl/) 1.27 or newer and the Xcode Command Line Tools.

```bash
git clone https://github.com/iystoychev/coke-everywhere.git
cd coke-everywhere
make test        # go vet + all tests
go build -o CokeEverywhere . && ./CokeEverywhere   # run from the terminal
make install     # build the app and copy it to /Applications
```

Where things live:

| Path | What it is |
|---|---|
| `snow/` | The simulation in plain Go: flakes, piles, decorations, the countdown. No macOS code, so it is fully unit tested. |
| `snow/draw.go` | Turns each frame into soft dots and colored triangles. New effects only need Go code here and in `snow/`. |
| `overlay_darwin.m` | The Cocoa/Metal side: overlay window, menu, window list, drawing. |
| `login/` | Start at Login (a per-user LaunchAgent). |
| `packaging/` | The app's `Info.plist` and icon. |

### Pull requests

1. Fork the repo and create a branch from `main`.
2. Keep each pull request to one change, and add or update tests in `snow/` or `login/` for behavior you change.
3. Run `make test` and try the app with `make install`.
4. Describe what changed and how you checked it. A before/after screenshot helps for visual changes.

Please keep the app free of permission prompts (no Screen Recording, no Accessibility) and light on CPU and battery.

By contributing you agree that your contributions are licensed under the [MIT License](LICENSE), and to follow the [Code of Conduct](CODE_OF_CONDUCT.md).
