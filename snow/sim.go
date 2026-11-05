// Package snow is the platform-free snow simulation: falling flakes,
// piles that build up on window top edges, and the ways piles shed.
// All coordinates are screen points with the origin at the top-left.
package snow

import (
	"math"
	"math/rand/v2"
)

type Point struct{ X, Y float64 }

type Rect struct{ X, Y, W, H float64 }

// Window is an on-screen window. Lists of windows are ordered front to back.
type Window struct {
	ID     uint32
	Bounds Rect
	// Hidden marks a window that still exists but is not on screen (another
	// Space, minimized). Its pile is kept, not drawn, until it comes back.
	Hidden bool
}

// DockID is the pile ID used for the Dock ledge.
const DockID = ^uint32(0)

const (
	colW        = 3.0    // pile column width
	maxPile     = 9.0    // max pile height
	edgeTaper   = 14.0   // pile tapers to zero over this distance at window corners
	flakeCap    = 400    // spawn new flakes only below this count
	hardCap     = 2000   // absolute flake limit, including shed snow
	spawnRate   = 90.0   // new flakes per second
	gravity     = 120.0  // pt/s²
	drag        = 2.0    // 1/s; falling terminal speed = gravity/drag
	flingSpeed  = 350.0  // window speed (pt/s) that throws its pile off
	brushR      = 24.0   // cursor brush radius
	perFlake    = 1.0    // pile height one flake adds (spread over 3 columns)
	catchChance = 0.6    // chance a ledge catches a flake that crosses it
	pushR       = 60.0   // cursor pushes falling flakes within this radius
	pushForce   = 900.0  // pt/s² outward push at the cursor
	refSpeed    = 1500.0 // clamp for window and cursor velocity estimates
	settleTime  = 0.05   // a desktop slide counts as over one window update after the last slide step
	adoptScaled = 30.0   // seconds a same-shape size change may last before it counts as a real resize
	flingDelay  = 0.2    // a fast move must last this long without being a desktop slide to count as a fling
	driftMin    = 0.35   // lowest point of a pile, as a share of maxPile
	driftWide   = 14     // columns between big drifts (~42 pt)
	driftFine   = 4      // columns between small ripples (~12 pt)
)

type flake struct {
	x, y, vx, vy, r, phase float64
}

// pile is everything attached to one window (or the Dock): the snow on its
// top edge and the icicles under its bottom edge.
type pile struct {
	bounds   Rect
	cols     []float64
	spans    [][2]float64 // visible x-ranges of the top edge
	bottom   [][2]float64 // visible x-ranges of the bottom edge
	icicles  []icicle
	seen     bool
	hidden   bool    // window was off screen (other Space, minimized) last update
	flingAt  float64 // window clock when a fast move began; 0 = none pending
	flingV   Point   // latest speed of that move
	scaledAt float64 // window clock when Mission Control-style scaling began; 0 = not scaled
	seed     uint64  // picks this window's drift shape
}

// Sim is the whole snow state. It is not safe for concurrent use.
type Sim struct {
	W, H     float64
	cfg      Settings
	flakes   []flake
	piles    map[uint32]*pile
	spawnAcc float64
	t        float64
	lastCur  Point
	rng      *rand.Rand

	wind, windTarget, windNext float64 // sideways gust speed, pt/s
	sparks                     []sparkle
	sparkAcc                   float64
	frost                      [4]float64 // per screen corner, 0..1
	crystals                   [4][]crystal
	winClock                   float64 // seconds of window updates (sum of SetWindows dt)
	settleUntil                float64 // a desktop slide is under way until winClock reaches this

}

// Settings are the user-adjustable knobs.
type Settings struct {
	Amount   float64 // snowfall multiplier; 1 = normal (90 flakes/s, 400 on screen)
	Size     float64 // flake radius multiplier; 1 = normal
	Piles    bool    // snow builds up on windows and the Dock
	Lights   bool    // Christmas lights along window top edges
	Icicles  bool    // icicles grow under window bottom edges
	Wind     bool    // gusts blow the snow sideways now and then
	Sparkles bool    // the cursor leaves a trail of glints
	Frost    bool    // frost creeps in from the screen corners
}

func DefaultSettings() Settings {
	return Settings{Amount: 1, Size: 1, Piles: true, Lights: true, Icicles: true, Wind: true, Sparkles: true, Frost: true}
}

