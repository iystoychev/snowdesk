package snow

import (
	"math"
	"testing"
)

func extrasSim(stars, flies, leaves bool) *Sim {
	s := New(1000, 800, 3)
	c := DefaultSettings()
	c.Amount = 0 // no snow, so Fill output is only the extras
	c.Frost, c.Sparkles, c.Wind = false, false, false
	c.Stars, c.Fireflies, c.Leaves = stars, flies, leaves
	s.Configure(c)
	return s
}

func run(s *Sim, secs float64, cursor Point) {
	for range int(secs * 60) {
		s.Step(1.0/60, cursor)
	}
}

var noCursor = Point{-1000, -1000}

func TestExtrasOffByDefault(t *testing.T) {
	s := New(1000, 800, 3)
	run(s, 20, noCursor)
	if len(s.stars)+len(s.flies)+len(s.leaves) != 0 {
		t.Errorf("extras appeared with default settings: %d stars, %d flies, %d leaves",
			len(s.stars), len(s.flies), len(s.leaves))
	}
}

func TestShootingStarsComeAndGo(t *testing.T) {
	s := extrasSim(true, false, false)
	seen := 0
	for range 30 * 60 {
		before := len(s.stars)
		s.Step(1.0/60, noCursor)
		if len(s.stars) > before {
			seen++
		}
		if len(s.stars) > 2 {
			t.Fatalf("%d stars at once; they should be rare", len(s.stars))
		}
	}
	if seen < 3 || seen > 11 {
		t.Errorf("%d shooting stars in 30 s, want about 3-10", seen)
	}
	c := s.cfg
	c.Stars = false
	s.Configure(c)
	run(s, 2, noCursor)
	if len(s.stars) != 0 {
		t.Error("stars keep coming after turning them off")
	}
}

func TestFirefliesStayOnScreenAndFlee(t *testing.T) {
	s := extrasSim(false, true, false)
	run(s, 60, noCursor)
	if len(s.flies) != fireflyCount {
		t.Fatalf("%d fireflies, want %d", len(s.flies), fireflyCount)
	}
	for _, ff := range s.flies {
		if ff.x < -20 || ff.x > s.W+20 || ff.y < -20 || ff.y > s.H+20 {
			t.Fatalf("firefly left the screen: %+v", ff)
		}
	}
	// Put the cursor on a firefly: it should get away.
	cur := Point{s.flies[0].x, s.flies[0].y}
	run(s, 1, cur)
	if d := math.Hypot(s.flies[0].x-cur.X, s.flies[0].y-cur.Y); d < 40 {
		t.Errorf("firefly still %.0f pt from the cursor after 1 s", d)
	}
	c := s.cfg
	c.Fireflies = false
	s.Configure(c)
	run(s, 0.1, noCursor)
	if len(s.flies) != 0 {
		t.Error("fireflies stay after turning them off")
	}
}

func TestLeavesFallAndDraw(t *testing.T) {
	s := extrasSim(false, false, true)
	run(s, 10, noCursor)
	if n := len(s.leaves); n == 0 || n > leafCap {
		t.Fatalf("%d leaves after 10 s", n)
	}
	for _, l := range s.leaves {
		if l.vy <= 0 || l.vy > leafFall*1.5 {
			t.Fatalf("leaf falls at %.0f pt/s, want about %v", l.vy, leafFall)
		}
	}
	dots, tris := make([]float32, 1<<12), make([]float32, 1<<16)
	if _, nt := s.Fill(dots, tris); nt == 0 {
		t.Error("leaves drew nothing")
	}
	c := s.cfg
	c.Leaves = false
	s.Configure(c)
	run(s, 40, noCursor)
	if len(s.leaves) != 0 {
		t.Errorf("%d leaves still falling 40 s after turning them off", len(s.leaves))
	}
}
