package icons

import (
	"bytes"
	"image/png"
	"testing"
)

func TestTrayIconsAreValidPNGs(t *testing.T) {
	for name, data := range map[string][]byte{
		"waiting":      Waiting,
		"recording":    Recording,
		"transcribing": Transcribing,
		"pasted":       Pasted,
	} {
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("%s: not a valid PNG: %v", name, err)
		}
		if img.Bounds().Dx() != 32 || img.Bounds().Dy() != 32 {
			t.Errorf("%s: size = %v, want 32×32", name, img.Bounds())
		}
	}
}

func TestAppIconSizes(t *testing.T) {
	for _, size := range []int{16, 64, 512} {
		img, err := png.Decode(bytes.NewReader(App(size)))
		if err != nil {
			t.Fatalf("App(%d): not a valid PNG: %v", size, err)
		}
		if img.Bounds().Dx() != size || img.Bounds().Dy() != size {
			t.Errorf("App(%d): size = %v", size, img.Bounds())
		}
	}
}
