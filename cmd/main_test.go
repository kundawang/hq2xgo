package main

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"testing"

	hq2x "github.com/pokemium/hq2xgo"
)

func getImage(path string) (image.Image, error) {
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

func BenchmarkRun(b *testing.B) {
	before, err := getImage("../example/1/demo.png")
	if err != nil {
		panic(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := hq2x.HQ2x(before.(*image.RGBA))
		if err != nil {
			panic(err)
		}
	}
}

func TestGridCLIRejectsZeroCellWidth(t *testing.T) {
	if err := doHQ2xGrid("../example/1/demo.png", "out.png", 0, 4, 0); err == nil {
		t.Fatal("expected an error for a non-positive cell width")
	}
}

func TestGridCLIWritesDoubledSheet(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.png")
	if err := doHQ2xGrid("../example/1/demo.png", out, 16, 0, 0); err != nil {
		t.Fatalf("doHQ2xGrid: %v", err)
	}
	got, err := getImage(out)
	if err != nil {
		t.Fatalf("getImage: %v", err)
	}
	if want := image.Rect(0, 0, 320, 288); got.Bounds() != want {
		t.Fatalf("bounds = %v, want %v", got.Bounds(), want)
	}
}
