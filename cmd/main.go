package main

import (
	"flag"
	"fmt"
	"image"
	"image/draw"
	"os"
	"path/filepath"

	hq2x "github.com/pokemium/hq2xgo"

	_ "image/jpeg"
	"image/png"
	_ "image/png"
)

func main() {
	os.Exit(Run())
}

// Run - run app
func Run() int {
	cellW := flag.Int("cell", 0, "sprite grid cell width in pixels (enables grid mode)")
	cellH := flag.Int("cell-height", 0, "sprite grid cell height; defaults to -cell")
	margin := flag.Int("margin", 0, "transparent border around the sprite grid in pixels")
	flag.Parse()

	input := flag.Arg(0)
	if input == "" {
		help()
		return 0
	}

	output := flag.Arg(1)
	if output == "" {
		base, ext := getFileNameWithoutExt(input)
		output = base + "_hq2x" + ext
	}

	var err error
	if *cellW != 0 || *cellH != 0 || *margin != 0 {
		err = doHQ2xGrid(input, output, *cellW, *cellH, *margin)
	} else {
		err = doHQ2x(input, output)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	return 0
}

func imageToRGBA(im image.Image) *image.RGBA {
	dst := image.NewRGBA(im.Bounds())
	draw.Draw(dst, im.Bounds(), im, im.Bounds().Min, draw.Src)
	return dst
}

func doHQ2x(input, output string) error {
	before, err := openImage(input)
	if err != nil {
		return err
	}

	b, ok := before.(*image.RGBA)
	if !ok {
		b = imageToRGBA(before)
	}

	after, err := hq2x.HQ2x(b)
	if err != nil {
		return err
	}

	if err := saveImage(output, after); err != nil {
		return err
	}

	return nil
}

// doHQ2xGrid enlarges each sprite grid cell independently. A zero cellHeight
// means "square cells" (equal to cellWidth).
func doHQ2xGrid(input, output string, cellW, cellH, margin int) error {
	if cellW <= 0 {
		return fmt.Errorf("-cell must be a positive width in grid mode, got %d", cellW)
	}
	if cellH == 0 {
		cellH = cellW
	}

	before, err := openImage(input)
	if err != nil {
		return err
	}

	b, ok := before.(*image.RGBA)
	if !ok {
		b = imageToRGBA(before)
	}

	after, err := hq2x.HQ2xGrid(b, cellW, cellH, margin)
	if err != nil {
		return err
	}

	return saveImage(output, after)
}

func help() {
	fmt.Println("hq2x <input> [<output>]")
	fmt.Println()
	fmt.Println("Single image:")
	fmt.Println("  hq2x input.png output.png")
	fmt.Println()
	fmt.Println("Sprite sheet (each cell scaled independently):")
	fmt.Println("  hq2x -cell 16 [-cell-height 24] [-margin 1] sheet.png output.png")
}

func getFileNameWithoutExt(path string) (filename string, ext string) {
	filename = path[:len(path)-len(filepath.Ext(path))]
	ext = filepath.Ext(path)
	return filename, ext
}

func openImage(path string) (image.Image, error) {
	file, err := os.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return img, err
	}

	return img, nil
}

func saveImage(path string, img image.Image) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := png.Encode(file, img); err != nil {
		return err
	}
	return nil
}
