package snow

import "math"

// The winter scene along the bottom of the main screen: a snowbank on the
// Dock's level, a forest of evergreens standing on it, and a Christmas
// tree with twinkling lights. Snow settles on every branch tier and the
// cursor brushes it off. Only the Sim marked Main draws a scene.

// GroundID is the pile ID of the snowbank along the bottom of the screen.
const GroundID = DockID - 1

const (
	groundCap     = 16.0 // the snowbank's pile grows deeper than a window's
	treeCatch     = 0.35 // chance a tier catches a flake falling into it
	treeSnowStep  = 0.025
	xmasTreeH     = 150.0
	xmasClearance = 75.0 // forest trees keep this far from the Christmas tree
)

// tier is one layer of branches: a triangle from the apex (ax, ay) down to
// a base at by, hw wide on each side. snow is how covered it is, 0..1.
type tier struct {
	ax, ay, by, hw, snow float64
}

func (t *tier) contains(x, y, pad float64) bool {
	if y < t.ay-pad || y > t.by+pad {
		return false
	}
	k := math.Max(0, (y-t.ay)/(t.by-t.ay))
	return math.Abs(x-t.ax) <= t.hw*k+pad
}

type tree struct {
	x, base, h float64
	back, xmas bool
	tiers      []tier
	presents   bool
}

// groundY is the snowbank's top: level with the Dock's top, or the screen
// bottom when there is no Dock ledge.
func (s *Sim) groundLevel(dock Rect) float64 {
	if dock.W > 0 {
		return dock.Y
	}
	return s.H
}

// updateGround keeps the snowbank pile in step with the screen and Dock.
func (s *Sim) updateGround(dock Rect, dt float64, calm bool) {
	if !s.Main {
		return
	}
	s.dock = dock
	s.groundY = s.groundLevel(dock)
	if s.full {
		if p, ok := s.piles[GroundID]; ok {
			p.seen, p.hidden = true, true
			p.spans, p.bottom = nil, nil
		}
		return
	}
	var front []Rect
	if dock.W > 0 {
		front = []Rect{dock}
	}
	s.updatePile(GroundID, Rect{0, s.groundY, s.W, 0}, front, dt, calm)
	if p := s.piles[GroundID]; p.cap == 0 {
		p.cap, p.always = groundCap, true
	}
	s.layoutTrees()
}

// layoutTrees places the forest and the Christmas tree. It only starts
// over (losing their snow) when the screen, the ground or the settings
// change.
func (s *Sim) layoutTrees() {
	key := [5]float64{s.W, s.groundY, s.dock.X, b2f(s.cfg.Forest), b2f(s.cfg.XmasTree)}
	if key == s.treeKey {
		return
	}
	s.treeKey = key
	s.trees = s.trees[:0]
	rng := newHashRNG(uint64(s.W)*31 + uint64(s.groundY))
	xmasX := -1e9
	if s.cfg.XmasTree {
		xmasX = s.xmasSpot()
	}
	if s.cfg.Forest {
		for x := -10 + rng.next()*15; x < s.W+10; x += 16 + rng.next()*22 {
			s.trees = append(s.trees, s.newTree(x, 38+rng.next()*34, true, false))
		}
		for x := 10 + rng.next()*40; x < s.W; x += 55 + rng.next()*75 {
			if math.Abs(x-xmasX) < xmasClearance {
				continue
			}
			s.trees = append(s.trees, s.newTree(x, 58+rng.next()*42, false, false))
		}
	}
	if s.cfg.XmasTree {
		s.trees = append(s.trees, s.newTree(xmasX, xmasTreeH, false, true))
	}
}

// xmasSpot puts the Christmas tree in the open space beside the Dock.
func (s *Sim) xmasSpot() float64 {
	if s.dock.W <= 0 {
		return s.W * 0.86
	}
	right := s.dock.X + s.dock.W
	if gap := s.W - right; gap >= 150 {
		return right + math.Min(gap*0.5, 150)
	}
	if s.dock.X >= 150 {
		return s.dock.X - math.Min(s.dock.X*0.5, 150)
	}
	return s.W * 0.86
}