func New(w, h float64, seed uint64) *Sim {
	s := &Sim{
		W: w, H: h,
		cfg:   DefaultSettings(),
		piles: map[uint32]*pile{},
		rng:   rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)),
	}
	s.seedCrystals()
	return s
}

// PileIDs returns the IDs of windows that currently have a pile.
func (s *Sim) PileIDs() []uint32 {
	ids := make([]uint32, 0, len(s.piles))
	for id := range s.piles {
		if id != DockID {
			ids = append(ids, id)
		}
	}
	return ids
}

// Configure applies new settings. Turning piles off drops all piled snow.
func (s *Sim) Configure(c Settings) {
	c.Amount = math.Max(0, math.Min(c.Amount, float64(hardCap)/flakeCap))
	c.Size = math.Max(0.1, c.Size)
	if s.cfg.Piles && !c.Piles {
		for _, p := range s.piles {
			s.shed(p, 0, len(p.cols), Point{}, true)
		}
	}
	s.cfg = c
}

// SetWindows updates ledges from the current window list (front to back)
// and the Dock rect (zero W means no Dock ledge). dt is the time since the
// previous call; it is used to estimate how fast windows move.
func (s *Sim) SetWindows(ws []Window, dock Rect, dt float64) {
	for _, p := range s.piles {
		p.seen = false
	}
	// While macOS slides between desktops, windows report fast sideways
	// moves. That is not a fling: keep their snow and icicles.
	s.winClock += dt
	if s.slideStarted(ws) {
		s.settleUntil = s.winClock + settleTime
	}
	calm := s.Sliding()
	// The Dock sits above every window, so it is the front-most occluder.
	occluders := make([]Rect, 0, len(ws)+1)
	if dock.W > 0 {
		s.updatePile(DockID, dock, nil, dt, calm)
		occluders = append(occluders, dock)
	}
	for _, w := range ws {
		if w.Hidden {
			if p, ok := s.piles[w.ID]; ok {
				p.seen = true
				p.hidden = true
				p.flingAt = 0 // it slid away to another desktop: not a fling
				p.spans, p.bottom = nil, nil
			}
			continue
		}
		s.updatePile(w.ID, w.Bounds, occluders, dt, calm)
		occluders = append(occluders, w.Bounds)
	}
	for _, p := range s.piles {
		s.resolveFling(p, calm)
	}
	for id, p := range s.piles {
		if !p.seen {
			s.shed(p, 0, len(p.cols), Point{}, true)
			s.breakIcicles(p, Point{})
			delete(s.piles, id)
		}
	}
}

func (s *Sim) updatePile(id uint32, b Rect, front []Rect, dt float64, calm bool) {
	p, ok := s.piles[id]
	if !ok {
		p = &pile{bounds: b, cols: make([]float64, ncols(b.W)), seed: uint64(id)}
		if id != DockID { // the Dock's bottom is the screen edge
			p.icicles = s.newIcicles(b.W)
		}
		s.piles[id] = p
	}
	p.seen = true
	p.hidden = false
	old := p.bounds
	if id != DockID && (b.W != old.W || b.H != old.H) && sameShape(old, b) {
		// Same shape, new size: Mission Control (or Exposé) is shrinking the
		// window. Keep its snow hidden at the original size until it returns.
		if p.scaledAt == 0 {
			p.scaledAt = s.winClock
		}
		if s.winClock-p.scaledAt < adoptScaled {
			p.flingAt = 0
			p.spans, p.bottom = nil, nil
			return
		}
		// Stayed that size for long: it was a real resize after all.
		p.cols = make([]float64, ncols(b.W))
		p.icicles = s.newIcicles(b.W)
		old = b
	}
	p.scaledAt = 0
	switch {
	case b.W != old.W || b.H != old.H:
		// Resized: snow falls straight off, icicles snap.
		s.shed(p, 0, len(p.cols), Point{}, true)
		s.breakIcicles(p, Point{})
		p.cols = make([]float64, ncols(b.W))
		if id != DockID {
			p.icicles = s.newIcicles(b.W)
		}
	case (b.X != old.X || b.Y != old.Y) && dt > 0 && !calm:
		v := Point{clamp((b.X-old.X)/dt, refSpeed), clamp((b.Y-old.Y)/dt, refSpeed)}
		if math.Hypot(v.X, v.Y) > flingSpeed {
			// Maybe a fling. resolveFling decides once it is clear this is
			// not a desktop slide.
			if p.flingAt == 0 {
				p.flingAt = s.winClock
			}
			p.flingV = v
		}
	}
	p.bounds = b
	p.spans = visibleSpans(b, front, s.W)
	p.bottom = edgeSpans(b.X, b.X+b.W, b.Y+b.H, front, s.W)
	if b.Y+b.H >= s.H {
		p.bottom = nil // bottom edge is off-screen
	}
}

