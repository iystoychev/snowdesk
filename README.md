# snowfall

Snow for your Mac, in Go. Snow falls over every app, builds up on the top edges of windows and on the Dock, and reacts to what you do:

![Snow piled on two windows and the Dock, Christmas lights along the window tops, icicles under them, frosted screen corners and a sparkle trail behind the cursor](docs/screenshot.png)

<sub>Preview rendered by the app's own snow engine on a mock desktop.</sub>

- **Resize a window** and its snow falls off.
- **Fling a window** and its snow flies off at the window's speed.
- **Run the cursor along a pile** to brush the snow off, including off the Dock.

Christmas decorations come with it:

- **Christmas lights** hang in sagging strings along each window's top edge and twinkle in a chasing pattern. They start after the window's traffic-light buttons.
- **Icicles** slowly grow under each window's bottom edge. A resize or fling snaps them off.
- **Wind gusts** blow all the snow sideways every so often.
- **Cursor sparkles**: moving the mouse leaves a short trail of glints.
- **Frosted corners**: frost creeps in from the screen corners over a few minutes, and melts when the cursor goes near.
- **Countdown**: the menu bar item shows the days left until Christmas ("❄︎ 50d"), and a tree on the day.

It needs **no permissions**: no Screen Recording and no Accessibility. It only reads window positions (never titles or contents), the cursor position, and the Dock's preferences.

## Install

You need macOS on Apple Silicon or Intel, [Go](https://go.dev/dl/) 1.27 or newer, and the Xcode Command Line Tools (cgo needs them for the Cocoa and Metal parts):

```bash
xcode-select --install
```

1. Build it:

   ```bash
   git clone https://github.com/iystoychev/coke-everywhere.git
   cd coke-everywhere
   go build -o snowfall .
   ```

2. Put the binary somewhere permanent. Start at Login remembers this path, so pick the final place first:

   ```bash
   mkdir -p ~/.local/bin
   cp snowfall ~/.local/bin/
   ```

3. Start it in the background, so closing the terminal does not stop it:

   ```bash
   nohup ~/.local/bin/snowfall >/dev/null 2>&1 &
   ```

4. Click ❄︎ in the menu bar and turn on **Start at Login**. From the next login on, macOS starts snowfall by itself.

### Update

Build again, quit snowfall from the ❄︎ menu, copy the new binary over the old one (step 2), and start it (step 3). Start at Login keeps working, because the path stays the same.

### Uninstall

1. In the ❄︎ menu, turn off **Start at Login**, then choose **Quit Snow**.
2. Remove the binary and the saved settings:

   ```bash
   rm ~/.local/bin/snowfall
   defaults delete snowfall
   ```

### How Start at Login works

It writes a per-user LaunchAgent, `~/Library/LaunchAgents/local.snowfall.plist`, that runs the binary once when you log in. Turning it off deletes the file; nothing else is changed. If you move the binary, start it once from the new place and the LaunchAgent follows it.

## Use

The overlay passes all clicks through to the apps below it. Moving the mouse also pushes falling flakes around.

The ❄︎ menu bar item holds the settings. They are saved, so they stay after a restart:

| Setting | Choices |
|---|---|
| Snow Amount | Light, Normal, Heavy, Blizzard |
| Flake Size | Small, Normal, Large |
| Opacity | 40%, 70%, 100% |
| Pile Up on Windows | on / off (off drops all piled snow) |
| Decorations | Christmas Lights, Icicles, Wind Gusts, Cursor Sparkles, Frosted Corners — each on / off |

The menu also has Start at Login, Pause and Quit.

Snow and icicles on a window survive switching Spaces and minimizing. They only fall when the window closes, is resized, or is flung.

## How it works

- `snow/` is the simulation in plain Go: flakes, piles (one height column every 3 pt along each window's top edge), occlusion by windows in front, shedding, brushing, settings (`sim.go`), the decorations (`decor.go`) and the countdown (`christmas.go`). `go test ./snow/` covers it.
- `snow/draw.go` turns each frame into two shapes only: soft colored dots (flakes, bulbs, sparkles, frost crystals) and colored triangles (piles, icicles, light wire, frost). New effects need no renderer changes.
- `overlay_darwin.m` is the Cocoa side. It has a transparent, click-through window at status-bar level and reads `CGWindowListCopyWindowInfo` for window bounds. It draws with Metal into a single GPU surface: one instanced draw for the dots and one for the triangles. WindowServer only blends that one surface, so video playback underneath keeps running smoothly.
- `login/` writes and removes the Start at Login LaunchAgent.
- `main.go` connects them. A 60 Hz timer calls the Go `snowStep`, which returns a draw buffer.

## Limits

- Main display only.
- No icicles under the Dock: its bottom edge is the bottom of the screen.
- macOS does not expose the Dock's width without Accessibility permission, so the app estimates it from the Dock's item count and icon size. It may be off by an icon or two. When the Dock is hidden or on the side, there is no Dock ledge.
- The app itself uses about 5% CPU. WindowServer uses roughly 12–15% more while it runs, at any snow amount.
