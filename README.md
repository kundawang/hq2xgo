# hq2xgo

![Go](https://github.com/pokemium/hq2xgo/workflows/Go/badge.svg) [![GoDoc](https://godoc.org/github.com/pokemium/hq2xgo?status.svg)](https://godoc.org/github.com/pokemium/hq2xgo)

Enlarge image by 2x with hq2x algorithm

## Example(Before -> After)

<img src="./example/1/demo.png" width="320" height="288" />&nbsp;&nbsp;&nbsp;&nbsp;<img src="./example/1/demo_hq2x.png" />

<br />

<img src="./example/2/demo.png" width="320" height="288" />&nbsp;&nbsp;&nbsp;&nbsp;<img src="./example/2/demo_hq2x.png" />

## Usage

### command line

```sh
$ make build # require make and go
$ hq2x input.png output.png
```

Sprite sheets can be enlarged cell by cell, so that frames never sample their
neighbors:

```sh
$ hq2x -cell 16 -margin 1 sheet.png output.png   # square 16px cells, 1px border
$ hq2x -cell 16 -cell-height 24 sheet.png output.png
```

### golang package

See [godoc](https://godoc.org/github.com/Akatsuki-py/hq2xgo) for details. 

```sh
$ go get github.com/pokemium/hq2xgo
```

```go
import (
	hq2x "github.com/pokemium/hq2xgo"
)

after, err := hq2x.HQ2x(before) // var before *image.RGBA

// Sprite sheet: each cellWidth x cellHeight cell is enlarged on its own.
// The margin is a transparent border around the whole grid.
sheet, err := hq2x.HQ2xGrid(before, cellWidth, cellHeight, margin)
```

## Sprite grid mode

`HQ2x` treats the whole image as one picture, so on a sprite sheet the pixels
at the edge of every frame sample the neighboring frame and pick up its
colors. `HQ2xGrid` (or `HQ2xGridImage` with a `SpriteGrid` value) fixes that:

- Each cell is processed with the normal HQ2x rules, but its 3x3 neighborhood
  is clamped to the cell, so frames never sample across cell boundaries.
- The output keeps the sheet's layout and is exactly twice the source size in
  both directions. A source cell at `(x, y)` lands at `(2x, 2y)`.
- The source alpha channel is preserved. Pixels with alpha 0 (transparent
  padding, gaps between frames) produce four fully transparent pixels and are
  never filled with color.
- If the inner area is not a whole number of cells, the trailing partial
  cell is still enlarged from its own pixels; no row/column is dropped.
- Images with a non-zero `Bounds()` origin (sub-images) work as expected.
- Invalid grids return an error wrapping `ErrInvalidGrid`; an output above the
  internal pixel budget returns `ErrImageTooLarge`.
- Cells are processed concurrently and each worker writes only its own output
  region, so results are identical regardless of scheduling.

The single-image `HQ2x` behavior is unchanged.

## Measure benchmark

```sh
$ cd cmd/
$ go test -bench .
```
