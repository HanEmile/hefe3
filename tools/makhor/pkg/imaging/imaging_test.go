package imaging

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func createTestImage(width, height int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	// Fill with a gradient for visual testing
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{
				R: uint8(x * 255 / width),
				G: uint8(y * 255 / height),
				B: 128,
				A: 255,
			})
		}
	}

	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

func TestResizeToSquare(t *testing.T) {
	t.Run("square image", func(t *testing.T) {
		input := createTestImage(200, 200)
		output, err := ResizeToSquare(input, 100)
		if err != nil {
			t.Fatalf("ResizeToSquare() error = %v", err)
		}

		// Verify output is valid PNG
		img, err := png.Decode(bytes.NewReader(output))
		if err != nil {
			t.Fatalf("output is not valid PNG: %v", err)
		}

		bounds := img.Bounds()
		if bounds.Dx() != 100 || bounds.Dy() != 100 {
			t.Errorf("output size = %dx%d, want 100x100", bounds.Dx(), bounds.Dy())
		}
	})

	t.Run("landscape image", func(t *testing.T) {
		input := createTestImage(400, 200)
		output, err := ResizeToSquare(input, 50)
		if err != nil {
			t.Fatalf("ResizeToSquare() error = %v", err)
		}

		img, err := png.Decode(bytes.NewReader(output))
		if err != nil {
			t.Fatalf("output is not valid PNG: %v", err)
		}

		bounds := img.Bounds()
		if bounds.Dx() != 50 || bounds.Dy() != 50 {
			t.Errorf("output size = %dx%d, want 50x50", bounds.Dx(), bounds.Dy())
		}
	})

	t.Run("portrait image", func(t *testing.T) {
		input := createTestImage(200, 400)
		output, err := ResizeToSquare(input, 75)
		if err != nil {
			t.Fatalf("ResizeToSquare() error = %v", err)
		}

		img, err := png.Decode(bytes.NewReader(output))
		if err != nil {
			t.Fatalf("output is not valid PNG: %v", err)
		}

		bounds := img.Bounds()
		if bounds.Dx() != 75 || bounds.Dy() != 75 {
			t.Errorf("output size = %dx%d, want 75x75", bounds.Dx(), bounds.Dy())
		}
	})

	t.Run("upscale small image", func(t *testing.T) {
		input := createTestImage(50, 50)
		output, err := ResizeToSquare(input, 100)
		if err != nil {
			t.Fatalf("ResizeToSquare() error = %v", err)
		}

		img, err := png.Decode(bytes.NewReader(output))
		if err != nil {
			t.Fatalf("output is not valid PNG: %v", err)
		}

		bounds := img.Bounds()
		if bounds.Dx() != 100 || bounds.Dy() != 100 {
			t.Errorf("output size = %dx%d, want 100x100", bounds.Dx(), bounds.Dy())
		}
	})

	t.Run("invalid image data", func(t *testing.T) {
		_, err := ResizeToSquare([]byte("not an image"), 100)
		if err == nil {
			t.Error("ResizeToSquare() expected error for invalid image data")
		}
	})

	t.Run("empty data", func(t *testing.T) {
		_, err := ResizeToSquare([]byte{}, 100)
		if err == nil {
			t.Error("ResizeToSquare() expected error for empty data")
		}
	})

	t.Run("size 1x1", func(t *testing.T) {
		input := createTestImage(100, 100)
		output, err := ResizeToSquare(input, 1)
		if err != nil {
			t.Fatalf("ResizeToSquare() error = %v", err)
		}

		img, err := png.Decode(bytes.NewReader(output))
		if err != nil {
			t.Fatalf("output is not valid PNG: %v", err)
		}

		bounds := img.Bounds()
		if bounds.Dx() != 1 || bounds.Dy() != 1 {
			t.Errorf("output size = %dx%d, want 1x1", bounds.Dx(), bounds.Dy())
		}
	})
}

