package avatar

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// makeJPEG returns a w×h JPEG filled with one color, for exercising Process.
func makeJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestProcessNormalizesToSquarePNG(t *testing.T) {
	// A non-square source should come out Size×Size PNG.
	out, err := Process(makeJPEG(t, 640, 400))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	img, format, err := image.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if format != "png" {
		t.Errorf("format = %q, want png", format)
	}
	if b := img.Bounds(); b.Dx() != Size || b.Dy() != Size {
		t.Errorf("size = %dx%d, want %dx%d", b.Dx(), b.Dy(), Size, Size)
	}
	// The result must be a valid PNG round-trip.
	if _, err := png.Decode(bytes.NewReader(out)); err != nil {
		t.Errorf("png.Decode: %v", err)
	}
}

func TestProcessRejectsNonImage(t *testing.T) {
	if _, err := Process([]byte("this is not an image")); err != ErrUnsupported {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}
