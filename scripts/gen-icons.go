// +build ignore

package main

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

func main() {
	dir := filepath.Join("internal", "menubar", "icons")
	os.MkdirAll(dir, 0755)

	colors := map[string]color.RGBA{
		"green":  {R: 0x34, G: 0xD3, B: 0x99, A: 0xFF}, // emerald
		"yellow": {R: 0xFB, G: 0xBF, B: 0x24, A: 0xFF}, // amber
		"red":    {R: 0xEF, G: 0x44, B: 0x44, A: 0xFF}, // red
	}

	for name, c := range colors {
		img := image.NewRGBA(image.Rect(0, 0, 22, 22))
		cx, cy, r := 11.0, 11.0, 8.0

		for y := 0; y < 22; y++ {
			for x := 0; x < 22; x++ {
				dx := float64(x) - cx
				dy := float64(y) - cy
				dist := math.Sqrt(dx*dx + dy*dy)
				if dist <= r {
					img.Set(x, y, c)
				} else if dist <= r+1 {
					// Anti-alias edge
					alpha := uint8(255 * (1 - (dist - r)))
					img.Set(x, y, color.RGBA{c.R, c.G, c.B, alpha})
				}
			}
		}

		f, _ := os.Create(filepath.Join(dir, name+".png"))
		png.Encode(f, img)
		f.Close()
	}
}