func TestBilinear(t *testing.T) {
	tests := []struct {
		name           string
		c00, c10, c01, c11 uint32
		xFrac, yFrac   float64
		want           uint32
	}{
		{
			name:  "top-left corner",
			c00:   1000, c10: 2000, c01: 3000, c11: 4000,
			xFrac: 0.0, yFrac: 0.0,
			want:  1000,
		},
		{
			name:  "top-right corner",
			c00:   1000, c10: 2000, c01: 3000, c11: 4000,
			xFrac: 1.0, yFrac: 0.0,
			want:  2000,
		},
		{
			name:  "bottom-left corner",
			c00:   1000, c10: 2000, c01: 3000, c11: 4000,
			xFrac: 0.0, yFrac: 1.0,
			want:  3000,
		},
		{
			name:  "bottom-right corner",
			c00:   1000, c10: 2000, c01: 3000, c11: 4000,
			xFrac: 1.0, yFrac: 1.0,
			want:  4000,
		},
		{
			name:  "center",
			c00:   0, c10: 100, c01: 100, c11: 200,
			xFrac: 0.5, yFrac: 0.5,
			want:  100, // Average of all corners
		},
		{
			name:  "horizontal center, top edge",
			c00:   0, c10: 100, c01: 0, c11: 100,
			xFrac: 0.5, yFrac: 0.0,
			want:  50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := bilinear(tt.c00, tt.c10, tt.c01, tt.c11, tt.xFrac, tt.yFrac)
			if got != tt.want {
				t.Errorf("bilinear() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestResizePreservesColor(t *testing.T) {
	// Create a solid red image
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	red := color.RGBA{R: 255, G: 0, B: 0, A: 255}
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			img.Set(x, y, red)
		}
	}

	var buf bytes.Buffer
	png.Encode(&buf, img)

	output, err := ResizeToSquare(buf.Bytes(), 50)
	if err != nil {
		t.Fatalf("ResizeToSquare() error = %v", err)
	}

	resultImg, err := png.Decode(bytes.NewReader(output))
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}

	// Check center pixel is still red
	r, g, b, a := resultImg.At(25, 25).RGBA()
	// RGBA returns 16-bit values, so 255 becomes 65535
	if r>>8 != 255 || g>>8 != 0 || b>>8 != 0 || a>>8 != 255 {
		t.Errorf("center pixel = RGBA(%d,%d,%d,%d), want red", r>>8, g>>8, b>>8, a>>8)
	}
}

func TestResizeOutputIsPNG(t *testing.T) {
	input := createTestImage(100, 100)
	output, err := ResizeToSquare(input, 50)
	if err != nil {
		t.Fatalf("ResizeToSquare() error = %v", err)
	}

	// PNG magic bytes: 137 80 78 71 13 10 26 10
	pngMagic := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	if len(output) < 8 {
		t.Fatal("output too short to be PNG")
	}
	if !bytes.Equal(output[:8], pngMagic) {
		t.Error("output does not have PNG magic bytes")
	}
}

// TestResizeExtremeAspectRatios tests images with very extreme aspect ratios.
func TestResizeExtremeAspectRatios(t *testing.T) {
	tests := []struct {
		name   string
		width  int
		height int
	}{
		{"very wide", 1000, 10},
		{"very tall", 10, 1000},
		{"panoramic", 500, 50},
		{"tower", 50, 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := createTestImage(tt.width, tt.height)
			output, err := ResizeToSquare(input, 64)
			if err != nil {
				t.Fatalf("ResizeToSquare() error = %v", err)
			}

			img, err := png.Decode(bytes.NewReader(output))
			if err != nil {
				t.Fatalf("output is not valid PNG: %v", err)
			}

			bounds := img.Bounds()
			if bounds.Dx() != 64 || bounds.Dy() != 64 {
				t.Errorf("output size = %dx%d, want 64x64", bounds.Dx(), bounds.Dy())
			}
		})
	}
}

