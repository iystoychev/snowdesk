package snow

import "math"

// Extra effects: shooting stars, fireflies and falling leaves. Each one is
// a menu toggle and off by default.

const (
	starGapMin = 3.0   // seconds between shooting stars, at least
	starGapMax = 9.0   // and at most
	starSpeed  = 900.0 // pt/s
	starLife   = 0.9   // seconds a star streaks before it burns out
	starTail   = 140.0 // tail length

	fireflyCount = 24
	fireflySpeed = 28.0 // pt/s cruising speed
	fireflyFlee  = 90.0 // fireflies dart away from the cursor within this radius

	leafCap  = 30
	leafRate = 1.2  // new leaves per second
	leafFall = 45.0 // pt/s terminal fall speed
	leafSize = 9.0  // half-length of an average leaf
)

var leafColors = []rgba{
	{0.93, 0.45, 0.12, 1}, // orange
	{0.80, 0.18, 0.12, 1}, // red
	{0.96, 0.75, 0.20, 1}, // yellow
	{0.62, 0.36, 0.16, 1}, // brown
}

type star struct {
	x, y, vx, vy, life float64
}

type firefly struct {
	x, y, heading, phase, period float64
}

type leaf struct {
	x, y, vx, vy float64
	size         float64
	spin, rot    float64 // rotation speed (rad/s) and angle
	flip, flipV  float64 // tumble angle and speed: squashes the leaf to fake 3D
	color        int
}

func (s *Sim) stepExtras(dt float64, cursor, cv Point) {
	s.stepStars(dt)
	s.stepFireflies(dt, cursor)
	s.stepLeaves(dt, cursor, cv)
}

func (s *Sim) drawExtras(f *frame) {
	s.drawStars(f)
	s.drawLeaves(f)
	s.drawFireflies(f)
}

// --- shooting stars --------------------------------------------------------

func (s *Sim) stepStars(dt float64) {
	if s.cfg.Stars && s.t >= s.starNext {
		s.starNext = s.t + starGapMin + s.rng.Float64()*(starGapMax-starGapMin)
		// Start in the upper part of the screen and streak down at a shallow
		// angle, left or right.
		ang := 0.25 + s.rng.Float64()*0.35 // radians below horizontal
		dir := math.Copysign(1, s.rng.Float64()-0.5)
		s.stars = append(s.stars, star{
			x:    s.W * (0.1 + s.rng.Float64()*0.8),
			y:    s.H * s.rng.Float64() * 0.35,
			vx:   dir * starSpeed * math.Cos(ang),
			vy:   starSpeed * math.Sin(ang),
			life: starLife * (0.7 + 0.3*s.rng.Float64()),
		})
	}
	live := s.stars[:0]
	for _, st := range s.stars {
		st.life -= dt
		if st.life <= 0 {
			continue
		}
		st.x += st.vx * dt
		st.y += st.vy * dt
		live = append(live, st)
	}
	s.stars = live
}

func (s *Sim) drawStars(f *frame) {
	for _, st := range s.stars {
		// Full brightness, then fading as it burns out.
		a := float32(math.Min(1, st.life/(starLife*0.4)))
		sp := math.Hypot(st.vx, st.vy)
		ux, uy := st.vx/sp, st.vy/sp
		tx, ty := st.x-ux*starTail, st.y-uy*starTail
		nx, ny := -uy*1.6, ux*1.6 // half the tail's width at the head
		head := rgba{1, 0.97, 0.88, 0.9 * a}
		gone := rgba{0.7, 0.8, 1, 0}
		f.tri(st.x+nx, st.y+ny, head, st.x-nx, st.y-ny, head, tx, ty, gone)
		f.dot(st.x, st.y, 6, 0, rgba{1, 0.95, 0.8, 0.5 * a})
		f.dot(st.x, st.y, 2, 0.5, rgba{1, 1, 1, a})
	}
}

// --- fireflies -------------------------------------------------------------

func (s *Sim) stepFireflies(dt float64, cursor Point) {
	if !s.cfg.Fireflies {
		s.flies = s.flies[:0]
		return
	}
	for len(s.flies) < fireflyCount {
		s.flies = append(s.flies, firefly{
			x: s.rng.Float64() * s.W, y: s.H * (0.2 + 0.8*s.rng.Float64()),
			heading: s.rng.Float64() * 2 * math.Pi,
			phase:   s.rng.Float64() * 10, period: 2.5 + s.rng.Float64()*3,
		})
	}
	for i := range s.flies {
		ff := &s.flies[i]
		// Wander: the heading drifts a little at random.
		ff.heading += (s.rng.Float64() - 0.5) * 4 * dt
		// Near an edge, turn back toward the middle of the screen.
		if ff.x < 40 || ff.x > s.W-40 || ff.y < 40 || ff.y > s.H-40 {
			ff.heading = turnToward(ff.heading, math.Atan2(s.H*0.6-ff.y, s.W/2-ff.x), 2*dt)
		}
		v := fireflySpeed
		if dx, dy := ff.x-cursor.X, ff.y-cursor.Y; dx*dx+dy*dy < fireflyFlee*fireflyFlee {
			ff.heading = turnToward(ff.heading, math.Atan2(dy, dx), 6*dt)
			v *= 3
		}
		ff.x = clampF(ff.x+math.Cos(ff.heading)*v*dt, -20, s.W+20)
		ff.y = clampF(ff.y+math.Sin(ff.heading)*v*dt, -20, s.H+20)
	}
}

