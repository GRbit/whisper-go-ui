package main

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"

	"whisper-go-ui/icons"
)

const usageText = `Whisper Transcriber - voice transcription with hotkey paste.

Usage:
  whisper-go-ui [options]

Options:
  --toggle-recording   Toggle recording, exactly like the in-app global
                       hotkey: the first call starts recording, the next one
                       stops it and copies/pastes the transcript. Works on the
                       already-running app; if the app is not running yet, it
                       starts and begins recording right away.
  -h, --help           Show this help and exit.

Single instance:
  Only one copy of the app runs. Launching whisper-go-ui again (without
  options) brings the existing window to the front instead of starting a
  second instance.

Global hotkey via your desktop environment:
  Instead of the app's built-in hotkey you can tick "Off" next to the hotkey
  in Settings and, in your desktop environment's keyboard shortcut settings,
  bind a shortcut to:
      whisper-go-ui --toggle-recording
`

// cliOptions holds the flags parsed from the command line. Unknown arguments
// are ignored.
type cliOptions struct {
	help            bool
	toggleRecording bool
}

// parseArgs scans command-line arguments (also used for the arguments a
// second instance was launched with).
func parseArgs(args []string) cliOptions {
	var o cliOptions
	for _, a := range args {
		switch strings.ToLower(a) {
		case "--help", "-h", "-help":
			o.help = true
		case "--toggle-recording", "-toggle-recording":
			o.toggleRecording = true
		}
	}
	return o
}

//go:embed all:frontend/dist
var assets embed.FS

// ensureWebKitRenderer disables WebKit's DMABUF renderer when no GPU render
// node is accessible: without this, webkit_web_view_new hangs forever on
// systems where /dev/dri/renderD* exists but is not readable by the user.
func ensureWebKitRenderer() {
	if os.Getenv("WEBKIT_DISABLE_DMABUF_RENDERER") != "" {
		return
	}
	nodes, _ := filepath.Glob("/dev/dri/renderD*")
	for _, n := range nodes {
		if f, err := os.OpenFile(n, os.O_RDWR, 0); err == nil {
			f.Close()
			return // GPU accessible: leave WebKit defaults alone
		}
	}
	os.Setenv("WEBKIT_DISABLE_DMABUF_RENDERER", "1")
}

func main() {
	opts := parseArgs(os.Args[1:])
	if opts.help {
		fmt.Print(usageText)
		return
	}

	ensureWebKitRenderer()
	app := NewApp()
	app.toggleOnLaunch = opts.toggleRecording

	err := wails.Run(&options.App{
		Title:             "Whisper Transcriber",
		Width:             900,
		Height:            640,
		HideWindowOnClose: true, // close button hides to tray
		Linux: &linux.Options{
			Icon:        icons.App(512), // window manager / taskbar icon
			ProgramName: "whisper-go-ui",
			// Keep the default that applies when options.Linux is nil:
			// GPU compositing is what hangs WebKit on this machine.
			WebviewGpuPolicy: linux.WebviewGpuPolicyNever,
		},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               "com.grbit.whisper-go-ui",
			OnSecondInstanceLaunch: app.onSecondInstance,
		},
		Menu: app.appMenu(),
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 24, G: 26, B: 32, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
