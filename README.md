# whisper-go-ui

Wails v2 desktop GUI for the whisper-paste voice transcriber (Linux/X11).

Press the global hotkey → record microphone → send to a
[whisper-asr-webservice](https://github.com/ahmetoner/whisper-asr-webservice)-compatible
server → paste the transcript into the focused window (clipboard + Ctrl+V).

## Features

- **System tray icon** with four states: gray (waiting), red (recording),
  amber (transcribing), green (pasted, shown for 2 s). Pure-Go D-Bus
  StatusNotifierItem via `fyne.io/systray`, the XFCE panel's status tray
  plugin picks it up natively.
- **Global hotkey** (default `ctrl+shift+r`, toggle mode) via gohook/XRecord —
  passive observation, never conflicts with other apps, hot-swappable from
  Settings without restart.
- **Settings UI**: ASR URL, optional auth header (name + value), language,
  engine, timeout/retries, hotkey with live validation and a Capture button
  (press the combo instead of typing it), PortAudio input device selection,
  history storage mode, debug logging.
- **Paste behaviour**: independent toggles for copying the recognized text
  to the clipboard and for pasting it instantly; the paste keystroke can be
  Ctrl+V or Ctrl+Shift+V (terminals). With clipboard-copy off, the previous
  clipboard text is restored after the paste.
- **Transcription history**: RAM-only (default) or persisted to
  `~/.local/share/whisper-go-ui/history.jsonl`, capped at 200 entries.
- Window closes to tray; single-instance (second launch focuses the window).
- Window Help menu with usage instructions, credits, and Quit.

## Command line

Only one copy of the app runs at a time (single-instance lock):

```
whisper-go-ui                     start, or bring the running window to the front
whisper-go-ui --toggle-recording  toggle recording exactly like the hotkey:
                                  first call starts, the next stops and
                                  copies/pastes the transcript; starts the app
                                  recording if it is not running yet
whisper-go-ui --help              print this help
```

### Hotkey via your DE instead of the built-in one

The built-in hotkey observes the keyboard globally (gohook/XRecord). If you
prefer your desktop environment to own the shortcut, tick "Off" next to the
hotkey in Settings and bind a keyboard shortcut in your DE's keyboard
settings to `whisper-go-ui --toggle-recording`. Keeping both active would
toggle twice per press, which is why the checkbox suppresses the in-app
hotkey.

## Configuration

`~/.config/whisper-go-ui/config.json` (0600 — the auth header value is stored
in plain text). Backward-compatible with the schema of the previous app
version; new fields get defaults. Edited from the Settings tab.

## Build

Requires GTK3 + webkit2gtk-4.1, PortAudio, libx11/libxtst (robotgo/gohook):

```sh
wails build -tags webkit2_41
./build/bin/whisper-go-ui
```

Development mode: `wails dev -tags webkit2_41`.

Tests and vet (tag required — the wails Linux frontend needs it to compile):

```sh
go vet -tags webkit2_41 ./...
go test -tags webkit2_41 -race ./...
```

All icons (tray states, window/taskbar icon) are rendered at runtime by the
`icons` package — no image files are committed. A pre-build hook in
`wails.json` writes the generated PNGs to `build/icons/` (hicolor set for
`make install`) and `build/appicon.png` (wails packaging) on every build;
both paths are gitignored build artifacts.

## Desktop integration (optional)

The app sets its window icon (`_NET_WM_ICON`), which titlebars use — but
most taskbar/dock plugins instead resolve icons by matching the window's
WM_CLASS (`whisper-go-ui`) against a `.desktop` entry and looking the icon up
in the icon theme at small fixed sizes. The app deliberately does **not**
install anything into your home directory; run `make install` (or
`make uninstall`) to apply exactly the steps below, or run them manually
from the repository root:

```sh
# the binary (build first: wails build -tags webkit2_41)
mkdir -p ~/.local/bin
cp build/bin/whisper-go-ui ~/.local/bin/whisper-go-ui

# icons for the hicolor theme (rendered into build/icons/ by the build)
for size in 16 22 24 32 48 64 128 256 512; do
  mkdir -p ~/.local/share/icons/hicolor/${size}x${size}/apps
  cp build/icons/${size}.png ~/.local/share/icons/hicolor/${size}x${size}/apps/whisper-go-ui.png
done

# application menu / taskbar entry
mkdir -p ~/.local/share/applications
cat > ~/.local/share/applications/whisper-go-ui.desktop <<EOF
[Desktop Entry]
Type=Application
Name=Whisper Transcriber
Comment=Voice transcription with hotkey paste
Exec=$HOME/.local/bin/whisper-go-ui
Icon=whisper-go-ui
Terminal=false
Categories=Utility;AudioVideo;
StartupWMClass=whisper-go-ui
EOF
```

After a rebuild, refresh the installed binary with the same `cp`.

If `~/.local/share/icons/hicolor/icon-theme.cache` exists on your system,
refresh it too (a stale cache hides new icons):
`gtk-update-icon-cache --force --ignore-theme-index ~/.local/share/icons/hicolor`

To undo everything:

```sh
rm -f ~/.local/bin/whisper-go-ui
rm -f ~/.local/share/applications/whisper-go-ui.desktop
rm -f ~/.local/share/icons/hicolor/*/apps/whisper-go-ui.png
```

## Note on WebKit and GPUs

If no `/dev/dri/renderD*` node is accessible (user not in the `render`
group), WebKitGTK's DMABUF renderer hangs forever inside
`webkit_web_view_new`. The app detects this at startup and sets
`WEBKIT_DISABLE_DMABUF_RENDERER=1` automatically (software rendering — fine
for this UI).