func (s *Sim) newTree(x, h float64, back, xmas bool) tree {
	t := tree{x: x, base: s.groundY + 3, h: h, back: back, xmas: xmas, presents: xmas}
	n := 4
	if xmas {
		n = 5
	}
	trunk := h * 0.1
	top := t.base - h
	th := (h - trunk) / (float64(n)*0.72 + 0.28)
	for i := range n {
		ay := top + float64(i)*th*0.72
		t.tiers = append(t.tiers, tier{
			ax: x, ay: ay, by: ay + th,
			hw: h * (0.13 + 0.25*float64(i+1)/float64(n)),
		})
	}
	return t
}

// landTree reports whether a flake falling from y0 to y1 at x settled on
// a branch tier, and adds to that tier's snow.
func (s *Sim) landTree(x, y0, y1 float64) bool {
	if len(s.trees) == 0 || y1 < s.groundY-xmasTreeH-10 {
		return false
	}
	for i := len(s.trees) - 1; i >= 0; i-- { // front trees first
		t := &s.trees[i]
		if math.Abs(x-t.x) > t.h*0.4 {
			continue
		}
		for k := range t.tiers {
			tr := &t.tiers[k]
			if tr.contains(x, y1, 0) && !tr.contains(x, y0, 0) && s.rng.Float64() < treeCatch {
				tr.snow = math.Min(1, tr.snow+treeSnowStep)
				return true
			}
		}
	}
	return false
}

// brushTrees knocks the snow off tiers the cursor sweeps through.
func (s *Sim) brushTrees(c, cv Point) {
	for i := range s.trees {
		t := &s.trees[i]
		if math.Abs(c.X-t.x) > t.h*0.45 || c.Y < t.base-t.h-20 || c.Y > t.base {
			continue
		}
		for k := range t.tiers {
			tr := &t.tiers[k]
			if tr.snow < 0.05 || !tr.contains(c.X, c.Y, 8) {
				continue
			}
			for n := 2 + int(tr.snow*10); n > 0 && len(s.flakes) < hardCap; n-- {
				s.flakes = append(s.flakes, flake{
					x:  tr.ax + (s.rng.Float64()-0.5)*tr.hw*1.6,
					y:  tr.ay + (tr.by-tr.ay)*s.rng.Float64(),
					vx: math.Copysign(40+s.rng.Float64()*80, tr.ax-c.X) + cv.X*0.3,
					vy: -(30 + s.rng.Float64()*80),
					r:  0.8 + s.rng.Float64()*1.2,
				})
			}
			tr.snow = 0
		}
	}
}

// --- drawing ---------------------------------------------------------------

// drawGround fills the snowbank beside the Dock, from the ground line down
// to the bottom of the screen.
func (s *Sim) drawGround(f *frame) {
	if !s.Main || s.groundY >= s.H-0.5 {
		return
	}
	top := rgba{0.96, 0.97, 1, 1}
	low := rgba{0.80, 0.86, 0.95, 1}
	spans := [][2]float64{{0, s.W}}
	if s.dock.W > 0 {
		spans = subtract(spans, s.dock.X+6, s.dock.X+s.dock.W-6)
	}
	for _, sp := range spans {
		for x := sp[0]; x < sp[1]; x += 6 {
			x1 := math.Min(x+6, sp[1])
			y0 := s.groundY + 1.5*math.Sin(x*0.045)
			y1 := s.groundY + 1.5*math.Sin(x1*0.045)
			f.tri(x, y0, top, x1, y1, top, x1, s.H, low)
			f.tri(x, y0, top, x1, s.H, low, x, s.H, low)
		}
	}
}

func (s *Sim) drawTrees(f *frame) {
	if !s.Main {
		return
	}
	for _, back := range []bool{true, false} {
		for i := range s.trees {
			if t := &s.trees[i]; t.back == back && !t.xmas {
				s.drawTree(f, t)
			}
		}
	}
	for i := range s.trees {
		if t := &s.trees[i]; t.xmas {
			s.drawTree(f, t)
			s.drawXmasExtras(f, t)
		}
	}
}

