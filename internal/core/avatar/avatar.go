// Package avatar normalizes user-uploaded images into a small, square PNG
// suitable for storing in the database and serving as a profile picture.
package avatar

import (
	"bytes"
	"errors"
	"image"
	_ "image/gif"  // register GIF decoder
	_ "image/jpeg" // register JPEG decoder
	"image/png"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // register WEBP decoder
)

// Size is the width and height (in pixels) of a processed avatar.
const Size = 256

// ErrUnsupported is returned when the uploaded bytes can't be decoded as a
// supported image (PNG, JPEG, GIF, or WEBP).
var ErrUnsupported = errors.New("unsupported or corrupt image")

// Process decodes an uploaded image, center-crops it to a square, scales it to
// Size×Size with a high-quality kernel, and re-encodes it as PNG. Normalizing
// to one small format bounds how much is stored per user and keeps serving
// simple (always image/png).
func Process(data []byte) ([]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, ErrUnsupported
	}

	// Center square crop: take the largest centered square of the source.
	b := src.Bounds()
	side := min(b.Dx(), b.Dy())
	ox := b.Min.X + (b.Dx()-side)/2
	oy := b.Min.Y + (b.Dy()-side)/2
	cropped := image.NewRGBA(image.Rect(0, 0, side, side))
	draw.Draw(cropped, cropped.Bounds(), src, image.Pt(ox, oy), draw.Src)

	// Scale the square down to Size×Size.
	dst := image.NewRGBA(image.Rect(0, 0, Size, Size))
	draw.CatmullRom.Scale(dst, dst.Bounds(), cropped, cropped.Bounds(), draw.Over, nil)

	var out bytes.Buffer
	if err := png.Encode(&out, dst); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
