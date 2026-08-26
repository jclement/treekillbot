package decor

import (
	"testing"

	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/render"
)

// pageGrid is a page-global lattice anchored an inch in from the paper's corner,
// which is where a page's content rect starts on a default Letter page.
var pageGrid = Grid{Origin: geom.Rect{X: geom.In(1), Y: geom.In(1), W: geom.In(6.5), H: geom.In(9)}, Pitch: geom.Mm(5)}

// dotCentre is one painted dot's centre, in ticks.
type dotCentre struct{ X, Y geom.Tick }

// dotCentres returns the centre of every dot painted.
func dotCentres(ops []render.Op) []dotCentre {
	var out []dotCentre
	for _, op := range ops {
		if op.Kind != render.OpRect || len(op.Args) < 4 {
			continue
		}
		x, y := ticks(op.Args[0]), ticks(op.Args[1])
		out = append(out, dotCentre{X: x + ticks(op.Args[2])/2, Y: y + ticks(op.Args[3])/2})
	}
	return out
}

// The point of `grid-origin: page`: two panels at unrelated y offsets show one
// continuous lattice, so a spread reads as a sheet of dot-grid paper with boxes
// on it rather than as a collection of unrelated grids.
func TestAdjacentPanelsShareOnePageLattice(t *testing.T) {
	d := build(t, "line-style: dotted", "dot-pitch: 5mm", "dot-size: 1pt")
	pitch := geom.Mm(5)

	// Deliberately awkward offsets: neither is a whole number of pitches from
	// the page grid origin, and they differ from each other by a fraction of one.
	first := geom.Rect{X: geom.In(1), Y: geom.In(1) + 37, W: geom.In(3), H: geom.Pt(150)}
	second := geom.Rect{X: geom.In(4.2), Y: geom.In(1) + 211, W: geom.In(3), H: geom.Pt(150)}

	for _, content := range []geom.Rect{first, second} {
		centres := dotCentres(draw(d, content, pageGrid))
		if len(centres) == 0 {
			t.Fatalf("panel at %v drew no dots", content)
		}
		for _, c := range centres {
			if (c.X-pageGrid.Origin.X)%pitch != 0 || (c.Y-pageGrid.Origin.Y)%pitch != 0 {
				t.Fatalf("dot at (%d,%d) is off the page lattice anchored at (%d,%d) pitch %d",
					c.X, c.Y, pageGrid.Origin.X, pageGrid.Origin.Y, pitch)
			}
		}
	}
}

// `grid-origin: box` is the escape hatch, and it must actually differ: the
// lattice centres itself in the box instead of continuing the page's.
func TestBoxOriginCentresTheLattice(t *testing.T) {
	pitch := geom.Mm(5)
	content := geom.Rect{X: geom.In(1) + 37, Y: geom.In(1) + 37, W: geom.Pt(150), H: geom.Pt(150)}

	page := dotCentres(draw(build(t, "line-style: dotted", "dot-pitch: 5mm", "dot-size: 1pt"), content, pageGrid))
	box := dotCentres(draw(build(t, "line-style: dotted", "dot-pitch: 5mm", "dot-size: 1pt", "grid-origin: box"), content, pageGrid))
	if len(box) == 0 || len(page) == 0 {
		t.Fatal("no dots")
	}
	if box[0] == page[0] {
		t.Fatal("box origin produced the page lattice; the two must differ here")
	}

	leftover := content.W - geom.Tick(int(content.W/pitch))*pitch
	if want := content.X + leftover/2; box[0].X != want {
		t.Errorf("first dot column at %d, want the centred anchor %d", box[0].X, want)
	}
}

// gopdf has no SetLineCap, so a zero-length stroke deposits no ink and the page
// comes out blank (DESIGN.md D7). Dots must be filled shapes.
func TestDotsAreFilledShapes(t *testing.T) {
	ops := draw(build(t, "line-style: dotted", "dot-pitch: 5mm", "dot-size: 1pt"), panel, pageGrid)

	if countOps(ops, render.OpFill) != 1 {
		t.Errorf("%d fill operators, want exactly 1 for the whole grid", countOps(ops, render.OpFill))
	}
	if countOps(ops, render.OpStrokeLines) != 0 || countOps(ops, render.OpStroke) != 0 {
		t.Error("dots were stroked; a zero-length butt-capped stroke draws nothing")
	}
	for _, op := range ops {
		if op.Kind != render.OpRect {
			continue
		}
		if op.Args[2] <= 0 || op.Args[3] <= 0 {
			t.Fatalf("degenerate dot %v", op.Args)
		}
		if want := op.Args[2] / 2; op.Args[4] != want {
			t.Fatalf("dot radius %v, want half its width %v — not a circle", op.Args[4], want)
		}
	}
}