func (s *Sim) drawTree(f *frame, t *tree) {
	light, dark := rgba{0.13, 0.40, 0.25, 1}, rgba{0.06, 0.24, 0.15, 1}
	switch {
	case t.back:
		light, dark = rgba{0.16, 0.30, 0.30, 1}, rgba{0.10, 0.21, 0.23, 1}
	case t.xmas:
		light, dark = rgba{0.15, 0.50, 0.26, 1}, rgba{0.05, 0.30, 0.14, 1}
	}
	// Trunk.
	tw := math.Max(2.5, t.h*0.05)
	bark := rgba{0.33, 0.21, 0.12, 1}
	last := t.tiers[len(t.tiers)-1]
	f.quad(t.x-tw, last.by-2, t.x+tw, last.by-2, t.x+tw, t.base, t.x-tw, t.base, bark)
	white := rgba{0.97, 0.98, 1, 1}
	for k := len(t.tiers) - 1; k >= 0; k-- { // lower tiers first: upper ones overlap them
		tr := t.tiers[k]
		droop := (tr.by - tr.ay) * 0.12
		lx, rx := tr.ax-tr.hw, tr.ax+tr.hw
		f.tri(tr.ax, tr.ay, light, lx, tr.by, light, tr.ax, tr.by+droop, shade(light, 0.9))
		f.tri(tr.ax, tr.ay, dark, tr.ax, tr.by+droop, dark, rx, tr.by, dark)
		if tr.snow < 0.02 {
			continue
		}
		// A cap of snow on the upper slope, and a rim along the branch tips.
		c := 0.2 + 0.5*tr.snow
		cy := tr.ay + (tr.by-tr.ay)*c
		cw := tr.hw * c
		a := float32(math.Min(1, tr.snow*2.5))
		w := rgba{white[0], white[1], white[2], a}
		f.tri(tr.ax, tr.ay-0.8, w, tr.ax-cw, cy, w, tr.ax, cy+cw*0.18, w)
		f.tri(tr.ax, tr.ay-0.8, w, tr.ax, cy+cw*0.18, w, tr.ax+cw, cy, w)
		rim := 0.8 + 2.4*tr.snow
		f.quad(lx, tr.by-rim, tr.ax, tr.by+droop-rim, tr.ax, tr.by+droop, lx, tr.by, w)
		f.quad(tr.ax, tr.by+droop-rim, rx, tr.by-rim, rx, tr.by, tr.ax, tr.by+droop, w)
	}
}

// drawXmasExtras adds the lights, the star and the presents.
func (s *Sim) drawXmasExtras(f *frame, t *tree) {
	if t.presents {
		s.ensureGifts()
		for _, g := range s.gifts {
			x, y := t.x+g.dx, t.base-2-g.drop
			w, h := g.w, g.h
			f.quad(x, y-h, x+w, y-h, x+w, y, x, y, g.box)
			f.quad(x+w*0.43, y-h, x+w*0.57, y-h, x+w*0.57, y, x+w*0.43, y, g.ribbon)
			f.quad(x, y-h*0.58, x+w, y-h*0.58, x+w, y-h*0.42, x, y-h*0.42, g.ribbon)
			f.dot(x+w*0.5, y-h-1.5, 2.4, 0.8, g.ribbon)
		}
	}
	// Bulbs along each tier's lower edge, twinkling in a chase. Snow on a
	// tier dims its lights, as if they glow through it.
	n := 0
	for _, tr := range t.tiers {
		droop := (tr.by - tr.ay) * 0.12
		count := max(3, int(tr.hw/8))
		for j := range count {
			u := (float64(j)+0.5)/float64(count)*2 - 1 // -1..1 across the tier
			x := tr.ax + u*tr.hw*0.86
			y := tr.by - 2 + droop*(1-u*u) - 3*(1-math.Abs(u))
			c := bulbColors[n%len(bulbColors)]
			b := 0.55 + 0.45*math.Sin(s.t*2.6-float64(n)*0.9)
			dim := 1 - 0.55*tr.snow
			f.dot(x, y, 7, 0, rgba{c[0], c[1], c[2], float32(0.35 * b * dim)})
			f.dot(x, y, 2.3, 0.7, rgba{c[0], c[1], c[2], float32((0.6 + 0.4*b) * (0.5 + 0.5*dim))})
			n++
		}
	}
	// A gold star on top, gently pulsing.
	top := t.tiers[0]
	sx, sy := t.x, top.ay-5
	pulse := 0.8 + 0.2*math.Sin(s.t*2)
	f.dot(sx, sy, 16, 0, rgba{1, 0.85, 0.35, float32(0.3 * pulse)})
	gold, deep := rgba{1, 0.88, 0.35, 1}, rgba{0.95, 0.65, 0.1, 1}
	for k := range 5 {
		a := -math.Pi/2 + float64(k)*2*math.Pi/5
		a1, a2 := a-math.Pi/5, a+math.Pi/5
		f.tri(sx, sy, gold, sx+8*math.Cos(a), sy+8*math.Sin(a), deep, sx+3.4*math.Cos(a1), sy+3.4*math.Sin(a1), gold)
		f.tri(sx, sy, gold, sx+3.4*math.Cos(a2), sy+3.4*math.Sin(a2), gold, sx+8*math.Cos(a), sy+8*math.Sin(a), deep)
	}
}