// glow is how bright a firefly is now, 0..1: dim most of the time, with a
// smooth flash once per period.
func (ff *firefly) glow(t float64) float64 {
	p := math.Mod(t+ff.phase, ff.period) / ff.period
	if p > 0.4 {
		return 0.08
	}
	return math.Max(0.08, math.Sin(p/0.4*math.Pi))
}

func (s *Sim) drawFireflies(f *frame) {
	for i := range s.flies {
		ff := &s.flies[i]
		g := float32(ff.glow(s.t))
		f.dot(ff.x, ff.y, 11, 0, rgba{0.75, 1, 0.3, 0.35 * g})
		f.dot(ff.x, ff.y, 2.2, 0.5, rgba{0.95, 1, 0.6, 0.3 + 0.7*g})
	}
}

// turnToward rotates heading a toward b by at most step radians.
func turnToward(a, b, step float64) float64 {
	d := math.Remainder(b-a, 2*math.Pi)
	return a + math.Max(-step, math.Min(step, d))
}

// --- falling leaves --------------------------------------------------------

func (s *Sim) stepLeaves(dt float64, cursor, cv Point) {
	if s.cfg.Leaves {
		s.leafAcc += leafRate * dt
		for ; s.leafAcc >= 1; s.leafAcc-- {
			if len(s.leaves) >= leafCap {
				continue
			}
			s.leaves = append(s.leaves, leaf{
				x: s.rng.Float64() * s.W, y: -15,
				vy:   leafFall * 0.5,
				size: leafSize * (0.7 + 0.6*s.rng.Float64()),
				spin: (s.rng.Float64() - 0.5) * 3, rot: s.rng.Float64() * 2 * math.Pi,
				flip: s.rng.Float64() * 2 * math.Pi, flipV: 1.5 + s.rng.Float64()*2.5,
				color: s.rng.IntN(len(leafColors)),
			})
		}
	}
	k := math.Exp(-drag * dt)
	live := s.leaves[:0]
	for _, l := range s.leaves {
		// The cursor bats leaves away and sets them spinning.
		if dx, dy := l.x-cursor.X, l.y-cursor.Y; dx*dx+dy*dy < pushR*pushR {
			d := math.Max(math.Hypot(dx, dy), 1)
			w := 1 - d/pushR
			l.vx += (dx/d*pushForce + cv.X*3) * w * dt
			l.vy += (dy/d*pushForce + cv.Y*3) * w * dt
			l.spin += (s.rng.Float64() - 0.5) * 20 * w * dt
		}
		l.vx *= k
		l.vy = leafFall + (l.vy-leafFall)*k // ease toward the fall speed
		l.flip += l.flipV * dt
		l.rot += l.spin * dt
		// Leaves swing side to side as they fall, and feel the wind more
		// than snow does.
		sway := math.Sin(l.flip*0.5) * 40
		l.x += (l.vx + sway + s.wind*1.5) * dt
		l.y += l.vy * dt
		if l.y > s.H+20 || l.x < -60 || l.x > s.W+60 {
			continue
		}
		live = append(live, l)
	}
	s.leaves = live
}

// leafShape is the outline of a leaf with half-length 1, tip at +x.
var leafShape = [][2]float64{
	{-1, 0}, {-0.55, 0.38}, {0, 0.5}, {0.5, 0.36}, {1, 0},
	{0.5, -0.36}, {0, -0.5}, {-0.55, -0.38},
}

func (s *Sim) drawLeaves(f *frame) {
	for _, l := range s.leaves {
		c := leafColors[l.color]
		dark := rgba{c[0] * 0.6, c[1] * 0.6, c[2] * 0.6, 1}
		cos, sin := math.Cos(l.rot), math.Sin(l.rot)
		// Tumbling squashes the leaf across its width (never quite flat).
		sq := 0.15 + 0.85*math.Abs(math.Cos(l.flip))
		pt := func(u, v float64) (float64, float64) {
			u, v = u*l.size, v*l.size*sq
			return l.x + u*cos - v*sin, l.y + u*sin + v*cos
		}
		n := len(leafShape)
		for i := range n {
			ax, ay := pt(leafShape[i][0], leafShape[i][1])
			bx, by := pt(leafShape[(i+1)%n][0], leafShape[(i+1)%n][1])
			f.tri(l.x, l.y, c, ax, ay, c, bx, by, c)
		}
		// Midrib and stem.
		x0, y0 := pt(-1.35, 0)
		x1, y1 := pt(0.85, 0)
		nx, ny := -sin*0.7*sq, cos*0.7*sq
		f.quad(x0+nx, y0+ny, x1+nx, y1+ny, x1-nx, y1-ny, x0-nx, y0-ny, dark)
	}
}
