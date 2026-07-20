// Command gen renders the four tray-state icons as 32×32 PNGs.
// Run via: go generate ./icons
package main

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
)

type spec struct {
	file string
	fill color.NRGBA
}

func main() {
	specs := []spec{
		{"waiting.png", color.NRGBA{0x8a, 0x8a, 0x8a, 0xff}},      // gray
		{"recording.png", color.NRGBA{0xe0, 0x34, 0x2c, 0xff}},    // red
		{"transcribing.png", color.NRGBA{0xf0, 0xa0, 0x30, 0xff}}, // amber
		{"pasted.png", color.NRGBA{0x2e, 0xa0, 0x43, 0xff}},       // green
	}
	for _, s := range specs {
		if err := writeIcon(s.file, s.fill); err != nil {
			log.Fatalf("%s: %v", s.file, err)
		}
		log.Printf("wrote %s", s.file)
	}
}

// writeIcon draws an anti-aliased filled circle on a transparent background.
func writeIcon(path string, fill color.NRGBA) error {
	const size = 32
	const cx, cy = size / 2.0, size / 2.0
	const radius = 12.0

	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			d := math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy)
			// 1px soft edge for anti-aliasing
			var a float64
			switch {
			case d <= radius-0.5:
				a = 1
			case d >= radius+0.5:
				a = 0
			default:
				a = radius + 0.5 - d
			}
			if a > 0 {
				c := fill
				c.A = uint8(a * float64(fill.A))
				img.SetNRGBA(x, y, c)
			}
		}
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
