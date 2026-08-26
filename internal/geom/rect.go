// Rectangles and edge insets, in the layout engine's coordinate system.
//
// The layout engine works in TOP-LEFT origin, y-increasing-downward coordinates
// — the opposite of PDF's native bottom-left origin. Three reasons this is
// worth the one conversion it costs:
//
//  1. Every distribution loop reads "y += h" instead of the sign-inverted
//     "child.Y = parent.Y + parent.H - offset - child.H", which is where
//     off-by-one-border bugs breed.
//  2. Font metrics are naturally distances measured downward from the top.
//  3. --dump-layout output and every human description of a form ("the header
//     is at the top") agree with the numbers.
//
// The flip happens in exactly one place, Rect.ToPDF, and it is exact because
// both operands are tick multiples.
package geom

import "fmt"

// Rect is an axis-aligned rectangle with a top-left origin and y increasing
// downward. Width and height are non-negative by construction everywhere the
// layout engine produces one.
type Rect struct {
	X, Y, W, H Tick
}

// Right returns the x coordinate of the rectangle's right edge.
func (r Rect) Right() Tick { return r.X + r.W }

// Bottom returns the y coordinate of the rectangle's bottom edge. Because y
// increases downward, this is the larger y value.
func (r Rect) Bottom() Tick { return r.Y + r.H }

// CenterX returns the horizontal midpoint, rounded down to a whole tick.
func (r Rect) CenterX() Tick { return r.X + r.W/2 }

// CenterY returns the vertical midpoint, rounded down to a whole tick.
func (r Rect) CenterY() Tick { return r.Y + r.H/2 }

// IsEmpty reports whether the rectangle encloses no area. Empty rectangles are
// legal — a panel given zero height still has a position — but nothing is
// painted into one.
func (r Rect) IsEmpty() bool { return r.W <= 0 || r.H <= 0 }

// Inset shrinks the rectangle by the given edge widths, clamping the result to
// non-negative dimensions rather than producing an inverted rectangle. Padding
// larger than the box is an authoring error reported elsewhere; producing a
// negative width here would corrupt every downstream computation.
func (r Rect) Inset(e Edges) Rect {
	out := Rect{X: r.X + e.Left, Y: r.Y + e.Top, W: r.W - e.Left - e.Right, H: r.H - e.Top - e.Bottom}
	if out.W < 0 {
		out.W = 0
	}
	if out.H < 0 {
		out.H = 0
	}
	return out
}

// InsetUniform shrinks the rectangle by d on all four sides.
func (r Rect) InsetUniform(d Tick) Rect { return r.Inset(EdgesAll(d)) }

// Outset grows the rectangle by the given edge widths.
func (r Rect) Outset(e Edges) Rect {
	return Rect{X: r.X - e.Left, Y: r.Y - e.Top, W: r.W + e.Left + e.Right, H: r.H + e.Top + e.Bottom}
}

// Translate moves the rectangle without resizing it.
func (r Rect) Translate(dx, dy Tick) Rect {
	return Rect{X: r.X + dx, Y: r.Y + dy, W: r.W, H: r.H}
}

// Intersect returns the overlapping region of two rectangles, or an empty
// rectangle positioned at a's origin when they do not overlap.
func (r Rect) Intersect(o Rect) Rect {
	x0, y0 := MaxOf(r.X, o.X), MaxOf(r.Y, o.Y)
	x1, y1 := MinTick(r.Right(), o.Right()), MinTick(r.Bottom(), o.Bottom())
	if x1 <= x0 || y1 <= y0 {
		return Rect{X: r.X, Y: r.Y}
	}
	return Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

// Contains reports whether o lies entirely within r.
func (r Rect) Contains(o Rect) bool {
	return o.X >= r.X && o.Y >= r.Y && o.Right() <= r.Right() && o.Bottom() <= r.Bottom()
}

// ToPDF converts to PDF user space: same x, but y measured upward from the
// bottom of a page of height pageH. This is the ONLY y-flip in the codebase;
// if you find yourself writing "pageH -" anywhere else, that is the bug.
func (r Rect) ToPDF(pageH Tick) Rect {
	return Rect{X: r.X, Y: pageH - r.Y - r.H, W: r.W, H: r.H}
}

// String renders the rectangle in points, for --dump-layout and diagnostics.
func (r Rect) String() string {
	return fmt.Sprintf("(%.2f,%.2f %.2fx%.2f)", r.X.Points(), r.Y.Points(), r.W.Points(), r.H.Points())
}

// Edges is a set of four per-side lengths: padding, margin, or border width.
type Edges struct {
	Top, Right, Bottom, Left Tick
}

// EdgesAll returns edges with the same value on all four sides.
func EdgesAll(d Tick) Edges { return Edges{Top: d, Right: d, Bottom: d, Left: d} }

// EdgesVH returns edges with one value for top/bottom and another for
// left/right, matching the two-value CSS shorthand the DSL accepts.
func EdgesVH(v, h Tick) Edges { return Edges{Top: v, Right: h, Bottom: v, Left: h} }

// Horizontal returns the total width consumed by the left and right edges.
func (e Edges) Horizontal() Tick { return e.Left + e.Right }

// Vertical returns the total height consumed by the top and bottom edges.
func (e Edges) Vertical() Tick { return e.Top + e.Bottom }

// IsZero reports whether all four edges are zero.
func (e Edges) IsZero() bool { return e == Edges{} }

// Max returns the largest of the four edge values, used when a single
// representative width is needed (for example, choosing a clip inset).
func (e Edges) Max() Tick { return MaxOf(MaxOf(e.Top, e.Right), MaxOf(e.Bottom, e.Left)) }

// Uniform reports whether all four edges are equal, which lets the renderer
// take the cheap single-rect path instead of stroking four separate sides.
func (e Edges) Uniform() bool {
	return e.Top == e.Right && e.Right == e.Bottom && e.Bottom == e.Left
}
