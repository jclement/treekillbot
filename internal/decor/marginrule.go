// `margin-rule` — the red line down the left of a school exercise book.
//
// Not a decoration in its own right but a modifier: it wraps whatever
// line-style the node asked for and adds one vertical line, so `ruled` plus a
// margin rule and `dotted` plus a margin rule are the same code path.
package decor

import (
	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/render"
)

// marginRule draws a vertical rule over another decoration.
type marginRule struct {
	params
	inner Decoration
}

// Draw paints the wrapped decoration and then the margin rule over it.
//
// Rule B: the rule's x is the centre of its stroke, so thickening it does not
// move the margin. It runs the full height of the content rect, which is the
// classic exercise-book look; the rules-only and page-height extents the layout
// spec also describes have no property to select them.
//
// The weight comes from line-width by the same ratio the Cornell dividers use,
// which lands on the spec's 0.6pt at the default 0.4pt line-width while still
// tracking an author who thickened their rules.
func (m *marginRule) Draw(content geom.Rect, grid Grid, dst render.Canvas) {
	m.inner.Draw(content, grid, dst)

	band := content.Inset(m.inset)
	if band.IsEmpty() {
		return
	}
	stroke := pen(m.marginRuleColor, m.width.Scale(dividerWeightNum, dividerWeightDen))
	if !stroke.IsVisible() {
		return
	}
	x := band.X + m.marginRuleOffset
	if x <= band.X || x >= band.Right() {
		return
	}
	dst.SetStroke(stroke)
	dst.DrawLine(x, band.Y, x, band.Bottom())
	dst.FlushLines()
}

// Baselines is the wrapped decoration's: a margin rule adds no writing rows.
func (m *marginRule) Baselines(content geom.Rect) []geom.Tick {
	return m.inner.Baselines(content)
}

// NaturalHeight is the wrapped decoration's, for the same reason.
func (m *marginRule) NaturalHeight() geom.Tick { return m.inner.NaturalHeight() }
