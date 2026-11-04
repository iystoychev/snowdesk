package snow

// Draw output. The renderer knows two shapes only, so every effect is plain
// Go: soft round dots (flakes, bulbs, sparkles, frost crystals) and colored
// triangles (piles, icicles, light wire, frost).
const (
	DotFloats  = 8 // x, y, radius, inner (0 = all glow, 1 = hard disc), r, g, b, a
	VertFloats = 6 // x, y, r, g, b, a
)

type rgba [4]float32

// frame fills caller-owned buffers; output past capacity is dropped.
type frame struct {
	dots, tris []float32
	nd, nt     int
}

func (f *frame) dot(x, y, r, inner float64, c rgba) {
	if f.nd+DotFloats > len(f.dots) {
		return
	}
	copy(f.dots[f.nd:], []float32{float32(x), float32(y), float32(r), float32(inner), c[0], c[1], c[2], c[3]})
	f.nd += DotFloats
}

func (f *frame) tri(x0, y0 float64, c0 rgba, x1, y1 float64, c1 rgba, x2, y2 float64, c2 rgba) {
	if f.nt+3*VertFloats > len(f.tris) {
		return
	}
	copy(f.tris[f.nt:], []float32{
		float32(x0), float32(y0), c0[0], c0[1], c0[2], c0[3],
		float32(x1), float32(y1), c1[0], c1[1], c1[2], c1[3],
		float32(x2), float32(y2), c2[0], c2[1], c2[2], c2[3],
	})
	f.nt += 3 * VertFloats
}

// quad draws the four-cornered shape a-b-c-d (in order) in one color.
func (f *frame) quad(ax, ay, bx, by, cx, cy, dx, dy float64, c rgba) {
	f.tri(ax, ay, c, bx, by, c, cx, cy, c)
	f.tri(ax, ay, c, cx, cy, c, dx, dy, c)
}

// Fill writes the current frame: dots into dots, triangle vertices into
// tris. It returns how many floats it wrote to each.
func (s *Sim) Fill(dots, tris []float32) (nd, nt int) {
	f := &frame{dots: dots, tris: tris}
	s.drawFrost(f)
	s.drawPiles(f)
	s.drawIcicles(f)
	s.drawLights(f)
	snow := rgba{1, 1, 1, 0.95}
	for _, fl := range s.flakes {
		f.dot(fl.x, fl.y, fl.r*s.cfg.Size, 0.55, snow)
	}
	s.drawSparkles(f)
	return f.nd, f.nt
}

func (s *Sim) drawPiles(f *frame) {
	white := rgba{0.97, 0.97, 0.97, 1}
	for _, p := range s.piles {
		base := p.bounds.Y
		for _, sp := range p.spans {
			lo := max(int((sp[0]-p.bounds.X)/colW), 0)
			hi := min(int((sp[1]-p.bounds.X)/colW)+1, len(p.cols))
			// Surface points from span start to span end; ends sit on the edge.
			px, py := sp[0], base
			for i := lo; i <= hi; i++ {
				x, h := sp[1], 0.0
				if i < hi {
					x, h = clampF(p.colX(i), sp[0], sp[1]), p.cols[i]
				}
				y := base - h
				if py < base-0.01 || y < base-0.01 {
					f.quad(px, py, x, y, x, base, px, base, white)
				}
				px, py = x, y
			}
		}
	}
}

func clampF(v, lo, hi float64) float64 { return max(lo, min(v, hi)) }
