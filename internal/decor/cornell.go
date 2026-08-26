// `cornell` — the three-region Cornell note page: a cue column, a notes area,
// and a summary band across the foot.
package decor

import (
	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/render"
)

// summaryMinHeight and summaryMax(Num/Den) bound the summary band. Twenty per
// cent of a Letter page is a comfortable band; below about half an inch it is
// too short to write a sentence in, and above three tenths of the page it eats
// the notes area it is supposed to summarise.
const (
	summaryMinHeight             = geom.TicksPerPt * 36
	summaryMaxNum, summaryMaxDen = 3, 10
)

// cornell lays out the three regions and rules them.
type cornell struct{ params }

// bands is the geometry of one Cornell panel: the three regions plus the two
// divider positions. Computed in one place because Draw and Baselines must not
// disagree about where the summary starts.
type bands struct {
	notes      geom.Rect // cue column and notes area together
	summary    geom.Rect
	summaryTop geom.Tick
	cueX       geom.Tick
}

// layout derives the three regions from the content rect.
func (c *cornell) layout(band geom.Rect) bands {
	height, ok := c.cornellSummary.Resolve(band.H)
	if !ok {
		height = band.H.Scale(20, 100)
	}
	height = geom.Clamp(height, summaryMinHeight, band.H.Scale(summaryMaxNum, summaryMaxDen))
	if height > band.H {
		height = band.H
	}
	top := band.Bottom() - height
	return bands{
		notes:      geom.Rect{X: band.X, Y: band.Y, W: band.W, H: top - band.Y},
		summary:    geom.Rect{X: band.X, Y: top, W: band.W, H: height},
		summaryTop: top,
		cueX:       band.X + c.cornellCue,
	}
}

// Draw rules both bands and then lays the dividers over them.
//
// The critical detail is that the notes area and the cue column share ONE rule
// set: the rules are computed once against the whole notes band and drawn
// straight across it, so a line continues over the vertical divider instead of
// restarting a fraction of a millimetre off. Running the ruled decoration twice,
// once per sub-region, is how those two numbers end up different.
//
// The summary band runs its own centred pass. It is a separate optical unit and
// should sit squarely in its own band rather than continue the notes rhythm.
//
// Rule B throughout, dividers included: they are decoration lines, not borders.
func (c *cornell) Draw(content geom.Rect, _ Grid, dst render.Canvas) {
	band := content.Inset(c.inset)
	if band.IsEmpty() {
		return
	}
	b := c.layout(band)

	stroke := c.rulePen()
	if stroke.IsVisible() {
		dst.SetStroke(stroke)
		for _, y := range c.rules(b.notes) {
			dst.DrawLine(band.X, y, band.Right(), y)
		}
		for _, y := range c.rules(b.summary) {
			dst.DrawLine(band.X, y, band.Right(), y)
		}
		dst.FlushLines()
	}

	divider := pen(c.color, c.width.Scale(dividerWeightNum, dividerWeightDen))
	if !divider.IsVisible() {
		return
	}
	dst.SetStroke(divider)
	if b.notes.H > 0 && b.cueX > band.X && b.cueX < band.Right() {
		dst.DrawLine(b.cueX, band.Y, b.cueX, b.summaryTop)
	}
	if b.summary.H > 0 && b.summary.H < band.H {
		dst.DrawLine(band.X, b.summaryTop, band.Right(), b.summaryTop)
	}
	dst.FlushLines()
}

// Baselines returns the notes rules followed by the summary rules, top-down —
// the same two sets Draw paints, from the same call.
func (c *cornell) Baselines(content geom.Rect) []geom.Tick {
	band := content.Inset(c.inset)
	if band.IsEmpty() {
		return nil
	}
	b := c.layout(band)
	notes := c.rules(b.notes)
	summary := c.rules(b.summary)
	out := make([]geom.Tick, 0, len(notes)+len(summary))
	out = append(out, notes...)
	out = append(out, summary...)
	return c.baselinesFrom(out)
}

// NaturalHeight is 0: a Cornell page is defined by the box it is given, since
// its summary band is a proportion of that box.
func (c *cornell) NaturalHeight() geom.Tick { return 0 }
