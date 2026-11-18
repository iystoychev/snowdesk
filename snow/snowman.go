package snow

import "math"

// Snowballs and snowmen. Hold Option and sweep the cursor through a drift:
// a ball forms and grows as it rolls through the snow. Let go of Option and
// it drops onto whatever is below: a ledge, the snowbank or another ball.
// Three balls stacked make a snowman with a hat, scarf and carrot nose.
// Hold Option over the top ball of a stack to pick it up again.

const (
	ballStartR   = 3.5
	ballMaxR     = 24.0
	ballCap      = 30
	ballGravity  = 650.0
	rollTake     = 30.0 // pile height per second a rolling ball picks up from each column
	rollGrow     = 1.1  // ball area gained per unit of pile height taken
	startReach   = 16.0 // Option over a drift this close starts a ball
	stackOverlap = 0.82 // stacked balls sink this far into each other
)

type ballState int

const (
	ballCarried ballState = iota
	ballFalling
	ballResting
)

type ball struct {
	x, y, r, vx, vy float64
	state           ballState
	hidden          bool
	pile            uint32  // ledge it rests on, when resting and below is nil
	offX            float64 // x relative to that ledge's left edge
	lastPX, lastPY  float64 // ledge position last step, to notice flings
	below           *ball   // ball it is stacked on
}

func (b *ball) area() float64 { return math.Pi * b.r * b.r }

func (b *ball) grow(a float64) {
	b.r = math.Min(ballMaxR, math.Sqrt((b.area()+a)/math.Pi))
}

// stepBalls rolls, carries, drops and stacks snowballs. Option is the
// state of the Option key; cv is the cursor velocity.
func (s *Sim) stepBalls(dt float64, c, cv Point) {
	if s.Option && s.carried == nil {
		s.pickUpBall(c)
	}
	if !s.Option && s.carried != nil {
		b := s.carried
		b.state = ballFalling
		b.vx, b.vy = clamp(cv.X*0.25, 300), 0
		s.carried = nil
	}
	if b := s.carried; b != nil {
		s.rollBall(b, c, dt)
	}
	// Bottom balls first, so a stacked ball follows the one below it.
	for depth := 0; depth < 4; depth++ {
		for _, b := range s.balls {
			if b.state != ballCarried && b.depth() == depth {
				s.stepBall(b, dt)
			}
		}
	}
	live := s.balls[:0]
	for _, b := range s.balls {
		if b.y-b.r < s.H+40 && b.x > -60 && b.x < s.W+60 {
			live = append(live, b)
		} else {
			s.unstack(b)
		}
	}
	s.balls = live
}

func (b *ball) depth() int {
	d := 0
	for p := b.below; p != nil && d < 8; p = p.below {
		d++
	}
	return d
}

// unstack drops every ball that rested on b.
func (s *Sim) unstack(b *ball) {
	for _, o := range s.balls {
		if o.below == b {
			o.below = nil
			o.state = ballFalling
		}
	}
}

func (s *Sim) onTop(b *ball) bool {
	for _, o := range s.balls {
		if o.below == b {
			return false
		}
	}
	return true
}

// pickUpBall starts carrying the top ball under the cursor, or starts a
// new ball when the cursor is in a drift.
func (s *Sim) pickUpBall(c Point) {
	for _, b := range s.balls {
		if !b.hidden && b.state == ballResting && s.onTop(b) && math.Hypot(c.X-b.x, c.Y-b.y) < b.r+4 {
			b.below = nil
			b.state = ballCarried
			s.carried = b
			return
		}
	}
	if len(s.balls) >= ballCap {
		return
	}
	if _, i, surf := s.driftUnder(c.X, c.Y, startReach); i >= 0 && surf > 0 {
		b := &ball{x: c.X, y: c.Y, r: ballStartR, state: ballCarried}
		s.balls = append(s.balls, b)
		s.carried = b
	}
}

// driftUnder finds a visible pile column under x whose snow surface is
// within reach of y and holds some snow. It returns the pile, the column
// and the surface y, or column -1.
func (s *Sim) driftUnder(x, y, reach float64) (*pile, int, float64) {
	for _, p := range s.piles {
		if p.hidden || !p.visible(x) {
			continue
		}
		i := int(math.Floor((x - p.bounds.X) / colW))
		if i < 0 || i >= len(p.cols) || p.cols[i] < 1 {
			continue
		}
		surf := p.bounds.Y - p.cols[i]
		if y > surf-reach && y < p.bounds.Y+reach {
			return p, i, surf
		}
	}
	return nil, -1, 0
}

