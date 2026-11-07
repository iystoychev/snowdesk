package snow

import "math"

// Candles: a group of three candles that sits in a screen corner. The
// overlay ignores the mouse, so the app puts a small catcher window over
// CandleRect; dragging it calls DragCandles, and letting go calls
// DropCandles, which glides the group to the nearest corner.

// Screen corners for Settings.CandleCorner.
const (
	BottomLeft = iota
	BottomRight
	TopLeft
	TopRight
)

const (
	candleW      = 80.0  // group width, dish included
	candleH      = 100.0 // group height, flames included
	candleMargin = 28.0  // gap to the screen side
	candleTop    = 40.0  // top corners sit below the menu bar
	candleGlide  = 9.0   // 1/s; how fast the group glides into its corner
)

// One candle of the group: offset of its base centre from the group's base
// centre, body size, wax color and drips (position across 0..1, length as
// a share of the height).
var candleParts = []struct {
	dx, w, h float64
	wax      rgba
	drips    [][2]float64
}{
	{0, 17, 58, rgba{0.74, 0.08, 0.11, 1}, [][2]float64{{0.18, 0.34}, {0.52, 0.12}, {0.8, 0.22}}}, // red, tall, at the back
	{-20, 14, 40, rgba{0.95, 0.91, 0.80, 1}, [][2]float64{{0.25, 0.3}, {0.72, 0.15}}},             // cream
	{20, 14, 30, rgba{0.95, 0.91, 0.80, 1}, [][2]float64{{0.35, 0.22}, {0.78, 0.38}}},
}

type candles struct {
	x, y     float64 // base centre of the group
	vx       float64 // horizontal speed, for the flames' lean
	lean     float64 // smoothed flame lean, pt
	dragging bool
	placed   bool
}

// candleHome is the base centre of the group in corner c.
func (s *Sim) candleHome(c int) (x, y float64) {
	x = candleMargin + candleW/2
	if c == BottomRight || c == TopRight {
		x = s.W - x
	}
	y = s.H - 6
	if c == TopLeft || c == TopRight {
		y = candleTop + candleH
	}
	return x, y
}

// CandleRect is where the candles will rest, in screen points (top-left
// origin), for the app's mouse catcher. ok is false when candles are off or
// a fullscreen app is showing.
func (s *Sim) CandleRect() (r Rect, ok bool) {
	if !s.cfg.Candles || s.full {
		return Rect{}, false
	}
	x, y := s.candleHome(s.cfg.CandleCorner)
	return Rect{x - candleW/2, y - candleH, candleW, candleH}, true
}

// DragCandles moves the candles by (dx, dy) while the user drags them.
func (s *Sim) DragCandles(dx, dy float64) {
	s.placeCandles()
	c := &s.candle
	c.dragging = true
	c.x = clampF(c.x+dx, candleW/2, s.W-candleW/2)
	c.y = clampF(c.y+dy, candleH, s.H)
}

// DropCandles ends a drag: the candles glide to the corner nearest to where
// they were let go. It returns that corner.
func (s *Sim) DropCandles() int {
	c := &s.candle
	c.dragging = false
	corner := TopLeft
	if c.y-candleH/2 > s.H/2 {
		corner = BottomLeft
	}
	if c.x > s.W/2 {
		corner++
	}
	s.cfg.CandleCorner = corner
	return corner
}

func (s *Sim) placeCandles() {
	if !s.candle.placed {
		s.candle.x, s.candle.y = s.candleHome(s.cfg.CandleCorner)
		s.candle.placed = true
	}
}

func (s *Sim) stepCandles(dt float64, cursor Point) {
	if !s.cfg.Candles {
		s.candle.placed = false
		return
	}
	s.placeCandles()
	c := &s.candle
	px := c.x
	if !c.dragging {
		hx, hy := s.candleHome(s.cfg.CandleCorner)
		k := 1 - math.Exp(-candleGlide*dt)
		c.x += (hx - c.x) * k
		c.y += (hy - c.y) * k
	}
	if dt > 0 {
		c.vx = (c.x - px) / dt
	}
	// Flames trail behind when the candles move, bend with the wind, and
	// shy away from a nearby cursor.
	target := clamp(-c.vx*0.02, 9) + s.wind*0.03
	if dx := c.x - cursor.X; math.Abs(dx) < 60 && cursor.Y > c.y-candleH-30 && cursor.Y < c.y {
		target += math.Copysign(6*(1-math.Abs(dx)/60), dx)
	}
	c.lean += (target - c.lean) * (1 - math.Exp(-8*dt))
}