// resolveFling throws a pile off once a fast move has lasted flingDelay
// without turning out to be a desktop slide, which it is if it goes on
// screen-edge-wide or the window leaves the screen.
func (s *Sim) resolveFling(p *pile, calm bool) {
	switch {
	case p.flingAt == 0:
	case calm:
		p.flingAt = 0
	case s.winClock-p.flingAt >= flingDelay:
		v := p.flingV
		// Flung: snow and icicles keep the window's speed.
		s.shed(p, 0, len(p.cols), Point{v.X * 0.8, v.Y*0.8 - 80}, true)
		s.breakIcicles(p, Point{v.X * 0.8, v.Y * 0.8})
		p.flingAt = 0
	}
}

// Sliding reports whether a desktop slide is under way.
func (s *Sim) Sliding() bool { return s.winClock < s.settleUntil }

// slideStarted reports whether this window update looks like macOS sliding
// between desktops rather than the user moving a window: windows switch
// between on and off screen, several windows move by the very same sideways
// step, or a window slides (or appears) partly past the screen edge.
func (s *Sim) slideStarted(ws []Window) bool {
	sameStep := map[float64]int{}
	for _, w := range ws {
		p, ok := s.piles[w.ID]
		if w.Hidden {
			if ok && !p.hidden {
				return true
			}
			continue
		}
		b := w.Bounds
		pastEdge := b.X < 0 || b.X+b.W > s.W
		switch {
		case !ok:
			if pastEdge && math.Min(b.X+b.W, s.W)-math.Max(b.X, 0) < b.W/2 {
				return true // a new window arriving mostly off screen
			}
		case p.hidden:
			return true
		default:
			dx, dy := b.X-p.bounds.X, b.Y-p.bounds.Y
			if dx == 0 || dy != 0 || b.W != p.bounds.W || b.H != p.bounds.H {
				continue
			}
			if pastEdge {
				return true
			}
			if sameStep[dx]++; sameStep[dx] >= 2 {
				return true
			}
		}
	}
	return false
}

// sameShape reports whether a and b have the same aspect ratio (within 2%),
// as when Mission Control scales a window.
func sameShape(a, b Rect) bool {
	if a.H <= 0 || b.H <= 0 {
		return false
	}
	ra, rb := a.W/a.H, b.W/b.H
	return math.Abs(ra-rb) <= 0.02*ra
}

func ncols(w float64) int { return max(1, int(w/colW)) }

func clamp(v, m float64) float64 { return math.Max(-m, math.Min(m, v)) }

// visibleSpans returns the x-ranges of b's top edge, clipped to [0,screenW],
// that no rect in front covers.
func visibleSpans(b Rect, front []Rect, screenW float64) [][2]float64 {
	if b.Y <= 0 {
		return nil // top edge is off-screen
	}
	return edgeSpans(b.X, b.X+b.W, b.Y, front, screenW)
}

// edgeSpans returns the parts of the horizontal edge x0..x1 at height y,
// clipped to [0,screenW], that no rect in front covers.
func edgeSpans(x0, x1, y float64, front []Rect, screenW float64) [][2]float64 {
	spans := [][2]float64{{math.Max(x0, 0), math.Min(x1, screenW)}}
	if spans[0][0] >= spans[0][1] {
		return nil
	}
	for _, f := range front {
		if f.Y > y || f.Y+f.H <= y {
			continue
		}
		spans = subtract(spans, f.X, f.X+f.W)
	}
	return spans
}