// rollBall moves a carried ball with the cursor and lets it gather the
// snow it rolls over.
func (s *Sim) rollBall(b *ball, c Point, dt float64) {
	b.x, b.y = c.X, c.Y
	p, _, _ := s.driftUnder(c.X, c.Y, b.r+10)
	if p == nil {
		return
	}
	// Rest the ball on the snow it rolls through and take some from each
	// column it touches.
	lo := max(int((b.x-b.r-p.bounds.X)/colW), 0)
	hi := min(int((b.x+b.r-p.bounds.X)/colW)+1, len(p.cols))
	got := 0.0
	for i := lo; i < hi; i++ {
		if !p.visible(p.colX(i)) {
			continue
		}
		take := math.Min(p.cols[i], rollTake*dt)
		p.cols[i] -= take
		got += take
	}
	b.grow(got * colW * rollGrow)
	b.y = p.bounds.Y - b.r*0.9
}

func (s *Sim) stepBall(b *ball, dt float64) {
	switch b.state {
	case ballResting:
		if b.below != nil {
			lb := b.below
			if lb.state != ballResting {
				b.below, b.state = nil, ballFalling
				b.vx, b.vy = lb.vx, lb.vy
				return
			}
			b.hidden = lb.hidden
			b.x = lb.x
			b.y = lb.y - (lb.r+b.r)*stackOverlap
			return
		}
		p, ok := s.piles[b.pile]
		if !ok {
			b.state, b.vx, b.vy = ballFalling, 0, 0
			return
		}
		b.hidden = p.hidden
		if p.hidden {
			return
		}
		if dt > 0 && !s.Sliding() {
			vx, vy := (p.bounds.X-b.lastPX)/dt, (p.bounds.Y-b.lastPY)/dt
			if math.Hypot(vx, vy) > flingSpeed*1.2 {
				b.state = ballFalling
				b.vx, b.vy = clamp(vx*0.8, refSpeed), clamp(vy*0.8-80, refSpeed)
				return
			}
		}
		b.lastPX, b.lastPY = p.bounds.X, p.bounds.Y
		b.x = p.bounds.X + b.offX
		if b.x < p.bounds.X || b.x > p.bounds.X+p.bounds.W {
			b.state, b.vx, b.vy = ballFalling, 0, 0
			return
		}
		b.y = s.surface(p, b.x) - b.r*0.88
	case ballFalling:
		b.hidden = false
		prev := b.y + b.r
		b.vy += ballGravity * dt
		b.vx *= math.Exp(-1.5 * dt)
		b.x += b.vx * dt
		b.y += b.vy * dt
		s.landBall(b, prev)
	}
}

// surface is the y of the snow top on p at x.
func (s *Sim) surface(p *pile, x float64) float64 {
	i := int(math.Floor((x - p.bounds.X) / colW))
	if i < 0 || i >= len(p.cols) {
		return p.bounds.Y
	}
	return p.bounds.Y - p.cols[i]
}

// landBall settles a falling ball whose bottom moved from prev to its
// current bottom on a ball or a ledge it crossed.
func (s *Sim) landBall(b *ball, prev float64) {
	bottom := b.y + b.r
	for _, o := range s.balls {
		if o == b || o.state != ballResting || o.hidden || !s.onTop(o) {
			continue
		}
		top := o.y - o.r*0.7
		if math.Abs(b.x-o.x) < o.r*0.95 && prev <= top && bottom >= top {
			b.state, b.below = ballResting, o
			b.vx, b.vy = 0, 0
			b.x, b.y = o.x, o.y-(o.r+b.r)*stackOverlap
			return
		}
	}
	for id, p := range s.piles {
		if p.hidden || !p.visible(b.x) || b.x < p.bounds.X || b.x > p.bounds.X+p.bounds.W {
			continue
		}
		surf := s.surface(p, b.x)
		if prev <= surf+2 && bottom >= surf {
			b.state, b.pile, b.below = ballResting, id, nil
			b.offX = b.x - p.bounds.X
			b.lastPX, b.lastPY = p.bounds.X, p.bounds.Y
			b.vx, b.vy = 0, 0
			b.y = surf - b.r*0.88
			return
		}
	}
}

// --- drawing ---------------------------------------------------------------

