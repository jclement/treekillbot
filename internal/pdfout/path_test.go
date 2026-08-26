package pdfout

import (
	"math"
	"testing"

	"github.com/jclement/treekillbot/internal/geom"
)

// The property that makes a fill and a border share a silhouette: nothing the
// rounded rectangle emits may fall outside the rect it was given. Corners
// curve inward, straight edges lie on the edge, and no vertex escapes.
func TestRoundedRectStaysInsideItsRect(t *testing.T) {
	tests := []struct {
		name   string
		rect   geom.Rect
		radius geom.Tick
	}{
		{"square corners", geom.Rect{X: geom.Pt(10), Y: geom.Pt(20), W: geom.Pt(100), H: geom.Pt(60)}, 0},
		{"small radius", geom.Rect{X: geom.Pt(10), Y: geom.Pt(20), W: geom.Pt(100), H: geom.Pt(60)}, geom.Pt(2)},
		{"typical panel", geom.Rect{X: geom.Pt(36), Y: geom.Pt(36), W: geom.Pt(300), H: geom.Pt(200)}, geom.Pt(6)},
		{"radius clamped to half the short side", geom.Rect{W: geom.Pt(40), H: geom.Pt(20)}, geom.Pt(50)},
		{"at the origin", geom.Rect{W: geom.Pt(20), H: geom.Pt(20)}, geom.Pt(4)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var path pathBuilder
			path.addRect(tc.rect, tc.radius)
			flat := path.flatten()
			if len(flat) != 1 || !flat[0].closed {
				t.Fatalf("want one closed subpath, got %d", len(flat))
			}

			left, top := tc.rect.X.Points(), tc.rect.Y.Points()
			right, bottom := tc.rect.Right().Points(), tc.rect.Bottom().Points()
			// The emitted coordinates are exact to a tick, so no epsilon is
			// needed for the straight edges; the curve flattener produces
			// interior points, which are strictly inside.
			for _, p := range flat[0].points {
				if p.X < left || p.X > right || p.Y < top || p.Y > bottom {
					t.Fatalf("point (%v,%v) escapes the rect [%v,%v]x[%v,%v]",
						p.X, p.Y, left, right, top, bottom)
				}
			}
			assertTouchesEveryEdge(t, flat[0], left, top, right, bottom)
		})
	}
}

// A square-cornered rectangle is four points and nothing else. Flattening it
// into a hundred would bloat every page in the document.
func TestSquareRectIsFourPoints(t *testing.T) {
	var path pathBuilder
	path.addRect(geom.Rect{W: geom.Pt(10), H: geom.Pt(10)}, 0)
	flat := path.flatten()
	if got := len(flat[0].points); got != 4 {
		t.Errorf("square rect emitted %d points, want 4", got)
	}
}

// The flattening budget has to keep the polyline's deviation from the true arc
// below the 0.01pt grid the coordinates are written on, or the approximation
// becomes visible in the file rather than disappearing into it.
func TestCornerFlatteningStaysBelowTheOutputGrid(t *testing.T) {
	const radiusPt = 6.0
	rect := geom.Rect{W: geom.Pt(200), H: geom.Pt(120)}
	var path pathBuilder
	path.addRect(rect, geom.Pt(radiusPt))
	points := path.flatten()[0].points

	// The top-left corner's centre. Every point on that arc should be
	// radiusPt away from it.
	centreX, centreY := radiusPt, radiusPt
	worst := 0.0
	for _, p := range points {
		if p.X > radiusPt || p.Y > radiusPt {
			continue // not on the top-left arc
		}
		distance := math.Hypot(p.X-centreX, p.Y-centreY)
		if deviation := math.Abs(distance - radiusPt); deviation > worst {
			worst = deviation
		}
	}
	const outputGridPt = 0.01
	if worst >= outputGridPt {
		t.Errorf("worst corner deviation %.5fpt, want below the %.2fpt output grid", worst, outputGridPt)
	}
	t.Logf("worst deviation on a %.0fpt corner: %.5fpt", radiusPt, worst)
}

// The Bézier circle constant is the one number in the corner construction that
// is not obvious, so it is worth pinning: a quarter circle's control points sit
// at 4/3*(sqrt(2)-1) of the radius.
func TestBezierCircleKappa(t *testing.T) {
	const want = 0.5522847498307933
	if math.Abs(bezierCircleKappa-want) > 1e-15 {
		t.Errorf("bezierCircleKappa = %.17f, want %.17f", bezierCircleKappa, want)
	}
}

// An empty rect paints nothing, and a subpath of one point would emit a
// moveto with no operator to consume it.
func TestDegeneratePathsAreDropped(t *testing.T) {
	tests := []struct {
		name  string
		build func(*pathBuilder)
	}{
		{"empty rect", func(p *pathBuilder) { p.addRect(geom.Rect{W: 0, H: geom.Pt(10)}, 0) }},
		{"lone moveto", func(p *pathBuilder) { p.moveTo(geom.Pt(1), geom.Pt(1)) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var path pathBuilder
			tc.build(&path)
			if flat := path.flatten(); len(flat) != 0 {
				t.Errorf("emitted %d subpaths, want none", len(flat))
			}
		})
	}
}

// A lineTo with no preceding moveTo is a malformed path in PDF. Starting one
// at the origin keeps a painting bug from taking down a hundred-page build.
func TestLineToWithoutMoveToStartsAPath(t *testing.T) {
	var path pathBuilder
	path.lineTo(geom.Pt(10), geom.Pt(10))
	flat := path.flatten()
	if len(flat) != 1 || len(flat[0].points) != 2 {
		t.Fatalf("want one two-point subpath, got %+v", flat)
	}
	if flat[0].points[0].X != 0 || flat[0].points[0].Y != 0 {
		t.Errorf("implicit start = %+v, want the origin", flat[0].points[0])
	}
}

// assertTouchesEveryEdge checks that the silhouette actually reaches all four
// declared edges. Without it, "nothing escapes the rect" would also be
// satisfied by a shape two points across.
func assertTouchesEveryEdge(t *testing.T, sub flatSubpath, left, top, right, bottom float64) {
	t.Helper()
	var onLeft, onTop, onRight, onBottom bool
	for _, p := range sub.points {
		onLeft = onLeft || p.X == left
		onRight = onRight || p.X == right
		onTop = onTop || p.Y == top
		onBottom = onBottom || p.Y == bottom
	}
	if !onLeft || !onTop || !onRight || !onBottom {
		t.Errorf("silhouette misses an edge (left=%v top=%v right=%v bottom=%v)",
			onLeft, onTop, onRight, onBottom)
	}
}