func subtract(spans [][2]float64, a, b float64) [][2]float64 {
	var out [][2]float64
	for _, s := range spans {
		if b <= s[0] || a >= s[1] {
			out = append(out, s)
			continue
		}
		if a > s[0] {
			out = append(out, [2]float64{s[0], a})
		}
		if b < s[1] {
			out = append(out, [2]float64{b, s[1]})
		}
	}
	return out
}

func (p *pile) visible(x float64) bool { return inSpans(p.spans, x) }

func (p *pile) colX(i int) float64 { return p.bounds.X + (float64(i)+0.5)*colW }

// colMax is the height limit for column i. It tapers near the corners so
// the pile forms a rounded cap, and rises and dips along the edge so the
// snow settles into drifts instead of an even strip.
func (p *pile) colMax(i int) float64 {
	d := math.Min(float64(i)+0.5, float64(len(p.cols)-i)-0.5) * colW
	return maxPile * p.drift(i) * math.Min(1, math.Sqrt(math.Max(d, 0)/edgeTaper))
}

// drift is a smooth bumpy profile in [driftMin, 1] for column i: wide
// drifts (driftWide columns apart) plus small ripples (driftFine apart).
// It depends only on the window, so a pile keeps its shape as it refills.
// Slopes stay under settle's 1.5 pt step, so settle never flattens them.
func (p *pile) drift(i int) float64 {
	n := 0.8*p.noise(i, driftWide, 0) + 0.2*p.noise(i, driftFine, 1)
	n = math.Max(0, math.Min(1, (n-0.15)/0.7)) // stretch so peaks reach the full cap
	return driftMin + (1-driftMin)*n
}

// noise is value noise in [0,1]: random knots every step columns, eased
// between.
func (p *pile) noise(i, step int, octave uint64) float64 {
	k, t := i/step, float64(i%step)/float64(step)
	t = t * t * (3 - 2*t)
	a, b := hash01(p.seed, octave, k), hash01(p.seed, octave, k+1)
	return a + (b-a)*t
}

// hash01 maps (seed, octave, k) to a stable pseudo-random value in [0,1).
func hash01(seed, octave uint64, k int) float64 {
	x := seed*0x9E3779B97F4A7C15 ^ octave*0xBF58476D1CE4E5B9 ^ uint64(k)*0x94D049BB133111EB
	x ^= x >> 30
	x *= 0xBF58476D1CE4E5B9
	x ^= x >> 27
	x *= 0x94D049BB133111EB
	x ^= x >> 31
	return float64(x>>11) / (1 << 53)
}

// shed turns columns [from,to) into flakes with base velocity v. With
// onlyVisible, snow in hidden columns is dropped without flakes.
func (s *Sim) shed(p *pile, from, to int, v Point, onlyVisible bool) {
	for i := from; i < to; i++ {
		h := p.cols[i]
		p.cols[i] = 0
		if h < 0.3 {
			continue
		}
		x := p.colX(i)
		if onlyVisible && !p.visible(x) {
			continue
		}
		for n := 1 + int(h/3); n > 0 && len(s.flakes) < hardCap; n-- {
			s.flakes = append(s.flakes, flake{
				x:     x + (s.rng.Float64()-0.5)*colW,
				y:     p.bounds.Y - s.rng.Float64()*h,
				vx:    v.X + (s.rng.Float64()-0.5)*40,
				vy:    v.Y + (s.rng.Float64()-0.5)*30,
				r:     0.8 + s.rng.Float64()*1.2,
				phase: s.rng.Float64() * 2 * math.Pi,
			})
		}
	}
}

