package hq2x

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"image/color"
	"runtime"
	"sync"
	"testing"
)

var (
	red  = color.RGBA{R: 0xff, A: 0xff}
	blue = color.RGBA{B: 0xff, A: 0xff}
	// marker is a transparent pixel: alpha zero, but with a non-zero color
	// that would show up if the code wrote source colors into blank space.
	marker = color.RGBA{R: 0x12, G: 0x34, B: 0x56, A: 0x00}
	blank  = color.RGBA{}
)

func newAtlas() *image.RGBA {
	// 8x4 atlas: two adjacent 4x4 frames. Each frame is a solid color with a
	// single transparent pixel in its top-left corner.
	img := image.NewRGBA(image.Rect(0, 0, 8, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 8; x++ {
			c := red
			if x >= 4 {
				c = blue
			}
			img.SetRGBA(x, y, c)
		}
	}
	img.SetRGBA(0, 0, marker)
	img.SetRGBA(4, 0, marker)
	return img
}

// frameAt returns the 2x2 block produced for the source pixel (x, y) whose
// block starts at doubled coordinates.
func frameAt(dest *image.RGBA, x, y int) [4]color.RGBA {
	dx, dy := 2*x, 2*y
	return [4]color.RGBA{
		dest.RGBAAt(dx, dy),
		dest.RGBAAt(dx+1, dy),
		dest.RGBAAt(dx, dy+1),
		dest.RGBAAt(dx+1, dy+1),
	}
}

func TestHQ2xGridAdjacentFramesDoNotBleed(t *testing.T) {
	src := newAtlas()
	got, err := HQ2xGrid(src, 4, 4, 0)
	if err != nil {
		t.Fatalf("HQ2xGrid: %v", err)
	}
	if want := image.Rect(0, 0, 16, 8); got.Bounds() != want {
		t.Fatalf("bounds = %v, want %v", got.Bounds(), want)
	}

	// The outer edge of the red frame must be pure red. A whole-sheet HQ2x
	// run samples the blue frame across the seam; that must not happen here.
	for y := 1; y < 4; y++ {
		for _, p := range frameAt(got, 0, y) {
			if p != red {
				t.Errorf("red left edge at y=%d = %v, want %v", y, p, red)
			}
		}
	}
	// The outer edge of the blue frame must be pure blue.
	for y := 1; y < 4; y++ {
		for _, p := range frameAt(got, 7, y) {
			if p != blue {
				t.Errorf("blue right edge at y=%d = %v, want %v", y, p, blue)
			}
		}
	}
	// Both sides of the seam are flat, so neither may show a blend.
	for _, tc := range []struct {
		x    int
		want color.RGBA
	}{{3, red}, {4, blue}} {
		for y := 0; y < 4; y++ {
			if y == 0 {
				continue // transparent marker on the first row
			}
			for _, p := range frameAt(got, tc.x, y) {
				if p != tc.want {
					t.Errorf("seam pixel (%d,%d) = %v, want %v", tc.x, y, p, tc.want)
				}
			}
		}
	}
}

func TestHQ2xGridTransparentCornerStaysTransparent(t *testing.T) {
	src := newAtlas()
	got, err := HQ2xGrid(src, 4, 4, 0)
	if err != nil {
		t.Fatalf("HQ2xGrid: %v", err)
	}

	for _, cellX := range []int{0, 4} {
		// The transparent corner of each frame expands to a fully blank 2x2
		// block, with no marker color leaking in.
		for _, p := range frameAt(got, cellX, 0) {
			if p != blank {
				t.Errorf("transparent corner of cell at x=%d = %v, want zero", cellX, p)
			}
		}
		// Its right neighbor is opaque and keeps its alpha.
		for _, p := range frameAt(got, cellX+1, 0) {
			if p.A != 0xff {
				t.Errorf("neighbor of transparent corner at x=%d has alpha %d, want 255",
					cellX+1, p.A)
			}
		}
	}
}

func TestHQ2xGridAlphaIsPreserved(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			src.SetRGBA(x, y, color.RGBA{R: 0xff, A: 0x80})
		}
	}
	got, err := HQ2xGrid(src, 4, 4, 0)
	if err != nil {
		t.Fatalf("HQ2xGrid: %v", err)
	}
	for _, p := range frameAt(got, 1, 1) {
		if p.A != 0x80 {
			t.Fatalf("alpha = %d, want 128", p.A)
		}
	}
}

