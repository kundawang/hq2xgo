package hq2x

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"runtime"
	"sync"
	"sync/atomic"
)

// Errors returned by HQ2x and HQ2xGrid. They are wrapped with detail, so use
// errors.Is to test them.
var (
	// ErrInvalidGrid reports a grid description that cannot be applied, such
	// as a non-positive cell size, a negative margin, or a grid that does not
	// fit inside the source image.
	ErrInvalidGrid = errors.New("hq2x: invalid sprite grid")
	// ErrImageTooLarge reports a source image whose 2x output would overflow
	// the address space or exceed the internal pixel budget.
	ErrImageTooLarge = errors.New("hq2x: image too large")
)

// maxInt is the largest value an int can hold on this platform.
const maxInt = int(^uint(0) >> 1)

// maxPixels bounds the size of an output image to keep a pathological input
// from exhausting memory. 1<<28 pixels is a 1 GiB RGBA buffer.
const maxPixels = 1 << 28

// SpriteGrid describes a uniform sprite sheet: equally sized cells laid out in
// rows, optionally inset from the image border by an outer margin.
//
// Cells are addressed by their column and row, starting at (0, 0) in the
// top-left corner. Margin is the transparent border around the whole grid
// (top, right, bottom and left); cells themselves are contiguous. Transparent
// padding inside a cell is ordinary cell content and is preserved.
type SpriteGrid struct {
	// CellWidth and CellHeight are the size of one cell in pixels. Both must
	// be positive.
	CellWidth, CellHeight int
	// Margin is the number of pixels left untouched around the outside of the
	// grid. It must be non-negative.
	Margin int
}

// HQ2xGrid enlarges each cell of a sprite sheet independently.
//
// Every cell is treated as its own image: the 3x3 neighborhood used by the
// HQ2x rules is clamped to the cell's bounds, so pixels never sample a
// neighboring frame. The result keeps the sheet's arrangement and doubles it
// in both directions: a source cell at (x, y) becomes a block at (2x, 2y) of
// twice the cell size.
//
// The sheet may have a transparent outer margin; cells are laid out inside it
// and the margin is doubled along with the rest of the sheet. If the inner
// area is not a whole number of cells, the last column and/or row is a
// partial cell: it is enlarged using only its own remaining pixels, so every
// source pixel is accounted for and no data is dropped. Pixels outside the
// cells, including transparent spacer pixels, are left fully transparent
// (zero) and are never sampled or filled in. A source pixel with alpha 0 maps
// to four fully transparent (zero) destination pixels; other alpha values are
// preserved as-is.
//
// The source is read through its Bounds(), so images with a non-zero origin
// (for example a sub-image) are handled correctly. The returned image always
// has origin (0, 0) and size equal to twice the source's size.
//
// HQ2xGrid returns ErrInvalidGrid if src is nil, if cellWidth or cellHeight is
// not positive, if margin is negative, or if the margin leaves no cell area.
// An output that would exceed the internal pixel budget returns
// ErrImageTooLarge.
//
// Cells are processed concurrently and each worker writes only to its own
// output region, so the result does not depend on scheduling or worker count.
func HQ2xGrid(src *image.RGBA, cellWidth, cellHeight, margin int) (*image.RGBA, error) {
	return HQ2xGridImage(src, SpriteGrid{
		CellWidth:  cellWidth,
		CellHeight: cellHeight,
		Margin:     margin,
	})
}

// HQ2xGridImage is HQ2xGrid with an explicit SpriteGrid. It is equivalent to
// calling HQ2xGrid with the same field values.
func HQ2xGridImage(src *image.RGBA, grid SpriteGrid) (*image.RGBA, error) {
	layout, err := newGridLayout(src, grid)
	if err != nil {
		return nil, err
	}

	destW, destH, err := scaledSize(int64(layout.srcW), int64(layout.srcH), 2)
	if err != nil {
		return nil, err
	}
	if int64(destW)*int64(destH) > maxPixels {
		return nil, fmt.Errorf("%w: %dx%d output exceeds pixel budget",
			ErrImageTooLarge, destW, destH)
	}
	dest := image.NewRGBA(image.Rect(0, 0, destW, destH))

	totalCells := layout.cols * layout.rows
	var (
		cursor int64
		wg     sync.WaitGroup
	)
	workers := runtime.NumCPU()
	if workers > totalCells {
		workers = totalCells
	}
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for {
				n := int(atomic.AddInt64(&cursor, 1) - 1)
				if n >= totalCells {
					return
				}
				hq2xGridCell(src, dest, layout, n%layout.cols, n/layout.cols)
			}
		}()
	}
	wg.Wait()

	return dest, nil
}