func (s *Sim) drawCandles(f *frame) {
	if !s.cfg.Candles || !s.candle.placed {
		return
	}
	c := &s.candle
	s.drawDish(f, c.x, c.y)
	plate := c.y - 7 // candles stand on the dish
	for i, p := range candleParts {
		s.drawCandle(f, i, c.x+p.dx, plate)
	}
	drawHolly(f, c.x-25, c.y-4)
}

// drawDish draws a brass dish with a soft shadow under it; (x, y) is the
// middle of its bottom.
func (s *Sim) drawDish(f *frame, x, y float64) {
	ellipse(f, x, y-1, 44, 6, 0, 2*math.Pi, rgba{0, 0, 0, 0.35}, rgba{0, 0, 0, 0})
	dark := rgba{0.45, 0.30, 0.10, 1}
	mid := rgba{0.72, 0.52, 0.20, 1}
	light := rgba{0.98, 0.84, 0.48, 1}
	ellipse(f, x, y-3, 35, 5, 0, math.Pi, dark, dark) // rounded underside
	f.tri(x-35, y-7, mid, x+35, y-7, dark, x+35, y-3, dark)
	f.tri(x-35, y-7, mid, x+35, y-3, dark, x-35, y-3, mid)
	ellipse(f, x, y-7, 35, 6, 0, 2*math.Pi, light, mid) // top face
	ellipse(f, x, y-7, 28, 4.5, 0, 2*math.Pi, mid, shade(mid, 0.85))
	f.quad(x-26, y-5.4, x-8, y-5.1, x-8, y-4.6, x-26, y-4.9, rgba{1, 0.95, 0.75, 0.7}) // rim glint
}