func TestHQ2xGridMatchesPerCellHQ2x(t *testing.T) {
	// Each cell must obey exactly the same rules as HQ2x run on that cell
	// alone. Alpha differs by design: HQ2x forces 255, grid mode preserves
	// the source alpha, so compare colors only and check alpha separately.
	src := newAtlas()
	got, err := HQ2xGrid(src, 4, 4, 0)
	if err != nil {
		t.Fatalf("HQ2xGrid: %v", err)
	}

	for cell := 0; cell < 2; cell++ {
		sub := image.NewRGBA(image.Rect(0, 0, 4, 4))
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				sub.SetRGBA(x, y, src.RGBAAt(cell*4+x, y))
			}
		}
		want, err := HQ2x(sub)
		if err != nil {
			t.Fatalf("HQ2x cell %d: %v", cell, err)
		}
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				g := got.RGBAAt(cell*8+2*x, 2*y)
				if c := src.RGBAAt(cell*4+x, y); c.A == 0 {
					if g != blank {
						t.Fatalf("cell %d transparent pixel (%d,%d) = %v, want zero",
							cell, x, y, g)
					}
					continue
				}
				w := want.RGBAAt(2*x, 2*y)
				if g.R != w.R || g.G != w.G || g.B != w.B {
					t.Fatalf("cell %d pixel (%d,%d) = %v, want color %v",
						cell, x, y, g, w)
				}
			}
		}
	}
}

func TestHQ2xGridMarginOffsetsFrames(t *testing.T) {
	// 12x8 sheet with a 2px margin around a row of two 4x4 frames at source
	// cell origins (2,2) and (6,2).
	src := image.NewRGBA(image.Rect(0, 0, 12, 8))
	for y := 2; y < 6; y++ {
		for x := 2; x < 10; x++ {
			c := red
			if x >= 6 {
				c = blue
			}
			src.SetRGBA(x, y, c)
		}
	}

	got, err := HQ2xGrid(src, 4, 4, 2)
	if err != nil {
		t.Fatalf("HQ2xGrid: %v", err)
	}
	if want := image.Rect(0, 0, 24, 16); got.Bounds() != want {
		t.Fatalf("bounds = %v, want %v", got.Bounds(), want)
	}

	// The margin doubles to 4px and stays blank even though a frame is only
	// two pixels away.
	for y := 0; y < 16; y++ {
		for x := 0; x < 24; x++ {
			if x >= 4 && x < 20 && y >= 4 && y < 12 {
				continue // inside the doubled frames
			}
			if p := got.RGBAAt(x, y); p != blank {
				t.Fatalf("margin pixel (%d,%d) = %v, want zero", x, y, p)
			}
		}
	}

	// Frame coordinates: doubled source position; every pixel of a flat
	// frame is that frame's color.
	for srcX := 2; srcX < 10; srcX++ {
		want := red
		if srcX >= 6 {
			want = blue
		}
		if px := 2 * srcX; got.RGBAAt(px, 4) != want {
			t.Fatalf("frame at source x=%d starts at %d = %v, want %v",
				srcX, px, got.RGBAAt(px, 4), want)
		}
		for y := 2; y < 6; y++ {
			for _, p := range frameAt(got, srcX, y) {
				if p != want {
					t.Fatalf("frame pixel src=(%d,%d) = %v, want %v", srcX, y, p, want)
				}
			}
		}
	}
}