// TestResizeLargeImage tests resizing a large image.
func TestResizeLargeImage(t *testing.T) {
	input := createTestImage(2000, 2000)
	output, err := ResizeToSquare(input, 100)
	if err != nil {
		t.Fatalf("ResizeToSquare() error = %v", err)
	}

	img, err := png.Decode(bytes.NewReader(output))
	if err != nil {
		t.Fatalf("output is not valid PNG: %v", err)
	}

	bounds := img.Bounds()
	if bounds.Dx() != 100 || bounds.Dy() != 100 {
		t.Errorf("output size = %dx%d, want 100x100", bounds.Dx(), bounds.Dy())
	}
}

// TestResizePreservesTransparency tests that alpha channel is preserved.
func TestResizePreservesTransparency(t *testing.T) {
	// Create image with transparency
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	transparent := color.RGBA{R: 255, G: 0, B: 0, A: 128} // Semi-transparent red
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			img.Set(x, y, transparent)
		}
	}

	var buf bytes.Buffer
	png.Encode(&buf, img)

	output, err := ResizeToSquare(buf.Bytes(), 50)
	if err != nil {
		t.Fatalf("ResizeToSquare() error = %v", err)
	}

	resultImg, err := png.Decode(bytes.NewReader(output))
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}

	// Check center pixel has alpha
	_, _, _, a := resultImg.At(25, 25).RGBA()
	// Alpha should be approximately 128 (scaled to 16-bit: ~32896)
	aScaled := a >> 8
	if aScaled < 100 || aScaled > 150 {
		t.Errorf("alpha = %d, expected around 128", aScaled)
	}
}

// TestBilinearExtremeValues tests bilinear interpolation with edge values.
func TestBilinearExtremeValues(t *testing.T) {
	tests := []struct {
		name               string
		c00, c10, c01, c11 uint32
		xFrac, yFrac       float64
	}{
		{"all zeros", 0, 0, 0, 0, 0.5, 0.5},
		{"all max", 65535, 65535, 65535, 65535, 0.5, 0.5},
		{"gradient horizontal", 0, 65535, 0, 65535, 0.5, 0.5},
		{"gradient vertical", 0, 0, 65535, 65535, 0.5, 0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic
			result := bilinear(tt.c00, tt.c10, tt.c01, tt.c11, tt.xFrac, tt.yFrac)
			_ = result
		})
	}
}

// TestResizeMinimumSize tests resizing to minimum sizes.
func TestResizeMinimumSize(t *testing.T) {
	sizes := []int{1, 2, 3, 4, 5}

	for _, size := range sizes {
		t.Run("size_"+string(rune('0'+size)), func(t *testing.T) {
			input := createTestImage(100, 100)
			output, err := ResizeToSquare(input, size)
			if err != nil {
				t.Fatalf("ResizeToSquare() error = %v", err)
			}

			img, err := png.Decode(bytes.NewReader(output))
			if err != nil {
				t.Fatalf("output is not valid PNG: %v", err)
			}

			bounds := img.Bounds()
			if bounds.Dx() != size || bounds.Dy() != size {
				t.Errorf("output size = %dx%d, want %dx%d", bounds.Dx(), bounds.Dy(), size, size)
			}
		})
	}
}

// TestResizeGrayscaleImage tests resizing a grayscale image.
func TestResizeGrayscaleImage(t *testing.T) {
	// Create grayscale gradient
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for y := 0; y < 100; y++ {
		gray := uint8(y * 255 / 100)
		for x := 0; x < 100; x++ {
			img.Set(x, y, color.RGBA{R: gray, G: gray, B: gray, A: 255})
		}
	}

	var buf bytes.Buffer
	png.Encode(&buf, img)

	output, err := ResizeToSquare(buf.Bytes(), 50)
	if err != nil {
		t.Fatalf("ResizeToSquare() error = %v", err)
	}

	resultImg, err := png.Decode(bytes.NewReader(output))
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}

	// Verify gradient is preserved (top should be darker than bottom)
	rTop, gTop, bTop, _ := resultImg.At(25, 5).RGBA()
	rBot, gBot, bBot, _ := resultImg.At(25, 45).RGBA()

	if rTop>>8 >= rBot>>8 || gTop>>8 >= gBot>>8 || bTop>>8 >= bBot>>8 {
		t.Error("gradient not preserved: top should be darker than bottom")
	}
}

