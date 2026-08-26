// The two stroke alignment rules, as functions, so no caller has to remember
// which one applies.
//
// PDF centres a stroke on its path. That single fact is where every half-point
// bug on a form lives, and DESIGN.md D4 settles it once:
//
//   - Rule A, box borders, are EDGE-aligned. A border of width w declared on
//     (x, y, W, H) strokes along (x+w/2, y+w/2, W-w, H-w), so the stroke's
//     outer edge lands on the declared rect and the box still measures exactly
//     W by H. Border-box sizing means adding a border must not make the cell
//     102pt.
//   - Rule B, line decorations, are CENTRE-aligned. A rule at y covers
//     [y-w/2, y+w/2]. A writing rule *is* the line, so changing its weight
//     must not move it.
//
// These take a render.Canvas rather than a *Canvas: the same geometry has to
// hold for the PDF, for --emit-ops and for the debug overlay, and asserting it
// against recorded operations is the only way to test it directly.
package pdfout

import (
	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/paint"
	"github.com/jclement/treekillbot/internal/render"
)

// StrokeBorder draws a border whose OUTER edge lands exactly on r — Rule A.
//
// radius is the outer corner radius; the stroked path's radius is reduced by
// the same inset so that the silhouette's corners are the radius that was
// asked for.
func StrokeBorder(canvas render.Canvas, r geom.Rect, radius geom.Tick, pen render.Stroke) {
	if !pen.IsVisible() || r.IsEmpty() {
		return
	}
	canvas.SetStroke(pen)
	canvas.AddRect(borderPath(r, pen.Width), borderRadius(radius, pen.Width))
	canvas.Stroke()
}

// FillPanel paints a panel's background and its border from one path, so the
// two share a silhouette.
//
// Filling r and then stroking the Rule A path would leave the fill's corner
// poking out from under a rounded border, and on a straight edge would put a
// half-width sliver of background outside the stroke. Painting both from the
// inset path puts the fill's edge on the stroke's centreline, where the
// stroke's outer half covers it exactly out to r.
func FillPanel(canvas render.Canvas, r geom.Rect, radius geom.Tick, fill paint.Color, pen render.Stroke) {
	if r.IsEmpty() {
		return
	}
	visiblePen := pen.IsVisible()
	if fill.IsInvisible() && !visiblePen {
		return
	}
	canvas.SetFill(fill)
	if !visiblePen {
		canvas.AddRect(r, radius)
		canvas.Fill()
		return
	}
	canvas.SetStroke(pen)
	canvas.AddRect(borderPath(r, pen.Width), borderRadius(radius, pen.Width))
	canvas.FillStroke()
}

// StrokeRule draws a decoration rule centred on the given line — Rule B.
//
// Batched: the rule joins the current DrawLine batch and is painted by
// FlushLines, because a ruled panel emits hundreds of these and they all share
// one pen.
func StrokeRule(canvas render.Canvas, x1, y1, x2, y2 geom.Tick, pen render.Stroke) {
	if !pen.IsVisible() {
		return
	}
	canvas.SetStroke(pen)
	canvas.DrawLine(x1, y1, x2, y2)
}

// StrokeHorizontalRule is the common case of StrokeRule: a full-width rule
// across a rect at a given y, centred on it.
func StrokeHorizontalRule(canvas render.Canvas, r geom.Rect, y geom.Tick, pen render.Stroke) {
	StrokeRule(canvas, r.X, y, r.Right(), y, pen)
}

// borderPath returns the path a Rule A border of width w strokes along.
//
// The inset rounds half-widths UP. Widths are ticks, and an author writing
// "0.5pt" gets 8 of them, so the halving is exact for every weight anyone
// actually types; for an odd number of ticks the extra 1/32pt falls inside the
// box rather than outside it, because border-box sizing means the declared
// rect is a ceiling.
func borderPath(r geom.Rect, w geom.Tick) geom.Rect {
	return r.InsetUniform(halfWidth(w))
}

// borderRadius shrinks an outer corner radius to the stroked path's radius. A
// radius smaller than the inset collapses to a square corner, which is what a
// 1pt border on a 0.25pt radius should look like.
func borderRadius(radius, w geom.Tick) geom.Tick {
	inner := radius - halfWidth(w)
	if inner < 0 {
		return 0
	}
	return inner
}

func halfWidth(w geom.Tick) geom.Tick { return (w + 1) / 2 }