// gridLayout is a validated description of where cells live in the source.
type gridLayout struct {
	grid           SpriteGrid
	srcW, srcH     int
	innerW, innerH int
	cols, rows     int
}

func newGridLayout(src *image.RGBA, grid SpriteGrid) (gridLayout, error) {
	if src == nil {
		return gridLayout{}, fmt.Errorf("%w: nil source", ErrInvalidGrid)
	}
	if grid.CellWidth <= 0 || grid.CellHeight <= 0 {
		return gridLayout{}, fmt.Errorf("%w: cell size %dx%d must be positive",
			ErrInvalidGrid, grid.CellWidth, grid.CellHeight)
	}
	if grid.Margin < 0 {
		return gridLayout{}, fmt.Errorf("%w: negative margin %d",
			ErrInvalidGrid, grid.Margin)
	}

	srcW, srcH := src.Bounds().Dx(), src.Bounds().Dy()
	innerW := int64(srcW) - 2*int64(grid.Margin)
	innerH := int64(srcH) - 2*int64(grid.Margin)
	if innerW < 1 || innerH < 1 {
		return gridLayout{}, fmt.Errorf("%w: %dx%d source with margin %d has no cell area",
			ErrInvalidGrid, srcW, srcH, grid.Margin)
	}

	// Rounding up keeps a trailing partial cell instead of dropping the
	// remaining source pixels.
	cols := int((innerW + int64(grid.CellWidth) - 1) / int64(grid.CellWidth))
	rows := int((innerH + int64(grid.CellHeight) - 1) / int64(grid.CellHeight))

	return gridLayout{
		grid:   grid,
		srcW:   srcW,
		srcH:   srcH,
		innerW: int(innerW),
		innerH: int(innerH),
		cols:   cols,
		rows:   rows,
	}, nil
}

// bounds returns the source-relative origin and size of cell (col, row). The
// width and height may be smaller than the cell size for the last column or
// row when the inner area is not an exact multiple.
func (l gridLayout) bounds(col, row int) (x, y, w, h int) {
	x = l.grid.Margin + col*l.grid.CellWidth
	y = l.grid.Margin + row*l.grid.CellHeight
	w, h = l.grid.CellWidth, l.grid.CellHeight
	if w > l.innerW-col*l.grid.CellWidth {
		w = l.innerW - col*l.grid.CellWidth
	}
	if h > l.innerH-row*l.grid.CellHeight {
		h = l.innerH - row*l.grid.CellHeight
	}
	return x, y, w, h
}

// hq2xGridCell enlarges the cell at grid position (col, row) in place.
func hq2xGridCell(src, dest *image.RGBA, layout gridLayout, col, row int) {
	cellX, cellY, cellW, cellH := layout.bounds(col, row)
	min := src.Bounds().Min

	for y := 0; y < cellH; y++ {
		for x := 0; x < cellW; x++ {
			// Resolve the 3x3 neighborhood, clamped to this cell so that
			// adjacent frames can never bleed into the result.
			var context [9]color.RGBA
			idx := 0
			for dy := -1; dy <= 1; dy++ {
				ny := clamp(y+dy, cellH)
				for dx := -1; dx <= 1; dx++ {
					nx := clamp(x+dx, cellW)
					context[idx] = src.RGBAAt(
						min.X+cellX+nx,
						min.Y+cellY+ny,
					)
					idx++
				}
			}

			dx := 2 * (cellX + x)
			dy := 2 * (cellY + y)

			a := context[CENTER].A
			if a == 0 {
				// Keep transparent padding completely blank: no color is
				// written into (or bled into) it.
				dest.SetRGBA(dx, dy, color.RGBA{})
				dest.SetRGBA(dx+1, dy, color.RGBA{})
				dest.SetRGBA(dx, dy+1, color.RGBA{})
				dest.SetRGBA(dx+1, dy+1, color.RGBA{})
				continue
			}

			tl, tr, bl, br := hq2xContext(context)
			tl.A, tr.A, bl.A, br.A = a, a, a, a
			dest.SetRGBA(dx, dy, tl)
			dest.SetRGBA(dx+1, dy, tr)
			dest.SetRGBA(dx, dy+1, bl)
			dest.SetRGBA(dx+1, dy+1, br)
		}
	}
}

// clamp returns v constrained to [0, n-1].
func clamp(v, n int) int {
	if v < 0 {
		return 0
	}
	if v >= n {
		return n - 1
	}
	return v
}
