// Command gen renders the four tray-state icons as 32×32 PNGs.
// Run via: go generate ./icons
package main

import (
	"fmt"
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
	if err := writeAppIcon("../build/appicon.png", 512); err != nil {
		log.Fatalf("appicon: %v", err)
	}
	log.Printf("wrote ../build/appicon.png")

	// Pre-rendered sizes for manual installation into the hicolor icon
	// theme (see README) — taskbar/dock plugins look icons up at small
	// fixed sizes and don't all scale a larger one down.
	for _, size := range []int{16, 22, 24, 32, 48, 64, 128, 256, 512} {
		path := fmt.Sprintf("../build/icons/%d.png", size)
		if err := writeAppIcon(path, size); err != nil {
			log.Fatalf("%s: %v", path, err)
		}
		log.Printf("wrote %s", path)
	}
}

// writeAppIcon draws the window/taskbar icon: a white microphone silhouette
// with a red record dot on a dark rounded-square background. The geometry is
// authored in 512×512 coordinates and scaled to the requested size.
func writeAppIcon(path string, size int) error {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	scale := float64(size) / 512

	bg := color.NRGBA{0x21, 0x24, 0x2c, 0xff}    // panel dark
	fg := color.NRGBA{0xe6, 0xe8, 0xee, 0xff}    // light text
	red := color.NRGBA{0xe0, 0x34, 0x2c, 0xff}   // recording red

	// coverage from a signed distance in authoring units (1 device px soft edge)
	cov := func(d float64) float64 {
		return math.Min(1, math.Max(0, 0.5-d*scale))
	}
	// signed distance to a rounded rectangle centered at (cx,cy)
	roundRect := func(px, py, cx, cy, hw, hh, r float64) float64 {
		dx := math.Abs(px-cx) - (hw - r)
		dy := math.Abs(py-cy) - (hh - r)
		ax, ay := math.Max(dx, 0), math.Max(dy, 0)
		return math.Hypot(ax, ay) + math.Min(math.Max(dx, dy), 0) - r
	}
	circle := func(px, py, cx, cy, r float64) float64 {
		return math.Hypot(px-cx, py-cy) - r
	}

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			// sample position in 512×512 authoring coordinates
			px, py := (float64(x)+0.5)/scale, (float64(y)+0.5)/scale

			// background rounded square
			a := cov(roundRect(px, py, 256, 256, 240, 240, 96))
			if a <= 0 {
				continue
			}
			c := bg

			// microphone: capsule body
			mic := roundRect(px, py, 256, 200, 62, 96, 62)
			// U-shaped holder: lower half of a ring around the capsule
			if py > 232 {
				ring := math.Abs(circle(px, py, 256, 232, 108)) - 13
				mic = math.Min(mic, ring)
			}
			// stem and base
			mic = math.Min(mic, roundRect(px, py, 256, 372, 13, 34, 12))
			mic = math.Min(mic, roundRect(px, py, 256, 408, 74, 13, 12))
			if ma := cov(mic); ma > 0 {
				c = blend(c, fg, ma)
			}

			// record dot, top-right
			if da := cov(circle(px, py, 398, 118, 44)); da > 0 {
				c = blend(c, red, da)
			}

			c.A = uint8(a * 255)
			img.SetNRGBA(x, y, c)
		}
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// blend mixes b over a with opacity t (0..1), ignoring alpha.
func blend(a, b color.NRGBA, t float64) color.NRGBA {
	mix := func(x, y uint8) uint8 {
		return uint8(float64(x)*(1-t) + float64(y)*t)
	}
	return color.NRGBA{mix(a.R, b.R), mix(a.G, b.G), mix(a.B, b.B), a.A}
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
