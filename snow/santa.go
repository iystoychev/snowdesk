package snow

import "math"

// Santa walks in along the snowbank every few minutes with his sack,
// stops beside the Christmas tree to wave, and walks off the other side.
// He leaves footprints in the snow and hops when the cursor gets close.

const (
	santaSpeed    = 42.0  // pt/s
	santaFirst    = 25.0  // seconds after launch of the first visit
	santaMinGap   = 240.0 // seconds between visits
	santaMaxGap   = 480.0
	santaWave     = 3.5 // seconds he stops to wave
	santaStepLen  = 13.0
	santaHopSpeed = 190.0
	santaGravity  = 700.0
)

type santa struct {
	active        bool
	x, dir        float64 // feet position; dir is +1 walking right, -1 left
	walked        float64 // distance walked, for the stride and footprints
	stopX         float64
	waved, waving float64 // seconds waved so far; waving > 0 while stopped
	gave          bool    // left this visit's present
	hop, hopV     float64 // height above the ground
	lastPrint     float64
}

// CallSanta brings Santa in right away, unless he is already here.
func (s *Sim) CallSanta() {
	if s.Main && !s.claus.active && !s.full {
		s.startSanta()
	}
}

func (s *Sim) startSanta() {
	c := &s.claus
	*c = santa{active: true, dir: 1}
	if s.rng.Float64() < 0.5 {
		c.dir = -1
	}
	c.x = -40
	if c.dir < 0 {
		c.x = s.W + 40
	}
	c.stopX = s.W / 2
	for _, t := range s.trees {
		if t.xmas {
			c.stopX = t.x - c.dir*60 // beside the tree, on the side he comes from
		}
	}
}

func (s *Sim) stepSanta(dt float64, cursor Point) {
	s.stepGifts(dt)
	if !s.Main || !s.cfg.Santa {
		s.claus.active = false
		return
	}
	if s.santaNext == 0 {
		s.santaNext = s.t + santaFirst
	}
	c := &s.claus
	if !c.active {
		if s.t >= s.santaNext && !s.full {
			s.startSanta()
		}
		return
	}
	// Hop when the cursor comes close.
	gy := s.groundAt(c.x)
	if c.hop == 0 && math.Abs(cursor.X-c.x) < 30 && cursor.Y > gy-70 && cursor.Y < gy+10 {
		c.hopV = santaHopSpeed
	}
	if c.hopV != 0 || c.hop > 0 {
		c.hop += c.hopV * dt
		c.hopV -= santaGravity * dt
		if c.hop <= 0 {
			c.hop, c.hopV = 0, 0
		}
	}
	// Walk, stopping once beside the tree to wave.
	if c.waving > 0 {
		c.waving -= dt
		c.waved += dt
		if !c.gave && c.waved > santaWave*0.35 {
			c.gave = true
			s.leaveGift(c.x + c.dir*16) // set down in front of him, toward the tree
		}
		return
	}
	nx := c.x + c.dir*santaSpeed*dt
	if c.waved == 0 && (c.x-c.stopX)*(nx-c.stopX) <= 0 {
		c.waving = santaWave
	}
	c.walked += math.Abs(nx - c.x)
	c.x = nx
	if c.hop == 0 && c.walked-c.lastPrint > santaStepLen {
		c.lastPrint = c.walked
		s.footprint(c.x)
	}
	if c.x < -60 || c.x > s.W+60 {
		c.active = false
		s.santaNext = s.t + santaMinGap + s.rng.Float64()*(santaMaxGap-santaMinGap)
	}
}

// groundAt is the top of the snow on the snowbank at x.
func (s *Sim) groundAt(x float64) float64 {
	for _, id := range []uint32{GroundID, DockID} {
		if p, ok := s.piles[id]; ok && !p.hidden && p.visible(x) {
			return s.surface(p, x)
		}
	}
	return s.groundY
}

// footprint presses the snowbank down a little under a boot.
func (s *Sim) footprint(x float64) {
	p, ok := s.piles[GroundID]
	if !ok || !p.visible(x) {
		return
	}
	i := int((x - p.bounds.X) / colW)
	for j := max(i-1, 0); j <= min(i+1, len(p.cols)-1); j++ {
		p.cols[j] *= 0.55
	}
}

// --- drawing ---------------------------------------------------------------

