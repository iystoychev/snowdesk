package snow

import (
	"math"
	"testing"
)

var testDock = Rect{300, 740, 400, 60}

// dropNear drops 10 flakes straight down at x from y and runs until they land or leave.
func dropNear(s *Sim, x, y float64) {
	for range 10 {
		s.flakes = append(s.flakes, flake{x: x, y: y, vy: 60})
	}
	for range 600 {
		s.t = 0
		s.wind, s.windTarget, s.windNext = 0, 0, 1e9
		s.step(1.0/60, Point{-1000, -1000})
		if len(s.flakes) == 0 {
			return
		}
	}
}

func mainSim(forest, xmas bool) *Sim {
	s := New(1000, 800, 1)
	s.Main = true
	c := DefaultSettings()
	c.Forest, c.XmasTree, c.Santa = forest, xmas, false
	s.Configure(c)
	s.SetWindows(nil, testDock, 0)
	return s
}

func TestSnowbankCatchesSnowBesideDock(t *testing.T) {
	s := mainSim(false, false)
	dropNear(s, 100, 700)
	p := s.piles[GroundID]
	if p == nil {
		t.Fatal("no snowbank pile")
	}
	if p.bounds.Y != testDock.Y {
		t.Errorf("snowbank at y=%v, want level with the Dock top %v", p.bounds.Y, testDock.Y)
	}
	if h := s.surface(p, 100); h >= testDock.Y {
		t.Errorf("no snow settled on the snowbank at x=100")
	}
	if inSpans(p.spans, 500) {
		t.Errorf("snowbank should leave the Dock's span to the Dock pile")
	}
	for _, id := range s.PileIDs() {
		if id == GroundID {
			t.Errorf("PileIDs must not list the snowbank")
		}
	}
}

func TestForestAndTreeCatchSnow(t *testing.T) {
	s := mainSim(true, true)
	var xmas *tree
	backs, fronts := 0, 0
	for i := range s.trees {
		switch tr := &s.trees[i]; {
		case tr.xmas:
			xmas = tr
		case tr.back:
			backs++
		default:
			fronts++
		}
	}
	if xmas == nil || backs == 0 || fronts == 0 {
		t.Fatalf("want a Christmas tree and both forest rows, got xmas=%v back=%d front=%d", xmas != nil, backs, fronts)
	}
	if xmas.x < testDock.X+testDock.W {
		t.Errorf("Christmas tree at x=%v should stand in the open space right of the Dock", xmas.x)
	}
	for _, tr := range s.trees {
		if !tr.xmas && !tr.back && math.Abs(tr.x-xmas.x) < xmasClearance {
			t.Errorf("forest tree at %v crowds the Christmas tree at %v", tr.x, xmas.x)
		}
	}
	for range 20 {
		dropNear(s, xmas.x+3, xmas.base-xmas.h-20)
	}
	snow := 0.0
	for _, tr := range xmas.tiers {
		snow += tr.snow
	}
	if snow == 0 {
		t.Fatal("no snow settled on the Christmas tree")
	}
	// Brushing the top tier knocks its snow off.
	top := &xmas.tiers[0]
	top.snow = 1
	s.step(1.0/60, Point{top.ax, (top.ay + top.by) / 2})
	if top.snow != 0 {
		t.Errorf("brushed tier still has snow %v", top.snow)
	}
}

func TestTreesKeepSnowAcrossWindowUpdates(t *testing.T) {
	s := mainSim(true, true)
	s.trees[0].tiers[0].snow = 0.5
	s.SetWindows([]Window{{ID: 5, Bounds: Rect{100, 100, 300, 200}}}, testDock, 1.0/20)
	if s.trees[0].tiers[0].snow != 0.5 {
		t.Error("a window update laid out the forest again and lost its snow")
	}
}

// fillPile gives a window a full pile of snow.
func fillPile(s *Sim, id uint32) *pile {
	p := s.piles[id]
	for i := range p.cols {
		p.cols[i] = p.colMax(i)
	}
	return p
}

// roll sweeps the cursor with Option held from x0 to x1 along y, then lets go.
func roll(s *Sim, x0, x1, y float64) *ball {
	s.Option = true
	for k := 0; k <= 60; k++ {
		s.step(1.0/60, Point{x0 + (x1-x0)*float64(k)/60, y})
	}
	b := s.carried
	return b
}

