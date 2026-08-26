// `ruled` — plain horizontal writing lines, the decoration everything else in
// this package is a variation on.
package decor

import (
	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/render"
)

// ruled draws one horizontal rule per writing row.
type ruled struct{ params }

// Draw strokes every rule as a single batched path.
//
// Rule B: each rule's y is the centre of its stroke. One DrawLine per rule and
// one FlushLines for the lot means a forty-line panel is one stroke operator
// rather than forty, so every rule is painted in an identical graphics state —
// a determinism win as much as a size one.
func (r *ruled) Draw(content geom.Rect, _ Grid, dst render.Canvas) {
	band := content.Inset(r.inset)
	drawRules(dst, r.rulePen(), r.rules(band), band.X, band.Right())
}

// drawRules strokes a set of horizontal rules between two x coordinates. Shared
// with checkbox and cornell so that the batching is written once.
func drawRules(dst render.Canvas, stroke render.Stroke, rules []geom.Tick, x1, x2 geom.Tick) {
	if len(rules) == 0 || !stroke.IsVisible() || x2 <= x1 {
		return
	}
	dst.SetStroke(stroke)
	for _, y := range rules {
		dst.DrawLine(x1, y, x2, y)
	}
	dst.FlushLines()
}

// Baselines returns the writing baseline of every row, which is the rule itself
// under the default `baseline-on-rule: true`.
func (r *ruled) Baselines(content geom.Rect) []geom.Tick {
	return r.baselinesFrom(r.rules(content.Inset(r.inset)))
}

// NaturalHeight is the space the requested number of rules occupies, or 0 when
// the panel should fill whatever height it is given.
func (r *ruled) NaturalHeight() geom.Tick { return r.rowNaturalHeight() }
