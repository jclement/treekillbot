// `checkbox` — a checklist row: a tick box, a gutter, and a writing rule.
package decor

import (
	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/render"
)

// checkbox draws one tick box per writing row, with an optional rule beside it.
//
// Its row geometry is the ruled decoration's, unchanged: a checkbox panel and a
// ruled panel of the same height and pitch put their rules in exactly the same
// places, so the two can sit side by side in adjacent columns and line up.
type checkbox struct{ params }

// Draw paints the trailing rules first and the boxes over them.
//
// Rule B for the rules; Rule A for the boxes. The box path is inset by half its
// stroke width so the outer silhouette is exactly size x size, which is what
// makes a column of boxes measure exactly one pitch between outer edges.
//
// The box does not centre on its row: it SITS on the baseline with a small
// optical overshoot below it, the way a well-set bullet or a checkbox glyph
// does. A box whose bottom edge is exactly on the baseline reads as floating
// high next to lowercase text, for the same reason a circle needs overshoot
// against a flat letter.
func (c *checkbox) Draw(content geom.Rect, _ Grid, dst render.Canvas) {
	band := content.Inset(c.inset)
	rules := c.rules(band)
	if len(rules) == 0 {
		return
	}

	size := c.checkboxSize
	if c.checkboxRule {
		ruleStart := band.X + size + c.checkboxGutter
		drawRules(dst, c.rulePen(), rules, ruleStart, band.Right())
	}

	stroke := pen(c.color, c.width.Scale(checkboxWeightNum, checkboxWeightDen))
	if size <= 0 || !stroke.IsVisible() {
		return
	}
	// The stroke sits inside the silhouette, so both the rect and its corner
	// radius shrink by half the pen width.
	half := stroke.Width / 2
	radius := c.checkboxRadius - half
	if radius < 0 {
		radius = 0
	}

	dst.SetStroke(stroke)
	sit := size.Scale(checkboxSitNum, checkboxSitDen)
	for _, baseline := range rules {
		bottom := baseline + sit
		box := geom.Rect{X: band.X, Y: bottom - size, W: size, H: size}
		dst.AddRect(box.InsetUniform(half), radius)
	}
	dst.Stroke()
}

// Baselines returns the same rows a ruled panel of this pitch would.
func (c *checkbox) Baselines(content geom.Rect) []geom.Tick {
	return c.baselinesFrom(c.rules(content.Inset(c.inset)))
}

// NaturalHeight is the space the requested number of rows occupies, or 0 to
// fill. Identical to ruled's, deliberately.
func (c *checkbox) NaturalHeight() geom.Tick { return c.rowNaturalHeight() }
