package snow

import "math"

// Christmas decorations: lights, icicles, wind, cursor sparkles, frost.

const (
	// Lights hang from the top edge of each window, skipping the traffic
	// light buttons on the left.
	lightsSkipLeft = 90.0
	lightsMinWidth = 220.0
	hookGap        = 70.0
	wireSag        = 7.0
	bulbGap        = 14.0

	icicleGrowTime = 80.0 // seconds for an icicle to reach full length

	sparkleCap  = 200
	sparkleLife = 0.7 // seconds

	frostGrowTime = 150.0 // seconds for a corner to frost over fully
	frostMinR     = 30.0
	frostMaxR     = 220.0
)

var bulbColors = []rgba{
	{1, 0.22, 0.2, 1},   // red
	{0.2, 0.95, 0.4, 1}, // green
	{1, 0.78, 0.2, 1},   // gold
	{0.3, 0.6, 1, 1},    // blue
}

type icicle struct {
	x, w, len, max float64 // x is relative to the window's left edge
}

type sparkle struct {
	x, y, vx, vy, life, phase float64
}

// crystal is a fixed spot in a frosted corner, in polar form.
type crystal struct{ ang, dist, r float64 }

// --- icicles ---------------------------------------------------------------

func (s *Sim) newIcicles(w float64) []icicle {
	var out []icicle
	for x := 10 + s.rng.Float64()*20; x < w-10; x += 12 + s.rng.Float64()*30 {
		out = append(out, icicle{x: x, w: 3 + s.rng.Float64()*3, max: 5 + s.rng.Float64()*17})
	}
	return out
}

// breakIcicles snaps every visible icicle off as shards moving at v.
func (s *Sim) breakIcicles(p *pile, v Point) {
	bottom := p.bounds.Y + p.bounds.H
	for i := range p.icicles {
		ic := &p.icicles[i]
		x := p.bounds.X + ic.x
		if ic.len > 1.5 && inSpans(p.bottom, x) {
			for n := 1 + int(ic.len/8); n > 0 && len(s.flakes) < hardCap; n-- {
				s.flakes = append(s.flakes, flake{
					x: x, y: bottom + s.rng.Float64()*ic.len,
					vx: v.X + (s.rng.Float64()-0.5)*40, vy: v.Y + s.rng.Float64()*30,
					r: 1 + s.rng.Float64(), phase: s.rng.Float64() * 2 * math.Pi,
				})
			}
		}
		ic.len = 0
	}
}

func (s *Sim) growIcicles(dt float64) {
	for _, p := range s.piles {
		for i := range p.icicles {
			ic := &p.icicles[i]
			if s.cfg.Icicles {
				ic.len = math.Min(ic.max, ic.len+ic.max/icicleGrowTime*dt)
			} else {
				ic.len = 0
			}
		}
	}
}

func (s *Sim) drawIcicles(f *frame) {
	tip := rgba{0.8, 0.92, 1, 0.55}
	root := rgba{0.92, 0.97, 1, 0.9}
	for _, p := range s.piles {
		bottom := p.bounds.Y + p.bounds.H
		for _, ic := range p.icicles {
			x := p.bounds.X + ic.x
			if ic.len < 0.5 || !inSpans(p.bottom, x) {
				continue
			}
			w := ic.w * math.Min(1, ic.len/6) // thin while short
			f.tri(x-w/2, bottom, root, x+w/2, bottom, root, x, bottom+ic.len, tip)
		}
	}
}

// --- lights ----------------------------------------------------------------

// wireY is the sagging wire's height at x between hooks h0 and h0+hookGap.
func wireY(top, h0, x float64) float64 {
	u := (x - h0) / hookGap
	return top + 2 + wireSag*4*u*(1-u)
}

func (s *Sim) drawLights(f *frame) {
	if !s.cfg.Lights {
		return
	}
	wire := rgba{0.08, 0.16, 0.1, 0.9}
	for id, p := range s.piles {
		b := p.bounds
		if id == DockID || b.W < lightsMinWidth || len(p.spans) == 0 {
			continue
		}
		start, end := b.X+lightsSkipLeft, b.X+b.W-20
		// Wire: short straight pieces along each sagging run.
		for x := start; x+3 < end; x += 4 {
			h0 := start + math.Floor((x-start)/hookGap)*hookGap
			x1 := math.Min(x+4, h0+hookGap)
			if !inSpans(p.spans, x) || !inSpans(p.spans, x1) {
				continue
			}
			y0, y1 := wireY(b.Y, h0, x), wireY(b.Y, h0, x1)
			f.quad(x, y0-0.6, x1, y1-0.6, x1, y1+0.6, x, y0+0.6, wire)
		}
		// Bulbs: a chasing twinkle runs along the string.
		for i, x := 0, start+bulbGap/2; x < end; i, x = i+1, x+bulbGap {
			if !inSpans(p.spans, x) {
				continue
			}
			h0 := start + math.Floor((x-start)/hookGap)*hookGap
			y := wireY(b.Y, h0, x) + 3
			lit := 0.35 + 0.65*(0.5+0.5*math.Sin(s.t*3-float64(i)*0.9))
			c := bulbColors[i%len(bulbColors)]
			glow := rgba{c[0], c[1], c[2], float32(0.35 * lit)}
			mix := float32(lit * 0.45) // hot bulbs go a little white
			core := rgba{c[0] + (1-c[0])*mix, c[1] + (1-c[1])*mix, c[2] + (1-c[2])*mix, 1}
			f.dot(x, y, 8, 0, glow)
			f.dot(x, y, 2.6, 0.6, core)
		}
	}
}

