# whisper-paste

Push-to-talk / toggle-to-talk dictation tool.  
Press a hotkey → speak → release (or press again) → transcript is instantly pasted into the focused window.

Uses a **remote** [whisper-asr-webservice](https://github.com/ahmetoner/whisper-asr-webservice) for transcription — no local GPU required.

---

## How it works

```
hotkey ──► record mic to buffer
        ──► hotkey again (toggle) or release (hold)
        ──► write 16-kHz mono WAV to /tmp
        ──► POST /asr to remote server
        ──► receive plain-text transcript
        ──► write to clipboard + send Ctrl+V to focused window
```

---

## Prerequisites

| Requirement | Notes |
|---|---|
| Go 1.21+ | `go build` |
| `libportaudio2` | `sudo apt install libportaudio2 portaudio19-dev` |
| `libx11-dev`, `libxtst-dev`, `libxkbcommon-dev` | needed by robotgo/gohook — `sudo apt install libx11-dev libxtst-dev libxkbcommon-dev` |
| A running [whisper-asr-webservice](https://github.com/ahmetoner/whisper-asr-webservice) | Local or remote |

### Start the ASR server (Docker example)

```bash
docker run -p 9000:9000 \
  -e ASR_MODEL=base \
  -e ASR_ENGINE=faster_whisper \
  onerahmet/openai-whisper-asr-webservice:latest-gpu
```

---

## Build & run

```bash
# Install system deps (Debian/Ubuntu)
sudo apt install libportaudio2 portaudio19-dev \
                 libx11-dev libxtst-dev libxkbcommon-dev

# Fetch Go dependencies
go mod tidy

# Build
go build -o whisper-paste .

# Run (simplest — uses defaults)
./whisper-paste
```

---

## Configuration

All options can be set as **flags** or **environment variables**.  
Flag value always wins; env wins over the built-in default.

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `-url` | `ASR_URL` | `http://localhost:9000` | ASR server base URL |
| `-language` | `ASR_LANGUAGE` | *(auto-detect)* | ISO-639-1 code, e.g. `en`, `ru` |
| `-engine` | `ASR_ENGINE` | `faster_whisper` | `faster_whisper` or `openai_whisper` |
| `-timeout` | `ASR_TIMEOUT` | `60` | Per-request timeout (seconds) |
| `-retries` | `ASR_RETRIES` | `3` | Retry count with exponential back-off |
| `-hotkey` | `HOTKEY` | `ctrl+shift+r` | Key combo — see key names below |
| `-mode` | `HOTKEY_MODE` | `toggle` | `toggle` or `hold` |
| `-device` | `DEVICE_ID` | *(auto)* | Audio device index for this run only |
| `-set-audio-input N` | — | — | Save device N to `~/.config/go-desktop-transcriber/device` and exit |
| `-list-audio-input` | — | — | Print all input devices and exit |
| `-tempdir` | `TEMP_DIR` | OS temp dir | Directory for temporary WAV files |
| `-debug` | `DEBUG=1` | `false` | Verbose debug logging + VU meter |

### Hotkey mode

| Mode | Behaviour |
|------|-----------|
| `toggle` | First press starts recording; second press stops it and triggers ASR. |
| `hold` | Hold the hotkey to record; release any key in the combo to stop. |

### Valid key names

Modifiers: `ctrl`, `shift`, `alt`, `super` / `win` / `meta`  
Letters: `a`–`z`  
Digits: `0`–`9`  
Function keys: `f1`–`f12`  
Other: `space`, `tab`, `enter`, `backspace`, `escape`, `delete`, `insert`, `home`, `end`, `pageup`, `pagedown`, `up`, `down`, `left`, `right`

### Device selection (saved across runs)

```bash
# List all input devices
./whisper-paste -list-audio-input

# Save device 3 for future runs (written to ~/.config/go-desktop-transcriber/device)
./whisper-paste -set-audio-input 3

# Override for a single run
./whisper-paste -device 5
```

---

## Examples

```bash
# Default — toggle mode, auto language, localhost ASR
./whisper-paste

# Hold mode — hold Ctrl+Alt+Space to record
./whisper-paste -mode hold -hotkey "ctrl+alt+space"

# Remote ASR, Russian, toggle with Win+Shift+T
./whisper-paste \
  -url http://192.168.1.50:9000 \
  -language ru \
  -hotkey "super+shift+t" \
  -mode toggle

# Debug mode — shows VU meter and all internal state transitions
./whisper-paste -debug
```

---

## Files

| File | Purpose |
|------|---------|
| `main.go` | Entry point, global state machine, hotkey event loop |
| `config.go` | Flag + env loading, hotkey string parser, key→rawcode table |
| `recorder.go` | Microphone capture via PortAudio, WAV file writer |
| `asr.go` | HTTP POST client for the `/asr` endpoint |
| `audio_utils.go` | Device selection, list, save/load |
| `paste.go` | Clipboard write + Ctrl+V |