func (s *Sim) drawSanta(f *frame) {
	c := &s.claus
	if !s.Main || !c.active {
		return
	}
	gx, gy := c.x, s.groundAt(c.x)-c.hop
	d := c.dir
	at := func(dx, dy float64) (float64, float64) { return gx + d*dx, gy + dy }
	quad := func(ax, ay, bx, by, cx, cy, dx, dy float64, col rgba) {
		x0, y0 := at(ax, ay)
		x1, y1 := at(bx, by)
		x2, y2 := at(cx, cy)
		x3, y3 := at(dx, dy)
		f.quad(x0, y0, x1, y1, x2, y2, x3, y3, col)
	}
	oval := func(dx, dy, rx, ry float64, cc, ec rgba) {
		x, y := at(dx, dy)
		ellipse(f, x, y, rx, ry, 0, 2*math.Pi, cc, ec)
	}
	red, darkRed := rgba{0.82, 0.08, 0.10, 1}, rgba{0.55, 0.03, 0.06, 1}
	fur := rgba{0.98, 0.98, 0.98, 1}
	black := rgba{0.08, 0.07, 0.07, 1}
	skin := rgba{1, 0.83, 0.70, 1}

	// Shadow on the snow.
	if c.hop < 20 {
		ellipse(f, gx, s.groundAt(c.x)+0.5, 13, 2.5, 0, 2*math.Pi, rgba{0.35, 0.4, 0.55, 0.28}, rgba{0.35, 0.4, 0.55, 0})
	}
	// Sack on his back.
	oval(-12, -27, 10, 12, rgba{0.62, 0.42, 0.22, 1}, rgba{0.42, 0.27, 0.13, 1})
	quad(-14, -40, -9, -40, -10, -36, -13, -36, rgba{0.42, 0.27, 0.13, 1})

	// Legs and boots, swinging in a stride while he walks.
	swing := 0.0
	if c.waving <= 0 && c.hop == 0 {
		swing = math.Sin(c.walked / santaStepLen * math.Pi)
	}
	for _, leg := range []float64{-1, 1} {
		fx := 4*leg*0.5 + swing*leg*5
		col := red
		if leg < 0 {
			col = darkRed // far leg
		}
		quad(-3+leg*2, -16, 3+leg*2, -16, fx+2.5, -5, fx-2.5, -5, col)
		quad(fx-3, -6, fx+5, -6, fx+6, 0, fx-3, 0, black)
	}

	// Coat, fur hem, belt and buckle.
	oval(0, -22, 11, 10.5, red, darkRed)
	quad(-11, -15, 11, -15, 10, -11.5, -10, -11.5, fur)
	quad(-11.2, -22, 11.2, -22, 11.2, -19, -11.2, -19, black)
	quad(-2.5, -22.6, 2.5, -22.6, 2.5, -18.4, -2.5, -18.4, rgba{1, 0.82, 0.25, 1})
	quad(2.8, -31, 4.5, -31, 4.5, -15, 2.8, -15, fur) // coat front trim

	// Arms: the near one waves while he stops.
	arm := func(sx, sy, hx, hy float64, col rgba) {
		quad(sx-2.5, sy, sx+2.5, sy, hx+2.2, hy, hx-2.2, hy, col)
		x, y := at(hx, hy)
		f.dot(x, y, 2.8, 0.8, black)
	}
	arm(-4, -29, -6-swing*3, -18, darkRed)
	if c.waving > 0 {
		w := math.Sin(c.waved * 10)
		arm(5, -29, 12+w*2.5, -42, red)
	} else {
		arm(5, -29, 7+swing*3, -18, red)
	}

	// Head: face, beard, moustache, rosy cheek and an eye.
	oval(2, -36, 5.8, 5.6, skin, shade(skin, 0.92))
	oval(3, -31, 7, 6.5, fur, rgba{0.9, 0.9, 0.92, 1})
	x0, y0 := at(-3, -32)
	x1, y1 := at(9, -32)
	x2, y2 := at(3, -23)
	f.tri(x0, y0, fur, x1, y1, fur, x2, y2, rgba{0.9, 0.9, 0.92, 1})
	oval(5, -34, 3.6, 1.6, fur, fur)
	ex, ey := at(5, -37.5)
	f.dot(ex, ey, 0.9, 0.8, black)
	cx, cy := at(6.5, -35.6)
	f.dot(cx, cy, 1.6, 0.2, rgba{1, 0.45, 0.45, 0.5})

	// Hat flopping back, with fur trim and a pompom.
	bob := math.Sin(c.walked/santaStepLen*math.Pi*2) * 0.8
	quad(-4.5, -40, 8.5, -40, 3, -46, -1, -46, red)
	tx, ty := at(-9, -45+bob)
	ax, ay := at(3, -46)
	bx, by := at(-1, -46)
	f.tri(ax, ay, red, bx, by, darkRed, tx, ty, darkRed)
	quad(-5.5, -42, 9.5, -42, 9.5, -38.8, -5.5, -38.8, fur)
	f.dot(tx, ty, 2.6, 0.75, fur)
}
