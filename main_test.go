package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestMakeKey(t *testing.T) {
	tests := []struct {
		r, g, b uint8
		want    int
	}{
		{0, 0, 0, 0},
		{255, 255, 255, 255*256*256 + 255*256 + 255},
		{10, 20, 30, 10*65536 + 20*256 + 30},
	}

	for _, tc := range tests {
		got := makeKey(tc.r, tc.g, tc.b)
		if got != tc.want {
			t.Errorf("makeKey(%d, %d, %d) = %d, want %d", tc.r, tc.g, tc.b, got, tc.want)
		}
	}
}

func TestMakeString(t *testing.T) {
	got := makeString(12, 34, 56)
	want := "r 12 g 34 b 56"
	if got != want {
		t.Errorf("makeString(12, 34, 56) = %q, want %q", got, want)
	}
}

func TestGetScaler(t *testing.T) {
	validScalers := []string{"NearestNeighbor", "ApproxBiLinear", "BiLinear", "CatmullRom"}
	for _, name := range validScalers {
		s := getScaler(name)
		if s == nil {
			t.Errorf("getScaler(%q) returned nil, expected non-nil scaler", name)
		}
	}

	if s := getScaler("invalid"); s != nil {
		t.Errorf("getScaler(\"invalid\") = %v, want nil", s)
	}
}

func TestCalcImg(t *testing.T) {
	scaler := getScaler("CatmullRom")

	// Create a 100x80 test image
	src := image.NewRGBA(image.Rect(0, 0, 100, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 100; x++ {
			src.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 100, A: 255})
		}
	}

	// Downscale to 32
	scaled, err := calcImg(src, "test.png", scaler, 32)
	if err != nil {
		t.Fatalf("calcImg failed: %v", err)
	}
	if scaled.Bounds().Dx() != 32 || scaled.Bounds().Dy() != 32 {
		t.Errorf("expected 32x32, got %dx%d", scaled.Bounds().Dx(), scaled.Bounds().Dy())
	}

	// Too small should return error
	smallSrc := image.NewRGBA(image.Rect(0, 0, 16, 16))
	_, err = calcImg(smallSrc, "test_small.png", scaler, 32)
	if err == nil {
		t.Errorf("expected error when image is smaller than pixelsize, got nil")
	}
}

func TestParseSrc(t *testing.T) {
	tmpDir := t.TempDir()
	imgPath := filepath.Join(tmpDir, "src.png")

	// Create a test image
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 50, B: 50, A: 255})
		}
	}

	f, err := os.Create(imgPath)
	if err != nil {
		t.Fatalf("failed to create test image: %v", err)
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		t.Fatalf("failed to encode test image: %v", err)
	}
	f.Close()

	srcImg, cacheMap, err := parseSrc(imgPath, "CatmullRom", 32)
	if err != nil {
		t.Fatalf("parseSrc failed: %v", err)
	}
	if srcImg == nil {
		t.Fatal("expected non-nil srcImg")
	}
	if cacheMap == nil {
		t.Fatal("expected non-nil cacheMap")
	}
	if srcImg.Bounds().Dx() > 32 || srcImg.Bounds().Dy() > 32 {
		t.Errorf("expected bounds <= 32, got %dx%d", srcImg.Bounds().Dx(), srcImg.Bounds().Dy())
	}
}

func TestEndToEndMosaic(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Create a library with 2 images
	libDir := filepath.Join(tmpDir, "lib")
	if err := os.MkdirAll(libDir, 0755); err != nil {
		t.Fatalf("failed to create lib dir: %v", err)
	}

	colors := []color.RGBA{
		{R: 255, G: 0, B: 0, A: 255},
		{R: 0, G: 0, B: 255, A: 255},
	}
	for i, c := range colors {
		im := image.NewRGBA(image.Rect(0, 0, 32, 32))
		for y := 0; y < 32; y++ {
			for x := 0; x < 32; x++ {
				im.Set(x, y, c)
			}
		}
		f, err := os.Create(filepath.Join(libDir, filepath.Base(t.Name())+"_img_"+string(rune('0'+i))+".png"))
		if err != nil {
			t.Fatalf("failed to create lib image: %v", err)
		}
		_ = png.Encode(f, im)
		f.Close()
	}

	// 2. Create a 4x4 src image
	srcPath := filepath.Join(tmpDir, "src.png")
	srcImg := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			srcImg.Set(x, y, color.RGBA{R: 250, G: 5, B: 5, A: 255})
		}
	}
	f, err := os.Create(srcPath)
	if err != nil {
		t.Fatalf("failed to create src image: %v", err)
	}
	_ = png.Encode(f, srcImg)
	f.Close()

	// 3. Test loadLib
	dbPath := filepath.Join(tmpDir, "test.db")
	parsedImg, cacheMap, err := parseSrc(srcPath, "CatmullRom", 4)
	if err != nil {
		t.Fatalf("parseSrc failed: %v", err)
	}

	err = loadLib(libDir, 2, dbPath, 16, "CatmullRom", true, "testlib")
	if err != nil {
		t.Fatalf("loadLib failed: %v", err)
	}

	// 4. Test genTarget
	targetPath := filepath.Join(tmpDir, "output.png")
	err = genTarget(parsedImg, targetPath, 2, dbPath, 16, 1, "CatmullRom", "testlib", cacheMap)
	if err != nil {
		t.Fatalf("genTarget failed: %v", err)
	}

	// Verify output
	info, err := os.Stat(targetPath)
	if err != nil {
		t.Fatalf("output file stat error: %v", err)
	}
	if info.Size() == 0 {
		t.Errorf("output file is empty")
	}

	tf, err := os.Open(targetPath)
	if err != nil {
		t.Fatalf("open output failed: %v", err)
	}
	defer tf.Close()

	resImg, _, err := image.Decode(tf)
	if err != nil {
		t.Fatalf("decode output failed: %v", err)
	}
	expectedW := parsedImg.Bounds().Dx() * 16
	expectedH := parsedImg.Bounds().Dy() * 16
	if resImg.Bounds().Dx() != expectedW || resImg.Bounds().Dy() != expectedH {
		t.Errorf("expected size %dx%d, got %dx%d", expectedW, expectedH, resImg.Bounds().Dx(), resImg.Bounds().Dy())
	}
}
