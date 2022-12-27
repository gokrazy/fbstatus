package main

import (
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"testing"
)

func drawToFile(w, h int) error {
	ctx := context.Background()
	img := image.NewRGBA(image.Rect(0, 0, w, h))

	drawer, err := newStatusDrawer(img)
	if err != nil {
		return err
	}
	if err := drawer.draw1(ctx); err != nil {
		return err
	}

	out, err := os.Create(fmt.Sprintf("/tmp/fbstatus-%dx%d.jpg", w, h))
	if err != nil {
		return err
	}
	defer out.Close()
	if err := jpeg.Encode(out, img, nil); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return nil
}

func TestDraw(t *testing.T) {
	for _, resolution := range []struct {
		w, h int
	}{
		{w: 800, h: 600},
		{w: 1024, h: 768},
		{w: 1920, h: 1080}, // Full HD
		{w: 2560, h: 1440}, // typical 27 inch resolution
		{w: 3840, h: 2160}, // 4K resolution
	} {
		if err := drawToFile(resolution.w, resolution.h); err != nil {
			t.Fatal(err)
		}
	}
}
