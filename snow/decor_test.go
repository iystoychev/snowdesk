package snow

import (
	"testing"
	"time"
)

// only returns settings with every decoration off except what f turns on.
func only(f func(*Settings)) Settings {
	c := Settings{Amount: 1, Size: 1, Piles: true}
	f(&c)
	return c
}

func TestWindBlowsFlakesSideways(t *testing.T) {
	s := New(1000, 800, 1)
	s.Configure(only(func(c *Settings) { c.Wind = true }))
	s.windNext = 1e9
	s.windTarget = 100
	for range 600 { // 10 s: wind eases up to the gust speed
		s.stepWind(1.0 / 60)
	}
	if s.wind < 95 {
		t.Fatalf("wind %v did not reach gust 100", s.wind)
	}
	s.flakes = []flake{{x: 500, y: 100, vy: 60}}
	s.step(1.0, Point{-1000, -1000})
	if s.flakes[0].x < 560 {
		t.Errorf("flake x %v: not blown right", s.flakes[0].x)
	}
	s.Configure(only(func(*Settings) {}))
	for range 600 {
		s.stepWind(1.0 / 60)
	}
	if s.wind > 5 {
		t.Errorf("wind %v did not calm down when off", s.wind)
	}
}

func icicleLen(p *pile) (sum float64) {
	for _, ic := range p.icicles {
		sum += ic.len
	}
	return sum
}

func TestIciclesGrowAndSnapOff(t *testing.T) {
	b := Rect{100, 200, 400, 300}
	tests := []struct {
		name         string
		next         Rect
		minVX, maxVX float64 // bounds on average shard vx
	}{
		{"resize snaps them straight down", Rect{100, 200, 420, 300}, -30, 30},
		{"fling throws them sideways", Rect{130, 200, 400, 300}, 400, 600},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(1000, 800, 1)
			s.Configure(only(func(c *Settings) { c.Icicles = true }))
			s.SetWindows([]Window{{ID: 1, Bounds: b}}, Rect{}, 0)
			p := s.piles[1]
			if len(p.icicles) == 0 {
				t.Fatal("window got no icicles")
			}
			for range 100 {
				s.growIcicles(1)
			}
			for _, ic := range p.icicles {
				if ic.len != ic.max {
					t.Fatalf("icicle %v not full grown after 100s", ic)
				}
			}
			s.flakes = nil
			for range 6 { // first update moves it; then it rests until a fling is decided
				s.SetWindows([]Window{{ID: 1, Bounds: tt.next}}, Rect{}, 1.0/20)
			}
			if icicleLen(s.piles[1]) != 0 || len(s.flakes) == 0 {
				t.Fatalf("icicles not snapped: len %v, shards %d", icicleLen(s.piles[1]), len(s.flakes))
			}
			if v := avgVX(s); v < tt.minVX || v > tt.maxVX {
				t.Errorf("avg shard vx %v not in [%v, %v]", v, tt.minVX, tt.maxVX)
			}
		})
	}
}

func TestIciclesHiddenUnderFrontWindow(t *testing.T) {
	s := New(1000, 800, 1)
	s.Configure(only(func(c *Settings) { c.Icicles = true }))
	back := Window{ID: 1, Bounds: Rect{100, 100, 400, 300}}
	front := Window{ID: 2, Bounds: Rect{50, 350, 600, 300}} // covers back's whole bottom edge
	s.SetWindows([]Window{front, back}, Rect{}, 0)
	for range 100 {
		s.growIcicles(1)
	}
	s.piles[2].icicles = nil
	if _, nt := s.Fill(nil, make([]float32, 100000)); nt != 0 {
		t.Errorf("drew %d floats of hidden icicles", nt)
	}
}