// drawCandle draws candle i standing at (bx, base).
func (s *Sim) drawCandle(f *frame, i int, bx, base float64) {
	p := candleParts[i]
	fl := s.flicker(i)
	w, top := p.w, base-p.h
	x0 := bx - w/2
	ry := w * 0.2 // the top and bottom ellipses' half-height
	warm := rgba{1, 0.78, 0.45, 1}

	// Light the candle casts around itself.
	f.dot(bx, top-14, 50+6*fl, 0, rgba{1, 0.62, 0.22, float32(0.11 + 0.04*fl)})

	// Body: vertical strips shaded like a cylinder lit from the front left,
	// warmer near the flame at the top.
	const strips = 8
	col := func(u, v float64) rgba { // u across 0..1, v down 0..1
		b := 0.55 + 0.3*math.Sin(math.Pi*u) + 0.35*math.Exp(-math.Pow((u-0.3)/0.16, 2))
		c := shade(p.wax, float32(b*(1-0.12*v)))
		return mix(c, warm, float32(0.28*(1-v)*(1-v)))
	}
	for k := range strips {
		u0, u1 := float64(k)/strips, float64(k+1)/strips
		xa, xb := x0+w*u0, x0+w*u1
		f.tri(xa, top, col(u0, 0), xb, top, col(u1, 0), xb, base, col(u1, 1))
		f.tri(xa, top, col(u0, 0), xb, base, col(u1, 1), xa, base, col(u0, 1))
		// Rounded bottom edge.
		ya := base + ry*math.Sin(math.Pi*u0)
		yb := base + ry*math.Sin(math.Pi*u1)
		f.tri(xa, base, col(u0, 1), xb, base, col(u1, 1), xb, yb, col(u1, 1))
		f.tri(xa, base, col(u0, 1), xb, yb, col(u1, 1), xa, ya, col(u0, 1))
	}

	// Top: a melted rim around a pool of wax that glows in the flame's light.
	ellipse(f, bx, top, w/2, ry, 0, 2*math.Pi, col(0.35, 0), shade(p.wax, 0.8))
	ellipse(f, bx, top+0.3, w/2-2.2, ry-1, 0, 2*math.Pi,
		mix(p.wax, rgba{1, 0.9, 0.6, 1}, float32(0.55+0.15*fl)), shade(p.wax, 0.9))

	// Drips run down from the rim and end in a drop.
	for _, d := range p.drips {
		u := d[0]
		dx := x0 + w*u
		y0 := top + ry*math.Sin(math.Pi*u)*0.9
		l := p.h * d[1]
		c := shade(col(u, 0.1), 1.06)
		f.quad(dx-1.8, y0-0.5, dx+1.8, y0-0.5, dx+1.3, y0+l, dx-1.3, y0+l, c)
		f.dot(dx, y0+l, 1.9, 0.85, c)
		f.dot(dx-0.6, y0+l-0.6, 0.6, 0.5, rgba{1, 1, 1, 0.5}) // shine
	}

	// Wick: a short charred curl with a glowing tip.
	wick := rgba{0.12, 0.08, 0.06, 1}
	bend := 1.2 + 0.4*s.candle.lean/9
	f.quad(bx-0.7, top, bx+0.7, top, bx+0.7+bend*0.4, top-3, bx-0.7+bend*0.4, top-3, wick)
	f.quad(bx-0.7+bend*0.4, top-3, bx+0.7+bend*0.4, top-3, bx+0.5+bend, top-5.5, bx-0.5+bend, top-5.5, wick)
	f.dot(bx+bend, top-5.5, 1, 0.3, rgba{1, 0.35, 0.1, 0.9})

	// Flame: blue at the root, then orange, yellow and a white core.
	h := 17 + 3*fl
	lean := s.candle.lean + 1.2*math.Sin(s.t*7.3+float64(i)*2)
	fy := top - 3.5
	f.dot(bx, fy-h*0.45, 13+2*fl, 0, rgba{1, 0.75, 0.35, 0.28}) // halo
	flame(f, bx, fy, 4.6, h, lean, rgba{1, 0.55, 0.1, 0.85}, rgba{1, 0.4, 0.05, 0.05})
	flame(f, bx, fy-0.6, 3.3, h*0.72, lean*0.75, rgba{1, 0.85, 0.3, 1}, rgba{1, 0.65, 0.15, 0.3})
	flame(f, bx, fy-1, 1.9, h*0.42, lean*0.45, rgba{1, 1, 0.92, 1}, rgba{1, 0.95, 0.7, 0.6})
	f.dot(bx, fy-1.2, 2, 0.2, rgba{0.35, 0.55, 1, 0.55})
}

// flame fills a teardrop: a round bottom of radius r resting on y, and a
// tip h above y, bent sideways by lean. Color fades from cc in the middle
// to ec at the edge.
func flame(f *frame, x, y, r, h, lean float64, cc, ec rgba) {
	var pts [24][2]float64
	n := 0
	cy := y - r
	// Right side up to the tip, then the left side back down.
	for k := 0; k <= 7; k++ {
		t := float64(k) / 7
		pts[n] = [2]float64{x + r*math.Pow(1-t, 1.3) + lean*t*t, cy - (h-r)*t}
		n++
	}
	for k := 6; k >= 1; k-- {
		t := float64(k) / 7
		pts[n] = [2]float64{x - r*math.Pow(1-t, 1.3) + lean*t*t, cy - (h-r)*t}
		n++
	}
	// Round bottom, from the left side to the right side.
	for k := 0; k <= 8; k++ {
		a := math.Pi - math.Pi*float64(k)/8
		pts[n] = [2]float64{x + r*math.Cos(a), cy + r*math.Sin(a)}
		n++
	}
	fan(f, x+lean*0.15, cy-r*0.3, pts[:n], cc, ec)
}

