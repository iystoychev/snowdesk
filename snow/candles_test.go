package snow

import (
	"math"
	"testing"
)

func candleSim() *Sim {
	s := extrasSim(false, false, false)
	c := s.cfg
	c.Candles = true
	s.Configure(c)
	return s
}

func TestCandlesOffByDefault(t *testing.T) {
	s := New(1000, 800, 3)
	if _, ok := s.CandleRect(); ok {
		t.Error("candles are on by default")
	}
}

func TestCandleRectSitsInItsCorner(t *testing.T) {
	s := candleSim()
	for corner, want := range map[int]Point{
		BottomLeft: {0, 800}, BottomRight: {1000, 800}, TopLeft: {0, 0}, TopRight: {1000, 0},
	} {
		c := s.cfg
		c.CandleCorner = corner
		s.Configure(c)
		r, ok := s.CandleRect()
		if !ok || r.X < 0 || r.Y < 0 || r.X+r.W > s.W || r.Y+r.H > s.H {
			t.Fatalf("corner %d: rect %+v off screen", corner, r)
		}
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		if math.Abs(cx-want.X) > 120 || math.Abs(cy-want.Y) > 200 {
			t.Errorf("corner %d: rect %+v is not near %+v", corner, r, want)
		}
	}
}

func TestDraggedCandlesSnapToNearestCorner(t *testing.T) {
	s := candleSim()
	run(s, 0.1, noCursor) // starts bottom left
	// Drag up and right, past the middle of the screen, and let go.
	for range 10 {
		s.DragCandles(70, -50)
		s.Step(1.0/60, noCursor)
	}
	if got := s.DropCandles(); got != TopRight {
		t.Fatalf("dropped at (%.0f, %.0f): corner %d, want TopRight", s.candle.x, s.candle.y, got)
	}
	run(s, 1.5, noCursor)
	hx, hy := s.candleHome(TopRight)
	if math.Hypot(s.candle.x-hx, s.candle.y-hy) > 1 {
		t.Errorf("candles at (%.0f, %.0f), want them to glide to (%.0f, %.0f)", s.candle.x, s.candle.y, hx, hy)
	}
	dots, tris := make([]float32, 1<<12), make([]float32, 1<<16)
	if nd, nt := s.Fill(dots, tris); nd == 0 || nt == 0 {
		t.Error("candles drew nothing")
	}
}
