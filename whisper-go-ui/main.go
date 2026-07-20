package main

import (
	"embed"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

// ensureWebKitRenderer disables WebKit's DMABUF renderer when no GPU render
// node is accessible — without this, webkit_web_view_new hangs forever on
// systems where /dev/dri/renderD* exists but is not readable by the user.
func ensureWebKitRenderer() {
	if os.Getenv("WEBKIT_DISABLE_DMABUF_RENDERER") != "" {
		return
	}
	nodes, _ := filepath.Glob("/dev/dri/renderD*")
	for _, n := range nodes {
		if f, err := os.OpenFile(n, os.O_RDWR, 0); err == nil {
			f.Close()
			return // GPU accessible — leave WebKit defaults alone
		}
	}
	os.Setenv("WEBKIT_DISABLE_DMABUF_RENDERER", "1")
}

func main() {
	ensureWebKitRenderer()
	app := NewApp()

	err := wails.Run(&options.App{
		Title:             "Whisper Transcriber",
		Width:             900,
		Height:            640,
		HideWindowOnClose: true, // close button minimizes to tray
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "com.grbit.whisper-go-ui",
			OnSecondInstanceLaunch: func(_ options.SecondInstanceData) {
				runtime.WindowShow(app.ctx)
			},
		},
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
