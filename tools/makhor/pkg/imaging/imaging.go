// Package imaging provides image processing utilities for avatar handling.
package imaging

import (
	"bytes"
	"image"
	"image/color"
	_ "image/gif"  // Register GIF decoder
	_ "image/jpeg" // Register JPEG decoder
	"image/png"
)

// ResizeToSquare decodes an image, crops it to a center square, and resizes
// it to the specified size. Returns PNG-encoded bytes.
func ResizeToSquare(data []byte, size int) ([]byte, error) {
	// Decode the image
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	// Get source bounds
	srcBounds := img.Bounds()
	srcW := srcBounds.Dx()
	srcH := srcBounds.Dy()

	// Calculate crop region (center crop to square)
	var cropX, cropY, cropSize int
	if srcW > srcH {
		cropSize = srcH
		cropX = (srcW - srcH) / 2
		cropY = 0
	} else {
		cropSize = srcW
		cropX = 0
		cropY = (srcH - srcW) / 2
	}

	// Create destination image
	dst := image.NewRGBA(image.Rect(0, 0, size, size))

	// Bilinear interpolation scaling with center crop
	for dy := 0; dy < size; dy++ {
		for dx := 0; dx < size; dx++ {
			// Map destination pixel to source coordinates (within crop region)
			srcXf := float64(dx) * float64(cropSize-1) / float64(size-1)
			srcYf := float64(dy) * float64(cropSize-1) / float64(size-1)

			// Add crop offset
			srcXf += float64(cropX + srcBounds.Min.X)
			srcYf += float64(cropY + srcBounds.Min.Y)

			// Get integer and fractional parts
			x0 := int(srcXf)
			y0 := int(srcYf)
			xFrac := srcXf - float64(x0)
			yFrac := srcYf - float64(y0)

			// Clamp coordinates
			x1 := x0 + 1
			y1 := y0 + 1
			if x1 >= srcBounds.Max.X {
				x1 = srcBounds.Max.X - 1
			}
			if y1 >= srcBounds.Max.Y {
				y1 = srcBounds.Max.Y - 1
			}

			// Get four neighboring pixels
			c00 := img.At(x0, y0)
			c10 := img.At(x1, y0)
			c01 := img.At(x0, y1)
			c11 := img.At(x1, y1)

			// Bilinear interpolation
			r00, g00, b00, a00 := c00.RGBA()
			r10, g10, b10, a10 := c10.RGBA()
			r01, g01, b01, a01 := c01.RGBA()
			r11, g11, b11, a11 := c11.RGBA()

			// Interpolate
			r := bilinear(r00, r10, r01, r11, xFrac, yFrac)
			g := bilinear(g00, g10, g01, g11, xFrac, yFrac)
			b := bilinear(b00, b10, b01, b11, xFrac, yFrac)
			a := bilinear(a00, a10, a01, a11, xFrac, yFrac)

			dst.Set(dx, dy, color.RGBA{
				R: uint8(r >> 8),
				G: uint8(g >> 8),
				B: uint8(b >> 8),
				A: uint8(a >> 8),
			})
		}
	}

	// Encode to PNG
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// bilinear performs bilinear interpolation between four values.
func bilinear(c00, c10, c01, c11 uint32, xFrac, yFrac float64) uint32 {
	// Interpolate horizontally
	top := float64(c00)*(1-xFrac) + float64(c10)*xFrac
	bottom := float64(c01)*(1-xFrac) + float64(c11)*xFrac
	// Interpolate vertically
	return uint32(top*(1-yFrac) + bottom*yFrac)
}