func (s *Sim) drawBalls(f *frame) {
	for depth := 0; depth < 4; depth++ {
		for _, b := range s.balls {
			if !b.hidden && b.depth() == depth {
				drawBall(f, b)
			}
		}
	}
	for _, b := range s.balls {
		if !b.hidden && b.state == ballResting && b.depth() == 2 && s.onTop(b) {
			drawSnowman(f, b, b.below, b.below.below)
		}
	}
}

func drawBall(f *frame, b *ball) {
	ellipse(f, b.x, b.y, b.r, b.r, 0, 2*math.Pi, rgba{0.95, 0.97, 1, 1}, rgba{0.78, 0.84, 0.93, 1})
	ellipse(f, b.x-b.r*0.28, b.y-b.r*0.3, b.r*0.55, b.r*0.5, 0, 2*math.Pi, rgba{1, 1, 1, 0.9}, rgba{1, 1, 1, 0})
}

// drawSnowman dresses a stack of three: head on top of mid on top of base.
func drawSnowman(f *frame, head, mid, base *ball) {
	coal := rgba{0.1, 0.1, 0.12, 1}
	// Stick arms out of the middle ball.
	stick := rgba{0.40, 0.26, 0.14, 1}
	for _, d := range []float64{-1, 1} {
		x0, y0 := mid.x+d*mid.r*0.8, mid.y-mid.r*0.2
		x1, y1 := mid.x+d*mid.r*1.9, mid.y-mid.r*0.9
		f.quad(x0, y0-1, x1, y1-0.8, x1, y1+0.8, x0, y0+1, stick)
		tx, ty := x0+(x1-x0)*0.7, y0+(y1-y0)*0.7
		f.quad(tx, ty-0.6, tx+d*mid.r*0.25, ty-mid.r*0.45, tx+d*mid.r*0.25+0.8, ty-mid.r*0.45, tx+0.8, ty+0.6, stick)
	}
	// Buttons.
	for k := range 3 {
		f.dot(mid.x, mid.y-mid.r*0.45+float64(k)*mid.r*0.42, math.Max(1.2, mid.r*0.1), 0.8, coal)
	}
	// Scarf around the neck, with a tail.
	red, deep := rgba{0.85, 0.12, 0.15, 1}, rgba{0.62, 0.06, 0.1, 1}
	ny := head.y + head.r*0.82
	sw := head.r * 1.05
	f.quad(head.x-sw, ny-2.2, head.x+sw, ny-2.2, head.x+sw*0.95, ny+2.2, head.x-sw*0.95, ny+2.2, red)
	f.quad(head.x+sw*0.35, ny, head.x+sw*0.75, ny, head.x+sw*0.85, ny+head.r*1.1, head.x+sw*0.45, ny+head.r*1.1, deep)
	// Face: coal eyes and smile, carrot nose.
	er := math.Max(1, head.r*0.1)
	f.dot(head.x-head.r*0.32, head.y-head.r*0.22, er, 0.8, coal)
	f.dot(head.x+head.r*0.32, head.y-head.r*0.22, er, 0.8, coal)
	for k := range 5 {
		a := math.Pi*0.25 + math.Pi*0.5*float64(k)/4
		f.dot(head.x+math.Cos(a)*head.r*0.5, head.y+math.Sin(a)*head.r*0.45, er*0.7, 0.8, coal)
	}
	orange := rgba{1, 0.5, 0.1, 1}
	f.tri(head.x, head.y-head.r*0.1, orange, head.x, head.y+head.r*0.12, shade(orange, 0.8), head.x+head.r*0.8, head.y+head.r*0.05, orange)
	// Top hat with a red band.
	hatW, hatH := head.r*0.7, head.r*1.0
	by := head.y - head.r*0.72
	black := rgba{0.08, 0.08, 0.1, 1}
	f.quad(head.x-head.r*1.0, by-head.r*0.16, head.x+head.r*1.0, by-head.r*0.16, head.x+head.r*1.0, by, head.x-head.r*1.0, by, black)
	f.quad(head.x-hatW, by-hatH, head.x+hatW, by-hatH, head.x+hatW, by-head.r*0.1, head.x-hatW, by-head.r*0.1, black)
	f.quad(head.x-hatW, by-head.r*0.42, head.x+hatW, by-head.r*0.42, head.x+hatW, by-head.r*0.22, head.x-hatW, by-head.r*0.22, red)
	_ = base
}