func TestLightsFollowVisibleTopEdge(t *testing.T) {
	s := New(1000, 800, 1)
	s.Configure(only(func(c *Settings) { c.Lights = true }))
	s.SetWindows([]Window{{ID: 1, Bounds: Rect{100, 200, 500, 300}}}, Rect{}, 0)
	nd, nt := s.Fill(make([]float32, 10000), make([]float32, 100000))
	if nd == 0 || nt == 0 {
		t.Fatalf("no bulbs (%d) or wire (%d)", nd, nt)
	}
	// A window in front covering the whole top edge hides every light.
	s.SetWindows([]Window{{ID: 2, Bounds: Rect{0, 150, 800, 100}}, {ID: 1, Bounds: Rect{100, 200, 500, 300}}}, Rect{}, 0)
	dots := make([]float32, 10000)
	nd, _ = s.Fill(dots, make([]float32, 100000))
	for i := 0; i < nd; i += DotFloats {
		if x := dots[i]; x >= 100 && x <= 600 && dots[i+1] > 195 {
			t.Fatalf("bulb at x=%v drawn under the front window", x)
		}
	}
	s.Configure(only(func(*Settings) {}))
	if nd, _ := s.Fill(make([]float32, 10000), make([]float32, 100000)); nd != 0 {
		t.Errorf("lights drawn while off: %d floats", nd)
	}
}

func TestSparkleTrail(t *testing.T) {
	s := New(1000, 800, 1)
	s.Configure(only(func(c *Settings) { c.Sparkles = true }))
	s.stepSparkles(1.0/60, Point{100, 100}, Point{200, 100})
	if len(s.sparks) < 10 {
		t.Fatalf("only %d sparkles for a 100pt stroke", len(s.sparks))
	}
	for range 60 {
		s.stepSparkles(1.0/60, Point{200, 100}, Point{200, 100})
	}
	if len(s.sparks) != 0 {
		t.Errorf("%d sparkles left after 1s", len(s.sparks))
	}
	s.stepSparkles(1.0/60, Point{0, 0}, Point{900, 700}) // a jump, not a stroke
	if len(s.sparks) != 0 {
		t.Errorf("jump made %d sparkles", len(s.sparks))
	}
	s.Configure(only(func(*Settings) {}))
	s.stepSparkles(1.0/60, Point{100, 100}, Point{200, 100})
	if len(s.sparks) != 0 {
		t.Errorf("sparkles while off: %d", len(s.sparks))
	}
}

func TestFrostGrowsAndMeltsNearCursor(t *testing.T) {
	s := New(1000, 800, 1)
	s.Configure(only(func(c *Settings) { c.Frost = true }))
	far := Point{500, 400}
	for range 160 {
		s.stepFrost(1, far)
	}
	for i, lv := range s.frost {
		if lv != 1 {
			t.Fatalf("corner %d frost %v, want 1", i, lv)
		}
	}
	for range 60 { // cursor sits in the top-left corner for 1 s
		s.stepFrost(1.0/60, Point{20, 20})
	}
	if s.frost[0] != 0 || s.frost[2] != 1 {
		t.Errorf("frost top-left %v (want melted), bottom-right %v (want 1)", s.frost[0], s.frost[2])
	}
	if _, nt := s.Fill(nil, make([]float32, 100000)); nt == 0 {
		t.Error("frost not drawn")
	}
}

func TestCountdown(t *testing.T) {
	sofia, err := time.LoadLocation("Europe/Sofia")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		now   time.Time
		days  int
		title string
	}{
		{time.Date(2026, 11, 5, 9, 0, 0, 0, sofia), 50, "❄︎ 50d"},
		{time.Date(2026, 3, 1, 23, 59, 0, 0, sofia), 299, "❄︎ 299d"}, // across the DST change
		{time.Date(2026, 12, 24, 23, 0, 0, 0, time.UTC), 1, "❄︎ 1d"},
		{time.Date(2026, 12, 25, 12, 0, 0, 0, time.UTC), 0, "❄︎ 🎄"},
		{time.Date(2026, 12, 26, 0, 0, 0, 0, time.UTC), 364, "❄︎ 364d"},
	}
	for _, tt := range tests {
		if d, title := DaysToChristmas(tt.now), CountdownTitle(tt.now); d != tt.days || title != tt.title {
			t.Errorf("%v: got %d %q, want %d %q", tt.now, d, title, tt.days, tt.title)
		}
	}
}
