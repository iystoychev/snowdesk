package snow

import (
	"math"
	"reflect"
	"testing"
)

func TestVisibleSpans(t *testing.T) {
	back := Rect{100, 200, 400, 300}
	tests := []struct {
		name  string
		front []Rect
		want  [][2]float64
	}{
		{"uncovered", nil, [][2]float64{{100, 500}}},
		{"middle covered", []Rect{{200, 150, 100, 100}}, [][2]float64{{100, 200}, {300, 500}}},
		{"front window below edge", []Rect{{200, 250, 100, 100}}, [][2]float64{{100, 500}}},
		{"front window ends above edge", []Rect{{200, 50, 100, 100}}, [][2]float64{{100, 500}}},
		{"fully covered", []Rect{{0, 100, 800, 200}}, nil},
		{"same top edge", []Rect{{50, 200, 100, 50}}, [][2]float64{{150, 500}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := visibleSpans(back, tt.front, 1000); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
	if got := visibleSpans(Rect{900, 100, 300, 100}, nil, 1000); !reflect.DeepEqual(got, [][2]float64{{900, 1000}}) {
		t.Errorf("screen clip: got %v", got)
	}
}

// step is Step without spawning new flakes.
func (s *Sim) step(dt float64, c Point) {
	s.spawnAcc = -1e9
	s.Step(dt, c)
}

// dropFlake drops 10 flakes straight down at x and runs until all land or leave.
func dropFlake(s *Sim, x float64) {
	for range 10 {
		s.flakes = append(s.flakes, flake{x: x, y: 0, vy: 60})
	}
	for range 600 {
		s.t = 0 // sin(phase 0) = 0: no drift, flakes fall straight
		s.wind, s.windTarget, s.windNext = 0, 0, 1e9
		s.step(1.0/60, Point{-1000, -1000})
		if len(s.flakes) == 0 {
			return
		}
	}
}

func TestBackLedgeDoesNotShieldFrontWindow(t *testing.T) {
	s := New(1000, 800, 1)
	front := Window{ID: 2, Bounds: Rect{100, 300, 300, 200}}
	back := Window{ID: 1, Bounds: Rect{50, 100, 400, 300}} // its edge is above front's, and visible
	s.SetWindows([]Window{front, back}, Rect{}, 0)
	for range 20 {
		dropFlake(s, 200)
	}
	if total(s.piles[1]) == 0 || total(s.piles[2]) == 0 {
		t.Errorf("both ledges should get snow: back %v, front %v", total(s.piles[1]), total(s.piles[2]))
	}
}

func total(p *pile) (h float64) {
	for _, c := range p.cols {
		h += c
	}
	return h
}

func TestAccumulatesOnVisibleLedgeOnly(t *testing.T) {
	s := New(1000, 800, 1)
	back := Window{ID: 1, Bounds: Rect{100, 300, 400, 300}}
	front := Window{ID: 2, Bounds: Rect{250, 200, 100, 300}} // covers back's edge at x 250..350
	s.SetWindows([]Window{front, back}, Rect{}, 0)

	dropFlake(s, 150)
	if total(s.piles[1]) == 0 {
		t.Fatal("flake on visible part of back edge did not pile up")
	}
	before := total(s.piles[1])
	dropFlake(s, 300)
	if total(s.piles[1]) != before {
		t.Error("flake landed on the hidden part of back edge")
	}
	if total(s.piles[2]) == 0 {
		t.Error("flake did not land on front window")
	}
}

func TestPileHeightIsCapped(t *testing.T) {
	s := New(1000, 800, 1)
	s.SetWindows([]Window{{ID: 1, Bounds: Rect{100, 300, 400, 300}}}, Rect{}, 0)
	for range 200 {
		dropFlake(s, 300)
	}
	for i, h := range s.piles[1].cols {
		if h > maxPile+1e-9 {
			t.Fatalf("col %d height %v > %v", i, h, maxPile)
		}
	}
}

func filled(id uint32, b Rect, s *Sim) *pile {
	s.SetWindows([]Window{{ID: id, Bounds: b}}, Rect{}, 0)
	p := s.piles[id]
	for i := range p.cols {
		p.cols[i] = 5
	}
	s.flakes = nil
	return p
}

func avgVX(s *Sim) float64 {
	var v float64
	for _, f := range s.flakes {
		v += f.vx
	}
	return v / float64(len(s.flakes))
}

func TestShedding(t *testing.T) {
	b := Rect{100, 300, 300, 200}
	tests := []struct {
		name     string
		next     Rect
		wantShed bool
		vxMin    float64 // bounds on average flake vx
		vxMax    float64
	}{
		{"resize drops straight", Rect{100, 300, 320, 200}, true, -30, 30},
		{"fling throws sideways", Rect{130, 300, 300, 200}, true, 400, 600}, // 30pt in 1/20s = 600 pt/s, ×0.8
		{"slow move keeps pile", Rect{102, 300, 300, 200}, false, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(1000, 800, 1)
			filled(1, b, s)
			s.SetWindows([]Window{{ID: 1, Bounds: tt.next}}, Rect{}, 1.0/20)
			for range 5 { // the window then rests; a fling is decided after flingDelay
				s.SetWindows([]Window{{ID: 1, Bounds: tt.next}}, Rect{}, 1.0/20)
			}
			if !tt.wantShed {
				if len(s.flakes) != 0 || total(s.piles[1]) == 0 {
					t.Fatalf("pile shed on slow move: %d flakes", len(s.flakes))
				}
				return
			}
			if len(s.flakes) == 0 || total(s.piles[1]) != 0 {
				t.Fatalf("pile not shed: %d flakes, pile %v", len(s.flakes), total(s.piles[1]))
			}
			if v := avgVX(s); v < tt.vxMin || v > tt.vxMax {
				t.Errorf("avg vx %v not in [%v, %v]", v, tt.vxMin, tt.vxMax)
			}
		})
	}
}

func TestHiddenWindowKeepsPile(t *testing.T) {
	s := New(1000, 800, 1)
	b := Rect{100, 300, 300, 200}
	p := filled(1, b, s)
	before := total(p)
	// Switch Space: the window is gone from the on-screen list but still exists.
	s.SetWindows([]Window{{ID: 1, Bounds: b, Hidden: true}}, Rect{}, 1.0/20)
	if len(s.flakes) != 0 || total(p) != before || p.spans != nil {
		t.Fatalf("hidden window lost or showed its pile: flakes %d, pile %v, spans %v", len(s.flakes), total(p), p.spans)
	}
	// Switch back.
	s.SetWindows([]Window{{ID: 1, Bounds: b}}, Rect{}, 1.0/20)
	if s.piles[1] != p || total(p) != before || len(p.spans) == 0 {
		t.Fatalf("pile not restored: pile %v, spans %v", total(p), p.spans)
	}
}

func TestClosedWindowDropsPile(t *testing.T) {
	s := New(1000, 800, 1)
	filled(1, Rect{100, 300, 300, 200}, s)
	s.SetWindows(nil, Rect{}, 1.0/20)
	if len(s.piles) != 0 || len(s.flakes) == 0 {
		t.Fatalf("piles %d flakes %d", len(s.piles), len(s.flakes))
	}
}

func TestCursorBrushesSnowOff(t *testing.T) {
	s := New(1000, 800, 1)
	p := filled(1, Rect{100, 300, 300, 200}, s)
	s.lastCur = Point{250, 297}
	s.step(1.0/60, Point{250, 297})
	for i, h := range p.cols {
		x := p.colX(i)
		if math.Abs(x-250) < brushR-3 && h != 0 {
			t.Errorf("col at x=%v under cursor still has %v", x, h)
		}
		if math.Abs(x-250) > brushR+3 && h == 0 {
			t.Errorf("col at x=%v far from cursor was cleared", x)
		}
	}
	if len(s.flakes) == 0 {
		t.Error("brushed snow made no flakes")
	}
}

func TestCursorPushesFlakes(t *testing.T) {
	s := New(1000, 800, 1)
	s.flakes = []flake{{x: 520, y: 400}, {x: 480, y: 400}, {x: 800, y: 400}}
	s.lastCur = Point{500, 400}
	s.step(1.0/60, Point{500, 400})
	if s.flakes[0].vx <= 0 || s.flakes[1].vx >= 0 {
		t.Errorf("near flakes not pushed away: vx %v, %v", s.flakes[0].vx, s.flakes[1].vx)
	}
	if s.flakes[2].vx != 0 {
		t.Errorf("far flake pushed: vx %v", s.flakes[2].vx)
	}
}

func TestAmountScalesFlakes(t *testing.T) {
	for _, amt := range []float64{0, 0.4, 2.5} {
		s := New(1000, 800, 1)
		s.Configure(Settings{Amount: amt, Size: 1, Piles: true})
		for range 600 {
			s.Step(1.0/60, Point{})
		}
		if lim := int(flakeCap * amt); len(s.flakes) > lim || (amt > 0 && len(s.flakes) < lim/2) {
			t.Errorf("amount %v: %d flakes, limit %d", amt, len(s.flakes), lim)
		}
	}
}

func TestSizeScalesFill(t *testing.T) {
	s := New(1000, 800, 1)
	s.flakes = []flake{{x: 1, y: 2, r: 2}}
	s.Configure(Settings{Amount: 1, Size: 1.5, Piles: true}) // decorations off
	dots := make([]float32, 100)
	if nd, _ := s.Fill(dots, nil); nd != DotFloats || dots[2] != 3 {
		t.Errorf("nd %d radius %v, want %d and 3", nd, dots[2], DotFloats)
	}
}

func TestPilesOffDropsSnowAndStopsLanding(t *testing.T) {
	s := New(1000, 800, 1)
	p := filled(1, Rect{100, 300, 300, 200}, s)
	s.Configure(Settings{Amount: 1, Size: 1, Piles: false})
	if total(p) != 0 || len(s.flakes) == 0 {
		t.Fatalf("piles not dropped: pile %v, flakes %d", total(p), len(s.flakes))
	}
	s.flakes = nil
	dropFlake(s, 200)
	if total(p) != 0 {
		t.Error("flake landed with piles off")
	}
}

func TestFlakeCap(t *testing.T) {
	s := New(1000, 800, 1)
	for range 600 {
		s.Step(1.0/60, Point{})
		if len(s.flakes) > flakeCap {
			t.Fatalf("%d flakes > cap %d", len(s.flakes), flakeCap)
		}
	}
	if len(s.flakes) < flakeCap/2 {
		t.Errorf("only %d flakes after 10s", len(s.flakes))
	}
}

func TestFillLayout(t *testing.T) {
	s := New(1000, 800, 1)
	filled(1, Rect{100, 300, 30, 200}, s)                  // 10 columns of height 5
	s.Configure(Settings{Amount: 1, Size: 1, Piles: true}) // decorations off
	s.flakes = []flake{{x: 1, y: 2, r: 3}}
	dots, tris := make([]float32, 100), make([]float32, 10000)
	nd, nt := s.Fill(dots, tris)
	if nd != DotFloats || dots[0] != 1 || dots[1] != 2 || dots[2] != 3 {
		t.Fatalf("flake dot: n=%d %v", nd, dots[:DotFloats])
	}
	// 12 surface points (edge, 10 columns, edge) give 11 quads of 2 triangles.
	if want := 11 * 2 * 3 * VertFloats; nt != want {
		t.Fatalf("pile floats %d, want %d", nt, want)
	}
	if nd, nt := s.Fill(make([]float32, 4), make([]float32, 10)); nd != 0 || nt != 0 {
		t.Errorf("small buffers overflowed: %d %d", nd, nt)
	}
}

// Replays a desktop switch as recorded from macOS: the current desktop's
// windows slide right off screen and turn hidden, the next desktop's slide
// in from the left; then the same back again.
func TestDesktopSlideKeepsSnow(t *testing.T) {
	full := func(x float64) Rect { return Rect{x, 33, 1728, 1020} }
	win := func(id uint32, x float64) Window { return Window{ID: id, Bounds: full(x)} }
	hidden := func(id uint32) Window { return Window{ID: id, Hidden: true} }

	s := New(1728, 1117, 1)
	s.SetWindows([]Window{win(1, 0), win(2, 0)}, Rect{}, 0)
	for _, id := range []uint32{1, 2} {
		for i := range s.piles[id].cols {
			s.piles[id].cols[i] = 5
		}
	}
	before := total(s.piles[1]) + total(s.piles[2])

	slide := func(out, in []uint32, xs []float64, back bool) {
		for _, x := range xs {
			var ws []Window
			for _, id := range out {
				ws = append(ws, win(id, x))
			}
			for _, id := range in {
				ws = append(ws, win(id, x-1640))
			}
			s.SetWindows(ws, Rect{}, 0.05)
			s.Step(0.05, Point{-1000, -1000})
		}
		var ws []Window
		for _, id := range in {
			ws = append(ws, win(id, 0))
		}
		for _, id := range out {
			ws = append(ws, hidden(id))
		}
		s.SetWindows(ws, Rect{}, 0.05)
	}

	s.flakes = nil
	slide([]uint32{1, 2}, []uint32{3, 4}, []float64{150, 316, 697, 1200, 1600}, false)
	if got := total(s.piles[1]) + total(s.piles[2]); got != before {
		t.Fatalf("snow on desktop-1 windows changed during the slide: %v -> %v", before, got)
	}
	if !s.Sliding() {
		t.Error("slide not detected")
	}

	slide([]uint32{3, 4}, []uint32{1, 2}, []float64{150, 316, 697, 1200, 1600}, true)
	if got := total(s.piles[1]) + total(s.piles[2]); got != before {
		t.Fatalf("snow lost after switching back: %v -> %v", before, got)
	}
	for range 20 { // let the slide settle
		s.SetWindows([]Window{win(1, 0), win(2, 0), hidden(3), hidden(4)}, Rect{}, 0.05)
		s.Step(0.05, Point{-1000, -1000})
	}
	if s.Sliding() {
		t.Fatal("still sliding long after the switch")
	}
	if _, nt := s.Fill(nil, make([]float32, 1<<21)); nt == 0 {
		t.Error("piles not drawn again after the slide settled")
	}
}

func TestLoneWindowSlidingToEmptyDesktopKeepsSnow(t *testing.T) {
	s := New(1728, 1117, 1)
	p := filled(1, Rect{100, 33, 1000, 900}, s)
	before := total(p)
	for _, x := range []float64{250, 416, 797, 1300} {
		s.SetWindows([]Window{{ID: 1, Bounds: Rect{x, 33, 1000, 900}}}, Rect{}, 0.05)
	}
	s.SetWindows([]Window{{ID: 1, Hidden: true}}, Rect{}, 0.05)
	if total(p) != before {
		t.Errorf("lone window lost snow sliding off: %v -> %v (flakes %d)", before, total(p), len(s.flakes))
	}
}

// Replays Mission Control as recorded from macOS: windows shrink in place,
// keeping their shape, then grow back. That is not a resize.
func TestMissionControlKeepsSnow(t *testing.T) {
	s := New(1728, 1117, 1)
	orig := Rect{0, 33, 1728, 1020}
	p := filled(1, orig, s)
	before := total(p)
	for _, b := range []Rect{{4, 43, 1604, 949}, {9, 65, 1360, 804}, {120, 106, 1100, 650}, {9, 65, 1360, 804}, orig} {
		s.SetWindows([]Window{{ID: 1, Bounds: b}}, Rect{}, 0.05)
		if b != orig && len(p.spans) != 0 {
			t.Fatalf("pile shown while the window is scaled to %v", b)
		}
	}
	for range 6 {
		s.SetWindows([]Window{{ID: 1, Bounds: orig}}, Rect{}, 0.05)
	}
	if total(p) != before || len(s.flakes) != 0 {
		t.Fatalf("Mission Control dropped snow: %v -> %v, %d flakes", before, total(p), len(s.flakes))
	}
	if len(p.spans) == 0 {
		t.Error("pile not shown again once the window is back")
	}
	// A real resize changes the shape and still drops the snow.
	s.SetWindows([]Window{{ID: 1, Bounds: Rect{0, 33, 1400, 1020}}}, Rect{}, 0.05)
	if total(p) != 0 || len(s.flakes) == 0 {
		t.Errorf("real resize kept the snow: pile %v, flakes %d", total(p), len(s.flakes))
	}
}

func TestDecorationsStayVisibleDuringSlide(t *testing.T) {
	s := New(1728, 1117, 1)
	a, b := Rect{100, 200, 600, 400}, Rect{800, 200, 600, 400}
	s.SetWindows([]Window{{ID: 1, Bounds: a}, {ID: 2, Bounds: b}}, Rect{}, 0.05)
	for _, id := range []uint32{1, 2} {
		for i := range s.piles[id].cols {
			s.piles[id].cols[i] = 5
		}
	}
	// Both slide by the same step: a desktop switch.
	a.X, b.X = a.X+150, b.X+150
	s.SetWindows([]Window{{ID: 1, Bounds: a}, {ID: 2, Bounds: b}}, Rect{}, 0.05)
	if !s.Sliding() {
		t.Fatal("slide not detected")
	}
	if _, nt := s.Fill(nil, make([]float32, 1<<21)); nt == 0 {
		t.Error("piles hidden during the slide; they should slide along with the windows")
	}
	s.SetWindows([]Window{{ID: 1, Bounds: a}, {ID: 2, Bounds: b}}, Rect{}, 0.05)
	if s.Sliding() {
		t.Error("slide not over one update after the windows stopped")
	}
}