// Step advances the simulation by dt seconds. cursor is the mouse position.
func (s *Sim) Step(dt float64, cursor Point) {
	s.t += dt
	s.spawn(dt)
	var cv Point // cursor velocity
	if dt > 0 {
		cv = Point{clamp((cursor.X-s.lastCur.X)/dt, refSpeed), clamp((cursor.Y-s.lastCur.Y)/dt, refSpeed)}
	}
	s.brush(cursor, cv)
	s.stepWind(dt)
	s.stepSparkles(dt, s.lastCur, cursor)
	s.stepFrost(dt, cursor)
	s.growIcicles(dt)
	k := math.Exp(-drag * dt)
	live := s.flakes[:0]
	for _, f := range s.flakes {
		// The cursor pushes nearby flakes away and drags them along.
		if dx, dy := f.x-cursor.X, f.y-cursor.Y; dx*dx+dy*dy < pushR*pushR {
			d := math.Max(math.Hypot(dx, dy), 1)
			w := 1 - d/pushR
			f.vx += (dx/d*pushForce + cv.X*3) * w * dt
			f.vy += (dy/d*pushForce + cv.Y*3) * w * dt
		}
		py := f.y
		f.vx *= k
		f.vy = f.vy*k + gravity*dt
		f.x += (f.vx + s.wind + math.Sin(s.t*1.3+f.phase)*12) * dt
		f.y += f.vy * dt
		if s.cfg.Piles && f.vy > 0 && s.land(f.x, py, f.y) {
			continue
		}
		if f.y > s.H+5 || f.x < -50 || f.x > s.W+50 {
			continue
		}
		live = append(live, f)
	}
	s.flakes = live
	s.lastCur = cursor
}

func (s *Sim) spawn(dt float64) {
	s.spawnAcc += spawnRate * s.cfg.Amount * dt
	limit := int(flakeCap * s.cfg.Amount)
	for ; s.spawnAcc >= 1; s.spawnAcc-- {
		if len(s.flakes) >= limit {
			continue
		}
		s.flakes = append(s.flakes, flake{
			x:     s.rng.Float64() * s.W,
			y:     -5,
			vy:    gravity / drag * (0.6 + s.rng.Float64()*0.5),
			r:     0.8 + s.rng.Float64()*1.6,
			phase: s.rng.Float64() * 2 * math.Pi,
		})
	}
}

// land reports whether a flake moving from y0 to y1 at x settled on a
// visible pile surface it crossed, and grows that pile. Flakes are drawn
// above every window, so a ledge of a window behind must not shield the
// windows below it: each crossed ledge catches the flake only by chance.
func (s *Sim) land(x, y0, y1 float64) bool {
	var best *pile
	bestI := 0
	for _, p := range s.piles {
		i := int(math.Floor((x - p.bounds.X) / colW))
		if i < 0 || i >= len(p.cols) || !p.visible(x) {
			continue
		}
		surf := p.bounds.Y - p.cols[i]
		if y0 < surf && y1 >= surf && s.rng.Float64() < catchChance {
			best, bestI = p, i
			break
		}
	}
	if best == nil {
		return false
	}
	add := func(i int, h float64) {
		if i >= 0 && i < len(best.cols) {
			best.cols[i] = math.Min(best.cols[i]+h, best.colMax(i))
		}
	}
	add(bestI, perFlake*0.5)
	add(bestI-1, perFlake*0.25)
	add(bestI+1, perFlake*0.25)
	best.settle(bestI)
	return true
}

// settle lets snow slide off steep steps around column i.
func (p *pile) settle(i int) {
	for j := max(i-3, 0); j < min(i+3, len(p.cols)-1); j++ {
		d := p.cols[j] - p.cols[j+1]
		if math.Abs(d) > 1.5 {
			m := (math.Abs(d) - 1.5) / 2 * math.Copysign(1, d)
			p.cols[j] = math.Min(p.cols[j]-m, p.colMax(j))
			p.cols[j+1] = math.Min(p.cols[j+1]+m, p.colMax(j+1))
		}
	}
}

// brush knocks snow off columns near the cursor, pushing it away.
// cv is the cursor velocity.
func (s *Sim) brush(c, cv Point) {
	for _, p := range s.piles {
		top := p.bounds.Y
		if c.Y < top-maxPile-brushR || c.Y > top+brushR {
			continue
		}
		lo := max(int((c.X-brushR-p.bounds.X)/colW), 0)
		hi := min(int((c.X+brushR-p.bounds.X)/colW)+1, len(p.cols))
		for i := lo; i < hi; i++ {
			x := p.colX(i)
			if p.cols[i] < 0.3 || !p.visible(x) || math.Hypot(x-c.X, top-p.cols[i]/2-c.Y) > brushR {
				continue
			}
			dir := math.Copysign(1, x-c.X)
			v := Point{
				dir*(60+s.rng.Float64()*120) + cv.X*0.5,
				-(50 + s.rng.Float64()*110) + math.Min(cv.Y*0.5, 0),
			}
			s.shed(p, i, i+1, v, true)
		}
	}
}
