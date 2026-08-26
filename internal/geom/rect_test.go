package geom

import "testing"

func TestInsetClampsRatherThanInverting(t *testing.T) {
	r := Rect{X: 0, Y: 0, W: 100, H: 100}
	got := r.Inset(EdgesAll(80))
	if got.W != 0 || got.H != 0 {
		t.Fatalf("over-inset produced %s, want zero dimensions", got)
	}
	if got.X != 80 || got.Y != 80 {
		t.Fatalf("over-inset moved origin to (%d,%d), want (80,80)", got.X, got.Y)
	}
}

func TestInsetOutsetRoundTrip(t *testing.T) {
	r := Rect{X: 160, Y: 320, W: 4000, H: 2400}
	e := Edges{Top: 16, Right: 8, Bottom: 24, Left: 32}
	if got := r.Inset(e).Outset(e); got != r {
		t.Fatalf("round trip gave %s, want %s", got, r)
	}
}

// The y-flip is the single most dangerous line in the renderer, so it gets an
// explicit test with hand-checkable numbers.
func TestToPDFFlipsY(t *testing.T) {
	pageH := In(11) // Letter portrait: 12672 ticks
	// A 1-inch-tall header sitting at the very top of the page.
	header := Rect{X: In(1), Y: 0, W: In(6.5), H: In(1)}
	got := header.ToPDF(pageH)
	// In PDF space its bottom edge is 10 inches up from the page bottom.
	if got.Y != In(10) {
		t.Fatalf("header PDF y = %d ticks (%.2fpt), want %d", got.Y, got.Y.Points(), In(10))
	}
	if got.X != header.X || got.W != header.W || got.H != header.H {
		t.Fatalf("flip disturbed x/w/h: %s", got)
	}
	// Flipping twice returns the original.
	if again := got.ToPDF(pageH); again != header {
		t.Fatalf("double flip gave %s, want %s", again, header)
	}
}

func TestIntersect(t *testing.T) {
	a := Rect{X: 0, Y: 0, W: 100, H: 100}
	b := Rect{X: 50, Y: 50, W: 100, H: 100}
	if got := a.Intersect(b); got != (Rect{X: 50, Y: 50, W: 50, H: 50}) {
		t.Fatalf("overlap = %s", got)
	}
	far := Rect{X: 500, Y: 500, W: 10, H: 10}
	if got := a.Intersect(far); !got.IsEmpty() {
		t.Fatalf("disjoint rects intersected to %s", got)
	}
}

func TestEdgesUniform(t *testing.T) {
	if !EdgesAll(16).Uniform() {
		t.Fatal("EdgesAll should be uniform")
	}
	if EdgesVH(16, 32).Uniform() {
		t.Fatal("EdgesVH(16,32) should not be uniform")
	}
	if got := EdgesVH(16, 32).Horizontal(); got != 64 {
		t.Fatalf("Horizontal = %d, want 64", got)
	}
}
