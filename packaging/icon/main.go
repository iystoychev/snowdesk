// Command icon draws the app icon (1024×1024 PNG): a white snowflake on a
// red rounded square. Run by `make icon`.
package main

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
)

const S = 1024

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: icon out.png")
	}
	img := image.NewRGBA(image.Rect(0, 0, S, S))
	const ss = 4 // samples per axis, for smooth edges
	for y := 0; y < S; y++ {
		for x := 0; x < S; x++ {
			var r, g, b, a float64
			for j := 0; j < ss; j++ {
				for i := 0; i < ss; i++ {
					px, py := float64(x)+(float64(i)+0.5)/ss, float64(y)+(float64(j)+0.5)/ss
					cr, cg, cb, ca := sample(px, py)
					r, g, b, a = r+cr*ca, g+cg*ca, b+cb*ca, a+ca
				}
			}
			n := float64(ss * ss)
			img.SetRGBA(x, y, color.RGBA{uint8(r / n * 255), uint8(g / n * 255), uint8(b / n * 255), uint8(a / n * 255)})
		}
	}
	f, err := os.Create(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
}

// sample returns the straight color and alpha at one point.
func sample(x, y float64) (r, g, b, a float64) {
	// macOS icon grid: an 824 px rounded square centred in the canvas.
	const half, rad = 412.0, 185.0
	dx := math.Max(math.Abs(x-S/2)-(half-rad), 0)
	dy := math.Max(math.Abs(y-S/2)-(half-rad), 0)
	if math.Hypot(dx, dy) > rad {
		return 0, 0, 0, 0
	}
	if inFlake(x-S/2, y-S/2) {
		return 1, 1, 1, 1
	}
	t := (y - (S/2 - half)) / (2 * half) // 0 at top, 1 at bottom
	return 0.93 - 0.18*t, 0.12 - 0.06*t, 0.17 - 0.08*t, 1
}

// inFlake reports whether (x, y), relative to the centre, is on the
// six-armed snowflake.
func inFlake(x, y float64) bool {
	const arm, w = 300.0, 22.0
	if math.Hypot(x, y) < 52 {
		return true
	}
	for k := 0; k < 6; k++ {
		ang := float64(k) * math.Pi / 3
		// Rotate the point into the arm's frame: u along the arm, v across it.
		u := x*math.Cos(ang) + y*math.Sin(ang)
		v := -x*math.Sin(ang) + y*math.Cos(ang)
		if u > 0 && u < arm && math.Abs(v) < w/2 {
			return true
		}
		if math.Hypot(u-arm, v) < w*0.9 { // rounded tip
			return true
		}
		// Two pairs of side branches, angled toward the tip.
		for _, at := range []struct{ pos, len float64 }{{140, 95}, {225, 65}} {
			for _, side := range []float64{-1, 1} {
				bu, bv := u-at.pos, v*side
				bx := bu*math.Cos(math.Pi/3) + bv*math.Sin(math.Pi/3)
				by := -bu*math.Sin(math.Pi/3) + bv*math.Cos(math.Pi/3)
				if bx > 0 && bx < at.len && math.Abs(by) < w*0.42 {
					return true
				}
			}
		}
	}
	return false
}
