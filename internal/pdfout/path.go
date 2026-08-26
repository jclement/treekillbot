// Path construction, in ticks, kept independent of how it is finally painted.
//
// render.Canvas models PDF's content-stream operators: build a path from
// moveto/lineto/curveto, then paint it once. gopdf's public API has no such
// operators — it offers whole shapes (Line, Polygon, Curve, Rectangle), each
// of which emits its own complete path and paint operator. So the path lives
// here until a paint call can hand it over as the closest shape gopdf can
// draw, which means curves are flattened to polylines on the way out.
//
// Two consequences worth knowing, both documented at their call sites in
// canvas.go: an open stroked subpath is emitted as consecutive segments rather
// than one joined path, and curves are approximated. The Bézier geometry is
// still built exactly, so the day a backend can emit "c" operators, deleting
// flatten() is the whole change.
package pdfout

import (
	"math"

	"github.com/signintech/gopdf"

	"github.com/jclement/treekillbot/internal/geom"
)

// bezierCircleKappa is the control-point offset, as a fraction of the radius,
// that makes a cubic Bézier approximate a quarter circle. Its maximum radial
// error is about 0.02% of the radius — a thousandth of a point on a 6pt
// corner, far below anything a printer resolves.
//
// math.Sqrt2 is an untyped constant, so this is computed once at compile time.
const bezierCircleKappa = 4.0 / 3.0 * (math.Sqrt2 - 1)

// Flattening budget. A line segment per half point of control polygon keeps a
// 6pt corner's worst-case deviation near 0.005pt, which is below the 0.01pt
// grid the PDF coordinates are written on, so the approximation disappears
// before it reaches the file. The floor stops tiny corners from degenerating
// into a visible chamfer; the ceiling stops a page-sized curve from filling
// the content stream.
const (
	flattenTicksPerSegment = 8
	minFlattenSegments     = 12
	maxFlattenSegments     = 96
)

type point struct{ X, Y geom.Tick }

// segment is one step of a subpath: a straight line to To, or a cubic Bézier
// through C1 and C2 to To.
type segment struct {
	curve  bool
	c1, c2 point
	to     point
}

// subpath is a connected run of segments. Closed subpaths are the ones gopdf
// can paint as a single polygon.
type subpath struct {
	start  point
	segs   []segment
	closed bool
}

// pathBuilder accumulates subpaths until a painting operation consumes them.
type pathBuilder struct {
	subpaths []subpath
}

func (p *pathBuilder) isEmpty() bool { return len(p.subpaths) == 0 }

func (p *pathBuilder) reset() { p.subpaths = p.subpaths[:0] }

func (p *pathBuilder) moveTo(x, y geom.Tick) {
	p.subpaths = append(p.subpaths, subpath{start: point{x, y}})
}

// current returns the subpath being built, starting one at the origin if the
// caller began with a lineTo. PDF treats that as an error; being lenient here
// keeps a painting bug from becoming a crash in a batch of a hundred pages.
func (p *pathBuilder) current() *subpath {
	if len(p.subpaths) == 0 {
		p.moveTo(0, 0)
	}
	return &p.subpaths[len(p.subpaths)-1]
}

func (p *pathBuilder) lineTo(x, y geom.Tick) {
	sp := p.current()
	sp.segs = append(sp.segs, segment{to: point{x, y}})
}

func (p *pathBuilder) curveTo(c1x, c1y, c2x, c2y, x, y geom.Tick) {
	sp := p.current()
	sp.segs = append(sp.segs, segment{curve: true, c1: point{c1x, c1y}, c2: point{c2x, c2y}, to: point{x, y}})
}

func (p *pathBuilder) close() { p.current().closed = true }

// addRect appends a rectangle whose OUTER silhouette is exactly r.
//
// With a radius, the straight edges lie on r's edges and the corner arcs curve
// inward, so the shape never exceeds r in any direction — which is what makes
// a fill and a border built from the same call share one silhouette, with no
// background hairline showing past the stroke. The radius is clamped to half
// the shorter side, since a larger one has no meaning and gopdf rejects it.
func (p *pathBuilder) addRect(r geom.Rect, radius geom.Tick) {
	if r.IsEmpty() {
		return
	}
	limit := geom.MinTick(r.W, r.H) / 2
	if radius > limit {
		radius = limit
	}
	if radius <= 0 {
		p.moveTo(r.X, r.Y)
		p.lineTo(r.Right(), r.Y)
		p.lineTo(r.Right(), r.Bottom())
		p.lineTo(r.X, r.Bottom())
		p.close()
		return
	}

	// The control-point offset from each corner. Computed once per rectangle
	// rather than per corner so all four corners are identical to the tick.
	pull := geom.Tick(math.Round(float64(radius) * bezierCircleKappa))
	left, top, right, bottom := r.X, r.Y, r.Right(), r.Bottom()

	p.moveTo(left+radius, top)
	p.lineTo(right-radius, top)
	p.curveTo(right-radius+pull, top, right, top+radius-pull, right, top+radius)
	p.lineTo(right, bottom-radius)
	p.curveTo(right, bottom-radius+pull, right-radius+pull, bottom, right-radius, bottom)
	p.lineTo(left+radius, bottom)
	p.curveTo(left+radius-pull, bottom, left, bottom-radius+pull, left, bottom-radius)
	p.lineTo(left, top+radius)
	p.curveTo(left, top+radius-pull, left+radius-pull, top, left+radius, top)
	p.close()
}

