// Command gen writes the app icon PNGs used outside the binary: the hicolor
// icon-theme set (build/icons/<size>.png, installed by `make install`) and
// build/appicon.png (wails packaging). The running app renders its own icons
// in memory via the icons package — these files are build artifacts only.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"whisper-go-ui/icons"
)

// hicolor icon-theme sizes; taskbar/dock plugins look icons up at small
// fixed sizes and don't all scale a larger one down.
var sizes = []int{16, 22, 24, 32, 48, 64, 128, 256, 512}

func main() {
	out := flag.String("out", "build", "output directory (the project's build/ dir)")
	flag.Parse()

	for _, size := range sizes {
		path := filepath.Join(*out, "icons", fmt.Sprintf("%d.png", size))
		write(path, icons.App(size))
	}
	write(filepath.Join(*out, "appicon.png"), icons.App(512))
}

func write(path string, data []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Fatalf("%s: %v", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Fatalf("%s: %v", path, err)
	}
	log.Printf("wrote %s", path)
}