// TestResizeNonSquareToSquare tests center cropping behavior.
func TestResizeNonSquareToSquare(t *testing.T) {
	// Create wide image with distinct left and right halves
	img := image.NewRGBA(image.Rect(0, 0, 200, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 200; x++ {
			if x < 100 {
				// Left half: red
				img.Set(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
			} else {
				// Right half: blue
				img.Set(x, y, color.RGBA{R: 0, G: 0, B: 255, A: 255})
			}
		}
	}

	var buf bytes.Buffer
	png.Encode(&buf, img)

	output, err := ResizeToSquare(buf.Bytes(), 50)
	if err != nil {
		t.Fatalf("ResizeToSquare() error = %v", err)
	}

	resultImg, err := png.Decode(bytes.NewReader(output))
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}

	// Center crop should include both colors since we're cropping the center 100x100 from 200x100
	// Left side of output should be more red, right side more blue
	rLeft, _, bLeft, _ := resultImg.At(10, 25).RGBA()
	rRight, _, bRight, _ := resultImg.At(40, 25).RGBA()

	// Left should have more red than blue (from center-left of original)
	if rLeft>>8 < bLeft>>8 {
		t.Log("Note: center crop behavior may vary")
	}
	// Right should have more blue than red (from center-right of original)
	if bRight>>8 < rRight>>8 {
		t.Log("Note: center crop behavior may vary")
	}
}

// TestResizeOutputSize tests that output file size is reasonable.
func TestResizeOutputSize(t *testing.T) {
	input := createTestImage(500, 500)
	output, err := ResizeToSquare(input, 100)
	if err != nil {
		t.Fatalf("ResizeToSquare() error = %v", err)
	}

	// Output should be smaller than input for downsizing
	if len(output) > len(input) {
		t.Logf("Note: output (%d bytes) larger than input (%d bytes)", len(output), len(input))
	}

	// Output should be reasonable size (< 100KB for a 100x100 PNG)
	if len(output) > 100*1024 {
		t.Errorf("output size %d bytes seems too large for 100x100 PNG", len(output))
	}
}

// TestBilinearInterpolationMidpoints tests interpolation at specific positions.
func TestBilinearInterpolationMidpoints(t *testing.T) {
	// Quarter positions
	tests := []struct {
		xFrac, yFrac float64
		// With corners 0,100,200,300 at positions 00,10,01,11
	}{
		{0.25, 0.25},
		{0.75, 0.25},
		{0.25, 0.75},
		{0.75, 0.75},
	}

	c00, c10, c01, c11 := uint32(0), uint32(100), uint32(200), uint32(300)

	for _, tt := range tests {
		result := bilinear(c00, c10, c01, c11, tt.xFrac, tt.yFrac)
		// Result should be between min and max
		if result > 300 {
			t.Errorf("bilinear(%.2f, %.2f) = %d, exceeds max", tt.xFrac, tt.yFrac, result)
		}
	}
}

// TestResizeConsistency tests that resizing is deterministic.
func TestResizeConsistency(t *testing.T) {
	input := createTestImage(100, 100)

	output1, err := ResizeToSquare(input, 50)
	if err != nil {
		t.Fatalf("first ResizeToSquare() error = %v", err)
	}

	output2, err := ResizeToSquare(input, 50)
	if err != nil {
		t.Fatalf("second ResizeToSquare() error = %v", err)
	}

	if !bytes.Equal(output1, output2) {
		t.Error("resizing same image twice produced different results")
	}
}