// flatSubpath is a subpath reduced to the polyline gopdf can draw. Coordinates
// are in points and still in the layout engine's top-left, y-down space:
// gopdf performs the y-flip itself when it writes the content stream.
type flatSubpath struct {
	points []gopdf.Point
	closed bool
}

// flatten converts every subpath to a polyline, subdividing curves. Subpaths
// with fewer than two points are dropped: they would paint nothing, and gopdf
// would still emit a "m" with no operator to consume it.
func (p *pathBuilder) flatten() []flatSubpath {
	out := make([]flatSubpath, 0, len(p.subpaths))
	for _, sp := range p.subpaths {
		pts := []gopdf.Point{toPDFPoint(sp.start)}
		cursor := sp.start
		for _, seg := range sp.segs {
			if !seg.curve {
				pts = append(pts, toPDFPoint(seg.to))
				cursor = seg.to
				continue
			}
			pts = appendFlattenedCurve(pts, cursor, seg)
			cursor = seg.to
		}
		if len(pts) < 2 {
			continue
		}
		out = append(out, flatSubpath{points: pts, closed: sp.closed})
	}
	return out
}

// appendFlattenedCurve subdivides one cubic into line segments, omitting the
// start point (already in pts) and including the end point exactly, so that
// consecutive segments meet with no gap and the final vertex is not an
// approximation.
func appendFlattenedCurve(pts []gopdf.Point, from point, seg segment) []gopdf.Point {
	steps := flattenSteps(from, seg)
	x0, y0 := float64(from.X), float64(from.Y)
	x1, y1 := float64(seg.c1.X), float64(seg.c1.Y)
	x2, y2 := float64(seg.c2.X), float64(seg.c2.Y)
	x3, y3 := float64(seg.to.X), float64(seg.to.Y)

	for i := 1; i < steps; i++ {
		t := float64(i) / float64(steps)
		u := 1 - t
		a, b, c, d := u*u*u, 3*u*u*t, 3*u*t*t, t*t*t
		pts = append(pts, gopdf.Point{
			X: ticksToPoints(a*x0 + b*x1 + c*x2 + d*x3),
			Y: ticksToPoints(a*y0 + b*y1 + c*y2 + d*y3),
		})
	}
	return append(pts, toPDFPoint(seg.to))
}

// flattenSteps picks the subdivision count from the control polygon's
// Manhattan length. Integer arithmetic on integer input, so two runs of the
// same document always subdivide the same curve the same way.
func flattenSteps(from point, seg segment) int {
	length := manhattan(from, seg.c1) + manhattan(seg.c1, seg.c2) + manhattan(seg.c2, seg.to)
	steps := int(length / flattenTicksPerSegment)
	if steps < minFlattenSegments {
		return minFlattenSegments
	}
	if steps > maxFlattenSegments {
		return maxFlattenSegments
	}
	return steps
}

func manhattan(a, b point) geom.Tick { return (b.X - a.X).Abs() + (b.Y - a.Y).Abs() }

// toPDFPoint converts a layout-space point to the points gopdf expects.
//
// There is no y-flip here, and that is not an oversight: gopdf's drawing API
// is already top-left origin and writes "pageHeight - y" into the content
// stream itself. Flipping with Rect.ToPDF on the way in would flip the page
// twice. DESIGN.md D3's "exactly one flip" still holds — it just lives in the
// library rather than in this package.
func toPDFPoint(p point) gopdf.Point {
	return gopdf.Point{X: p.X.Points(), Y: p.Y.Points()}
}

// ticksToPoints converts a fractional tick count, which only the curve
// flattener produces, to points.
func ticksToPoints(ticks float64) float64 { return ticks / float64(geom.TicksPerPt) }