// hashRNG is a tiny deterministic generator, so the forest keeps the same
// shape every time it is laid out for the same screen.
type hashRNG struct{ seed, k uint64 }

func newHashRNG(seed uint64) *hashRNG { return &hashRNG{seed: seed} }

func (r *hashRNG) next() float64 {
	r.k++
	return hash01(r.seed, 7, int(r.k))
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// gift is a present under the Christmas tree; dx is its left edge
// relative to the tree, drop its height above the ground while it falls.
type gift struct {
	dx, w, h, drop, vy float64
	box, ribbon        rgba
}

const maxGifts = 10

var giftColors = [][2]rgba{
	{{0.75, 0.10, 0.14, 1}, {1, 0.82, 0.3, 1}},     // red, gold ribbon
	{{0.16, 0.36, 0.72, 1}, {0.96, 0.96, 1, 1}},    // blue, white
	{{0.95, 0.80, 0.25, 1}, {0.75, 0.10, 0.14, 1}}, // gold, red
	{{0.12, 0.50, 0.28, 1}, {0.85, 0.12, 0.15, 1}}, // green, red
	{{0.48, 0.24, 0.62, 1}, {1, 0.82, 0.3, 1}},     // purple, gold
	{{0.96, 0.96, 0.98, 1}, {0.80, 0.10, 0.14, 1}}, // white, red
}

// ensureGifts puts the first three presents under the tree.
func (s *Sim) ensureGifts() {
	if len(s.gifts) == 0 {
		s.gifts = []gift{
			{dx: -38, w: 18, h: 13, box: giftColors[0][0], ribbon: giftColors[0][1]},
			{dx: 20, w: 22, h: 16, box: giftColors[1][0], ribbon: giftColors[1][1]},
			{dx: -22, w: 12, h: 9, box: giftColors[2][0], ribbon: giftColors[2][1]},
		}
	}
}

// leaveGift drops a new present from Santa's hand at x, beside the tree.
// The oldest extra present goes once there are too many.
func (s *Sim) leaveGift(x float64) {
	var t *tree
	for i := range s.trees {
		if s.trees[i].xmas {
			t = &s.trees[i]
		}
	}
	if t == nil {
		return
	}
	s.ensureGifts()
	if len(s.gifts) >= maxGifts {
		s.gifts = append(s.gifts[:3], s.gifts[4:]...)
	}
	c := giftColors[s.rng.IntN(len(giftColors))]
	w := 11 + s.rng.Float64()*11
	s.gifts = append(s.gifts, gift{
		dx: x - t.x - w/2, w: w, h: 8 + s.rng.Float64()*9,
		drop: 24, box: c[0], ribbon: c[1],
	})
}

// stepGifts lets falling presents land.
func (s *Sim) stepGifts(dt float64) {
	for i := range s.gifts {
		g := &s.gifts[i]
		if g.drop > 0 {
			g.vy += santaGravity * dt
			g.drop = math.Max(0, g.drop-g.vy*dt)
			if g.drop == 0 {
				g.vy = 0
			}
		}
	}
}