// drawHolly draws two holly leaves and three berries centred on (x, y).
func drawHolly(f *frame, x, y float64) {
	green := rgba{0.10, 0.42, 0.18, 1}
	light := rgba{0.25, 0.62, 0.30, 1}
	for _, a := range []float64{math.Pi * 0.93, math.Pi * 0.07} {
		hollyLeaf(f, x, y, a, 11, green, light)
	}
	for _, b := range [][2]float64{{-2.2, -2}, {2.4, -1.6}, {0, 1}} {
		bx, by := x+b[0], y+b[1]
		f.dot(bx, by, 2.4, 0.85, rgba{0.80, 0.06, 0.08, 1})
		f.dot(bx-0.7, by-0.8, 0.8, 0.3, rgba{1, 0.8, 0.8, 0.8})
	}
}

// hollyLeaf draws a spiky leaf from (x, y) pointing at angle a, len long.
func hollyLeaf(f *frame, x, y, a, l float64, c, mid rgba) {
	var pts [16][2]float64
	n := 0
	ux, uy := math.Cos(a), math.Sin(a)
	vx, vy := -uy, ux
	at := func(u, v float64) [2]float64 { return [2]float64{x + ux*u + vx*v, y + uy*u + vy*v} }
	// Along one edge to the tip and back the other way, spikes poking out.
	for _, side := range []float64{1, -1} {
		for k := 0; k <= 6; k++ {
			t := float64(k) / 6
			if side < 0 {
				t = 1 - t
			}
			wid := l * 0.32 * math.Sin(math.Pi*t)
			if k%2 == 1 {
				wid *= 1.45 // spike
			}
			if (side > 0 && k == 6) || (side < 0 && k == 0) {
				continue // the tip is added once
			}
			pts[n] = at(t*l, side*wid)
			n++
		}
		if side > 0 {
			pts[n] = at(l*1.08, 0)
			n++
		}
	}
	fan(f, x+ux*l*0.5, y+uy*l*0.5, pts[:n], mid, c)
	a0, a1 := at(0, 0), at(l, 0)
	f.quad(a0[0]-vx*0.4, a0[1]-vy*0.4, a1[0]-vx*0.2, a1[1]-vy*0.2, a1[0]+vx*0.2, a1[1]+vy*0.2, a0[0]+vx*0.4, a0[1]+vy*0.4, shade(c, 0.7))
}

// fan fills the closed polygon pts from the point (cx, cy) inside it, with
// color cc there fading to ec at the outline.
func fan(f *frame, cx, cy float64, pts [][2]float64, cc, ec rgba) {
	for k := range pts {
		a, b := pts[k], pts[(k+1)%len(pts)]
		f.tri(cx, cy, cc, a[0], a[1], ec, b[0], b[1], ec)
	}
}

// ellipse fills the arc of an ellipse from angle a0 to a1 (y grows
// downward, so 0..π is the lower half), fading from cc at the centre to ec
// at the edge.
func ellipse(f *frame, x, y, rx, ry, a0, a1 float64, cc, ec rgba) {
	const n = 20
	px, py := x+rx*math.Cos(a0), y+ry*math.Sin(a0)
	for k := 1; k <= n; k++ {
		a := a0 + (a1-a0)*float64(k)/n
		qx, qy := x+rx*math.Cos(a), y+ry*math.Sin(a)
		f.tri(x, y, cc, px, py, ec, qx, qy, ec)
		px, py = qx, qy
	}
}

// flicker is a smooth, irregular wobble in [-1, 1] for candle i.
func (s *Sim) flicker(i int) float64 {
	t, o := s.t, float64(i)*1.7
	return (math.Sin(t*9.1+o) + 0.6*math.Sin(t*15.7+o*2) + 0.4*math.Sin(t*23.3+o*3)) / 2
}

func shade(c rgba, k float32) rgba {
	return rgba{min(c[0]*k, 1), min(c[1]*k, 1), min(c[2]*k, 1), c[3]}
}

// mix blends a toward b by t.
func mix(a, b rgba, t float32) rgba {
	return rgba{a[0] + (b[0]-a[0])*t, a[1] + (b[1]-a[1])*t, a[2] + (b[2]-a[2])*t, a[3] + (b[3]-a[3])*t}
}