// --- wind ------------------------------------------------------------------

func (s *Sim) stepWind(dt float64) {
	if !s.cfg.Wind {
		s.windTarget = 0
	} else if s.t >= s.windNext {
		s.windNext = s.t + 5 + s.rng.Float64()*10
		if s.rng.Float64() < 0.4 {
			s.windTarget = 0 // calm
		} else {
			s.windTarget = math.Copysign(40+s.rng.Float64()*100, s.rng.Float64()-0.5)
		}
	}
	s.wind += (s.windTarget - s.wind) * math.Min(1, 0.6*dt)
}

// --- sparkles --------------------------------------------------------------

// stepSparkles drops glints along the cursor's path and ages old ones.
func (s *Sim) stepSparkles(dt float64, from, to Point) {
	if s.cfg.Sparkles {
		// Skip big jumps (first frame, screen changes): not a real stroke.
		if d := math.Hypot(to.X-from.X, to.Y-from.Y); d > 2 && d < 400 {
			s.sparkAcc += d / 7
			for ; s.sparkAcc >= 1 && len(s.sparks) < sparkleCap; s.sparkAcc-- {
				u := s.rng.Float64()
				s.sparks = append(s.sparks, sparkle{
					x:  from.X + (to.X-from.X)*u + (s.rng.Float64()-0.5)*6,
					y:  from.Y + (to.Y-from.Y)*u + (s.rng.Float64()-0.5)*6,
					vx: (s.rng.Float64() - 0.5) * 30, vy: (s.rng.Float64() - 0.5) * 30,
					life: sparkleLife * (0.6 + 0.4*s.rng.Float64()), phase: s.rng.Float64() * 6,
				})
			}
			s.sparkAcc = math.Min(s.sparkAcc, 1)
		}
	}
	live := s.sparks[:0]
	for _, sp := range s.sparks {
		sp.life -= dt
		if sp.life <= 0 {
			continue
		}
		sp.x += sp.vx * dt
		sp.y += sp.vy * dt
		sp.vy += 25 * dt
		live = append(live, sp)
	}
	s.sparks = live
}

func (s *Sim) drawSparkles(f *frame) {
	for _, sp := range s.sparks {
		a := sp.life / sparkleLife * (0.6 + 0.4*math.Sin(s.t*25+sp.phase))
		f.dot(sp.x, sp.y, 2.2, 0.3, rgba{1, 0.93, 0.7, float32(a)})
	}
}

// --- frost -----------------------------------------------------------------

type corner struct{ x, y, ang float64 }

// corners returns the screen corners and the angle where each one's inward
// quarter-circle starts (y grows downward).
func (s *Sim) corners() [4]corner {
	return [4]corner{{0, 0, 0}, {s.W, 0, math.Pi / 2}, {s.W, s.H, math.Pi}, {0, s.H, 3 * math.Pi / 2}}
}

func frostR(level float64) float64 { return frostMinR + (frostMaxR-frostMinR)*level }

// stepFrost grows frost in the corners and melts it near the cursor.
func (s *Sim) stepFrost(dt float64, cursor Point) {
	for i, c := range s.corners() {
		lv := &s.frost[i]
		switch {
		case !s.cfg.Frost:
			*lv = math.Max(0, *lv-0.5*dt)
		case math.Hypot(cursor.X-c.x, cursor.Y-c.y) < frostR(*lv)+60:
			*lv = math.Max(0, *lv-1.5*dt)
		default:
			*lv = math.Min(1, *lv+dt/frostGrowTime)
		}
	}
}

func (s *Sim) drawFrost(f *frame) {
	for i, c := range s.corners() {
		lv := s.frost[i]
		if lv < 0.01 {
			continue
		}
		r := frostR(lv)
		center := rgba{0.85, 0.93, 1, float32(0.45 * lv)}
		edge := rgba{0.85, 0.93, 1, 0}
		const segs = 12
		for k := 0; k < segs; k++ {
			a0 := c.ang + math.Pi/2*float64(k)/segs
			a1 := c.ang + math.Pi/2*float64(k+1)/segs
			f.tri(c.x, c.y, center,
				c.x+r*math.Cos(a0), c.y+r*math.Sin(a0), edge,
				c.x+r*math.Cos(a1), c.y+r*math.Sin(a1), edge)
		}
		for _, cr := range s.crystals[i] {
			if cr.dist > lv {
				continue // crystals appear as the frost reaches them
			}
			d := cr.dist * r * 0.9
			f.dot(c.x+d*math.Cos(c.ang+cr.ang), c.y+d*math.Sin(c.ang+cr.ang), cr.r, 0.3,
				rgba{0.95, 0.98, 1, float32(0.7 * lv)})
		}
	}
}

func (s *Sim) seedCrystals() {
	for i := range s.crystals {
		s.crystals[i] = make([]crystal, 16)
		for k := range s.crystals[i] {
			s.crystals[i][k] = crystal{
				ang: s.rng.Float64() * math.Pi / 2, dist: 0.1 + s.rng.Float64()*0.9, r: 1 + s.rng.Float64()*1.4,
			}
		}
	}
}

func inSpans(spans [][2]float64, x float64) bool {
	for _, s := range spans {
		if x >= s[0] && x < s[1] {
			return true
		}
	}
	return false
}