func TestRollSnowballAndBuildSnowman(t *testing.T) {
	s := New(1000, 800, 1)
	win := Window{ID: 1, Bounds: Rect{100, 400, 700, 300}}
	s.SetWindows([]Window{win}, Rect{}, 0)
	fillPile(s, 1)

	var built []*ball
	for k, span := range [][2]float64{{150, 330}, {360, 500}, {530, 640}} {
		b := roll(s, span[0], span[1], 396)
		if b == nil {
			t.Fatalf("ball %d: Option-sweeping through the drift made no ball", k)
		}
		if b.r <= ballStartR+2 {
			t.Fatalf("ball %d: did not grow while rolling, r=%v", k, b.r)
		}
		// Carry it over the stack (or a free spot for the first) and let go.
		target := Point{700, 300}
		if k > 0 {
			target = Point{built[0].x, built[0].y - 120}
		}
		for range 15 { // hold still over the spot, so it is not thrown
			s.step(1.0/60, target)
		}
		s.Option = false
		for range 120 { // let go in place, then wait for it to settle
			s.step(1.0/60, target)
		}
		if b.state != ballResting {
			t.Fatalf("ball %d did not come to rest (state %v)", k, b.state)
		}
		built = append(built, b)
	}
	if built[0].pile != 1 || built[0].below != nil {
		t.Errorf("first ball should rest on the window ledge")
	}
	if built[1].below != built[0] || built[2].below != built[1] {
		t.Fatalf("balls did not stack into a snowman")
	}
	if built[2].depth() != 2 || !s.onTop(built[2]) {
		t.Errorf("the head should be third in the stack and on top")
	}

	// The snowman rides along when the window moves slowly...
	win.Bounds.X += 5
	s.SetWindows([]Window{win}, Rect{}, 1.0/20)
	s.step(1.0/60, Point{-1000, -1000})
	if math.Abs(built[2].x-(built[0].x)) > 0.01 || built[0].x != 100+5+built[0].offX {
		t.Errorf("snowman did not follow the window")
	}
	// ...and topples when the window is closed.
	s.SetWindows(nil, Rect{}, 1.0/20)
	s.step(1.0/60, Point{-1000, -1000})
	for k, b := range built {
		if b.state != ballFalling {
			t.Errorf("ball %d should fall when its window closes, state %v", k, b.state)
		}
	}
}

func TestPickUpTopBall(t *testing.T) {
	s := New(1000, 800, 1)
	s.SetWindows([]Window{{ID: 1, Bounds: Rect{100, 400, 700, 300}}}, Rect{}, 0)
	fillPile(s, 1)
	b := roll(s, 150, 330, 396)
	s.Option = false
	for range 60 {
		s.step(1.0/60, Point{-1000, -1000})
	}
	if b.state != ballResting {
		t.Fatal("ball did not rest")
	}
	s.Option = true
	s.step(1.0/60, Point{b.x, b.y})
	if s.carried != b {
		t.Error("Option over a resting ball should pick it up")
	}
}

func TestSantaWalksByAndWaves(t *testing.T) {
	s := mainSim(true, true)
	c := s.cfg
	c.Santa = true
	s.Configure(c)
	s.CallSanta()
	if !s.claus.active {
		t.Fatal("Santa did not come when called")
	}
	waved, minX, maxX := false, s.claus.x, s.claus.x
	for range 60 * 60 {
		s.step(1.0/60, Point{-1000, -1000})
		if !s.claus.active {
			break
		}
		waved = waved || s.claus.waving > 0
		minX, maxX = math.Min(minX, s.claus.x), math.Max(maxX, s.claus.x)
	}
	if s.claus.active {
		t.Fatal("Santa never left")
	}
	if len(s.gifts) != 4 {
		t.Errorf("Santa should leave one present beside the three under the tree, have %d", len(s.gifts))
	} else if g := s.gifts[3]; g.drop != 0 {
		t.Errorf("the new present should have landed, still %v up", g.drop)
	}
	if !waved {
		t.Error("Santa did not stop to wave")
	}
	if minX > 0 || maxX < s.W {
		t.Errorf("Santa should cross the whole screen, went %v..%v", minX, maxX)
	}
	if s.santaNext <= s.t {
		t.Error("no next visit scheduled")
	}
}

func TestSecondaryScreenFullscreen(t *testing.T) {
	s := New(1000, 800, 1)
	s.NoDock = true
	s.SetWindows([]Window{{ID: 1, Bounds: Rect{0, 25, 1000, 775}}}, Rect{}, 0)
	if s.Fullscreen() {
		t.Error("a zoomed window below the menu bar is not fullscreen")
	}
	s.SetWindows([]Window{{ID: 1, Bounds: Rect{0, 0, 1000, 800}}}, Rect{}, 1.0/20)
	if !s.Fullscreen() {
		t.Error("a window covering the whole screen is fullscreen")
	}
}