func TestHQ2xGridNonDivisibleKeepsPartialCell(t *testing.T) {
	// 6x4 with 4x4 cells: one full cell and a trailing 2px-wide partial
	// cell. The partial cell is scaled on its own rather than dropped or
	// padded with samples from the neighboring frame.
	src := image.NewRGBA(image.Rect(0, 0, 6, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 6; x++ {
			c := red
			if x >= 4 {
				c = blue
			}
			src.SetRGBA(x, y, c)
		}
	}

	got, err := HQ2xGrid(src, 4, 4, 0)
	if err != nil {
		t.Fatalf("HQ2xGrid: %v", err)
	}
	if want := image.Rect(0, 0, 12, 8); got.Bounds() != want {
		t.Fatalf("bounds = %v, want %v", got.Bounds(), want)
	}
	for y := 0; y < 8; y++ {
		for x := 0; x < 12; x++ {
			want := red
			if x >= 8 {
				want = blue
			}
			if p := got.RGBAAt(x, y); p != want {
				t.Fatalf("pixel (%d,%d) = %v, want %v", x, y, p, want)
			}
		}
	}
}

func TestHQ2xGridNonZeroBounds(t *testing.T) {
	base := image.NewRGBA(image.Rect(0, 0, 8, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 8; x++ {
			c := red
			if x >= 4 {
				c = blue
			}
			base.SetRGBA(x, y, c)
		}
	}
	// The sub-image has Min=(1,0), a red 3-wide cell then a blue 3-wide cell.
	sub := base.SubImage(image.Rect(1, 0, 7, 3)).(*image.RGBA)

	got, err := HQ2xGrid(sub, 3, 3, 0)
	if err != nil {
		t.Fatalf("HQ2xGrid: %v", err)
	}
	if want := image.Rect(0, 0, 12, 6); got.Bounds() != want {
		t.Fatalf("bounds = %v, want %v", got.Bounds(), want)
	}
	for y := 0; y < 6; y++ {
		for x := 0; x < 12; x++ {
			want := red
			if x >= 6 {
				want = blue
			}
			if p := got.RGBAAt(x, y); p != want {
				t.Fatalf("sub-image pixel (%d,%d) = %v, want %v", x, y, p, want)
			}
		}
	}
}

func TestHQ2xGridInvalidInputs(t *testing.T) {
	src := newAtlas()
	tests := []struct {
		name   string
		src    *image.RGBA
		w, h   int
		margin int
	}{
		{"nil source", nil, 4, 4, 0},
		{"zero width", src, 0, 4, 0},
		{"zero height", src, 4, 0, 0},
		{"negative width", src, -4, 4, 0},
		{"negative margin", src, 4, 4, -1},
		{"margin eats image", src, 4, 4, 4},
		{"margin larger than image", src, 1, 1, 10},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := HQ2xGrid(tc.src, tc.w, tc.h, tc.margin); !errors.Is(err, ErrInvalidGrid) {
				t.Fatalf("err = %v, want ErrInvalidGrid", err)
			}
		})
	}
}

func TestHQ2xGridTooLarge(t *testing.T) {
	// Only the logical bounds matter for the guard; the pixel buffer stays
	// tiny so the test does not allocate gigabytes.
	huge := &image.RGBA{Pix: make([]byte, 4), Stride: 4, Rect: image.Rect(0, 0, 1<<15, 1<<15)}
	if _, err := HQ2xGrid(huge, 1, 1, 0); !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("err = %v, want ErrImageTooLarge", err)
	}
}

func TestHQ2xGridDeterministic(t *testing.T) {
	// Run the same sheet repeatedly under different GOMAXPROCS values and
	// concurrently; the worker scheduling order must not affect the bytes.
	src := image.NewRGBA(image.Rect(0, 0, 32, 32))
	seed := uint32(1)
	next := func() uint8 {
		seed = seed*1664525 + 1013904223
		return uint8(seed >> 24)
	}
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			a := uint8(0xff)
			if (x/8+y/8)%3 == 0 && x%8 == 0 {
				a = 0 // transparent edges
			}
			src.SetRGBA(x, y, color.RGBA{R: next(), G: next(), B: next(), A: a})
		}
	}

	want := sha256OfGrid(t, src)
	if got := sha256OfGrid(t, src); got != want {
		t.Fatalf("repeat hash = %s, want %s", got, want)
	}

	oldProcs := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(oldProcs)

	var wg sync.WaitGroup
	results := make([]string, 8)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = sha256OfGrid(t, src)
		}(i)
	}
	wg.Wait()
	for i, got := range results {
		if got != want {
			t.Fatalf("concurrent run %d hash = %s, want %s", i, got, want)
		}
	}
}

