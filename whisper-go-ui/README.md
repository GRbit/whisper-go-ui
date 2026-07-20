# whisper-go-ui

Wails v2 desktop GUI for the whisper-paste voice transcriber (Linux/X11).

Press the global hotkey → record microphone → send to a
[whisper-asr-webservice](https://github.com/ahmetoner/whisper-asr-webservice)-compatible
server → paste the transcript into the focused window (clipboard + Ctrl+V).

## Features

- **System tray icon** with four states: gray (waiting), red (recording),
  amber (transcribing), green (pasted, shown for 2 s). Pure-Go D-Bus
  StatusNotifierItem via `fyne.io/systray` — the XFCE panel's status tray
  plugin picks it up natively.
- **Global hotkey** (default `ctrl+shift+r`, toggle mode) via gohook/XRecord —
  passive observation, never conflicts with other apps, hot-swappable from
  Settings without restart.
- **Settings UI**: ASR URL, optional auth header (name + value), language,
  engine, timeout/retries, hotkey with live validation, PortAudio input
  device selection, history storage mode, debug logging.
- **Transcription history**: RAM-only (default) or persisted to
  `~/.local/share/whisper-go-ui/history.jsonl`, capped at 200 entries.
- Window closes to tray; single-instance (second launch focuses the window).

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

Tray icons are committed as PNGs; regenerate with `go generate ./icons`.

## Note on WebKit and GPUs

If no `/dev/dri/renderD*` node is accessible (user not in the `render`
group), WebKitGTK's DMABUF renderer hangs forever inside
`webkit_web_view_new`. The app detects this at startup and sets
`WEBKIT_DISABLE_DMABUF_RENDERER=1` automatically (software rendering — fine
for this UI).