// No dot may be clipped by the content edge, so the lattice is inset by the dot
// radius before the index range is taken.
func TestDotsStayInsideTheContentRect(t *testing.T) {
	content := geom.Rect{X: geom.In(1), Y: geom.In(1), W: geom.Mm(50), H: geom.Mm(50)}
	radius := geom.Pt(1) / 2
	for _, c := range dotCentres(draw(build(t, "line-style: dotted", "dot-pitch: 5mm", "dot-size: 1pt"), content, pageGrid)) {
		if c.X-radius < content.X || c.X+radius > content.Right() ||
			c.Y-radius < content.Y || c.Y+radius > content.Bottom() {
			t.Fatalf("dot at (%d,%d) with radius %d is clipped by %v", c.X, c.Y, radius, content)
		}
	}
}

// Major graph lines are indexed from the page grid, not from the panel, so they
// align across panels the same way the minor lines do.
func TestGraphMajorsCountFromThePageOrigin(t *testing.T) {
	pitch := geom.Mm(5)
	content := geom.Rect{X: geom.In(1) + 133, Y: geom.In(1) + 71, W: geom.Mm(60), H: geom.Mm(60)}
	ops := draw(build(t, "line-style: graph", "dot-pitch: 5mm", "grid-major: 5",
		"line-width: 0.4pt", "grid-major-width: 0.75pt"), content, pageGrid)

	// Two pens, minors first then majors, so the heavy lines overprint cleanly.
	if got := countOps(ops, render.OpStrokeLines); got != 2 {
		t.Fatalf("%d line batches, want one for the minors and one for the majors", got)
	}
	var majorPenSeen bool
	majors := 0
	for _, op := range ops {
		if op.Kind == render.OpSetStroke && op.Width == 0.75 {
			majorPenSeen = true
			continue
		}
		if op.Kind != render.OpStrokeLines || !majorPenSeen {
			continue
		}
		for _, s := range segments([]render.Op{op}) {
			majors++
			offset := s.X1 - pageGrid.Origin.X
			if s.X1 != s.X2 {
				offset = s.Y1 - pageGrid.Origin.Y
			}
			if offset%(pitch*5) != 0 {
				t.Errorf("major line %d ticks from the page origin is not a multiple of 5 squares", offset)
			}
		}
	}
	if majors == 0 {
		t.Fatal("no major lines drawn")
	}
}

// A dot or graph grid has no writing rules and no natural height: it fills.
func TestLatticeDecorationsFill(t *testing.T) {
	for _, style := range []string{"dotted", "graph"} {
		d := build(t, "line-style: "+style)
		if len(d.Baselines(panel)) != 0 {
			t.Errorf("%s reported writing rules", style)
		}
		if d.NaturalHeight() != 0 {
			t.Errorf("%s wants a natural height; it should fill", style)
		}
	}
}

// A pitch below a point is a grey wash, not a guide, and a page of them is
// millions of shapes. Clamping up beats hanging the PDF writer.
func TestAbsurdPitchIsClamped(t *testing.T) {
	content := geom.Rect{X: 0, Y: 0, W: geom.Pt(72), H: geom.Pt(72)}
	dots := dotCentres(draw(build(t, "line-style: dotted", "dot-pitch: 0.0625pt", "dot-size: 0.5pt"), content, Grid{}))
	if len(dots) > 73*73 {
		t.Fatalf("%d dots from a 1/16pt pitch; the clamp did not hold", len(dots))
	}
}

// The schema promises `dot-pitch` defaults to `line-pitch`, so a square dot grid
// is one number. The page grid supplies the ORIGIN, which is what makes adjacent
// panels continuous; its pitch is only a fallback.
func TestDotPitchDefaultsToLinePitch(t *testing.T) {
	content := geom.Rect{X: geom.In(1), Y: geom.In(1), W: geom.Mm(60), H: geom.Mm(60)}
	// pageGrid's pitch is 5mm; this panel rules at 8mm and must use its own.
	centres := dotCentres(draw(build(t, "line-style: dotted", "line-pitch: 8mm", "dot-size: 1pt"), content, pageGrid))
	if len(centres) < 4 {
		t.Fatalf("%d dots, want a grid", len(centres))
	}

	var pitch geom.Tick
	for _, c := range centres {
		if c.Y != centres[0].Y {
			break
		}
		if c.X != centres[0].X && pitch == 0 {
			pitch = c.X - centres[0].X
		}
	}
	if pitch != geom.Mm(8) {
		t.Errorf("dot pitch %d, want line-pitch %d rather than the page grid's %d",
			pitch, geom.Mm(8), pageGrid.Pitch)
	}
	if (centres[0].X-pageGrid.Origin.X)%pitch != 0 {
		t.Errorf("the lattice is no longer anchored to the page origin")
	}
}