func sha256OfGrid(t *testing.T, src *image.RGBA) string {
	t.Helper()
	out, err := HQ2xGrid(src, 8, 8, 0)
	if err != nil {
		t.Fatalf("HQ2xGrid: %v", err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(out.Pix))
}

func TestHQ2xGridEmptyCellStaysBlank(t *testing.T) {
	// The right half of the sheet is a completely empty (transparent) frame.
	// Nothing from the red frame may bleed into it, and everything in it must
	// remain zero, including RGB channels.
	src := image.NewRGBA(image.Rect(0, 0, 8, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			src.SetRGBA(x, y, red)
		}
	}
	got, err := HQ2xGrid(src, 4, 4, 0)
	if err != nil {
		t.Fatalf("HQ2xGrid: %v", err)
	}
	for y := 0; y < 8; y++ {
		for x := 8; x < 16; x++ {
			if p := got.RGBAAt(x, y); p != blank {
				t.Fatalf("empty frame pixel (%d,%d) = %v, want zero", x, y, p)
			}
		}
	}
}

func TestHQ2xGridNonDivisibleWithMargin(t *testing.T) {
	// 9x7 with a 1px margin leaves a 7x5 inner area: a full 4x4 cell plus a
	// 3x1 partial cell. The partial cell must not sample the margin or the
	// neighboring cell.
	src := image.NewRGBA(image.Rect(0, 0, 9, 7))
	for y := 1; y < 6; y++ {
		for x := 1; x < 8; x++ {
			c := red
			if x >= 5 {
				c = blue
			}
			src.SetRGBA(x, y, c)
		}
	}
	got, err := HQ2xGrid(src, 4, 4, 1)
	if err != nil {
		t.Fatalf("HQ2xGrid: %v", err)
	}
	if want := image.Rect(0, 0, 18, 14); got.Bounds() != want {
		t.Fatalf("bounds = %v, want %v", got.Bounds(), want)
	}
	// The 3x1 partial cell occupies source rows 5..5, cols 5..7; it must be
	// blue and blank above and below it.
	for x := 10; x < 16; x++ {
		for y := 10; y < 12; y++ {
			if p := got.RGBAAt(x, y); p != blue {
				t.Fatalf("partial cell pixel (%d,%d) = %v, want blue", x, y, p)
			}
		}
	}
	for x := 10; x < 16; x++ {
		if p := got.RGBAAt(x, 12); p != blank {
			t.Fatalf("row after partial cell (%d,12) = %v, want zero", x, p)
		}
	}
}

func TestHQ2xGridEdgeMatchesIsolatedCell(t *testing.T) {
	// A fixed blue frame with two red detail pixels, sitting next to a blue
	// frame. Run as one sheet, the HQ2x rules sample the neighboring frame
	// across the seam and blend its blue into the frame's right edge; in grid
	// mode that pixel must equal running HQ2x on the frame alone.
	cell := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			cell.SetRGBA(x, y, blue)
		}
	}
	cell.SetRGBA(0, 0, red)
	cell.SetRGBA(3, 1, red)

	sheet := image.NewRGBA(image.Rect(0, 0, 8, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			sheet.SetRGBA(x, y, cell.RGBAAt(x, y))
		}
		for x := 4; x < 8; x++ {
			sheet.SetRGBA(x, y, blue)
		}
	}

	isolated, err := HQ2x(cell)
	if err != nil {
		t.Fatalf("HQ2x: %v", err)
	}
	got, err := HQ2xGrid(sheet, 4, 4, 0)
	if err != nil {
		t.Fatalf("HQ2xGrid: %v", err)
	}

	// Guard the premise: the seam pixel really does differ under whole-sheet
	// processing, so this test is not vacuous.
	whole, err := HQ2x(sheet)
	if err != nil {
		t.Fatalf("HQ2x sheet: %v", err)
	}
	if whole.RGBAAt(7, 2) == isolated.RGBAAt(7, 2) {
		t.Fatal("premise broken: whole-sheet HQ2x did not bleed at the seam")
	}

	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			w, g := isolated.RGBAAt(x, y), got.RGBAAt(x, y)
			if w.R != g.R || w.G != g.G || w.B != g.B {
				t.Fatalf("seam cell pixel (%d,%d) = %v, want isolated cell %v", x, y, g, w)
			}
		}
	}
}

func TestHQ2xSingleImageUnchanged(t *testing.T) {
	// Guard the original behavior: the exact bytes for a fixed 4x3 image,
	// with alpha forced to 255 and edge pixels clamped to the image border.
	src := image.NewRGBA(image.Rect(0, 0, 4, 3))
	pix := []color.RGBA{
		{255, 0, 0, 255}, {0, 255, 0, 255}, {0, 0, 255, 255}, {255, 255, 0, 255},
		{0, 255, 255, 255}, {255, 0, 255, 255}, {10, 20, 30, 255}, {40, 50, 60, 128},
		{255, 255, 255, 255}, {0, 0, 0, 255}, {100, 100, 100, 255}, {200, 100, 50, 0},
	}
	for i, c := range pix {
		src.SetRGBA(i%4, i/4, c)
	}
	got, err := HQ2x(src)
	if err != nil {
		t.Fatalf("HQ2x: %v", err)
	}
	const want = "05c63e796bc4bd815fd36a55fe46a00b81c0388473f5e159d7fb926edd274011"
	if hash := sha256OfPix(got); hash != want {
		t.Fatalf("HQ2x hash = %s, want %s", hash, want)
	}
}

func sha256OfPix(img *image.RGBA) string {
	return fmt.Sprintf("%x", sha256.Sum256(img.Pix))
}
