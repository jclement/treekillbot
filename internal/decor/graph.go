// `graph` — squared engineering paper, with a heavier line every N squares.
package decor

import (
	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/render"
)

// graph paints a squared grid on the page-global lattice.
type graph struct{ params }

// Draw strokes the minor lines, then the major ones on top.
//
// Rule B: every line's coordinate is the centre of its stroke, so raising
// grid-major-width thickens the heavy lines symmetrically instead of nudging
// them off the lattice. Two batches, minors first, so the majors overprint
// cleanly at the intersections; each batch is one stroke operator.
//
// A line is major iff its index from the LATTICE ANCHOR is a multiple of
// grid-major — counted from the page, not from the panel, so the heavy lines
// line up across adjacent panels the same way the light ones do.
func (g *graph) Draw(content geom.Rect, grid Grid, dst render.Canvas) {
	band := content.Inset(g.inset)
	if band.IsEmpty() {
		return
	}
	lat := g.lattice(band, grid)
	if lat.Pitch <= 0 {
		return
	}

	firstCol, lastCol := indices(lat.X, lat.Pitch, band.X, band.Right())
	firstRow, lastRow := indices(lat.Y, lat.Pitch, band.Y, band.Bottom())

	for _, major := range []bool{false, true} {
		stroke := g.rulePen()
		if major {
			stroke = pen(g.color, g.majorWide)
		}
		if !stroke.IsVisible() {
			continue
		}
		drawn := false
		for col := firstCol; col <= lastCol; col++ {
			if isMajor(col, g.majorHint) != major {
				continue
			}
			x := lat.X + geom.Tick(col)*lat.Pitch
			if !drawn {
				dst.SetStroke(stroke)
				drawn = true
			}
			dst.DrawLine(x, band.Y, x, band.Bottom())
		}
		for row := firstRow; row <= lastRow; row++ {
			if isMajor(row, g.majorHint) != major {
				continue
			}
			y := lat.Y + geom.Tick(row)*lat.Pitch
			if !drawn {
				dst.SetStroke(stroke)
				drawn = true
			}
			dst.DrawLine(band.X, y, band.Right(), y)
		}
		if drawn {
			dst.FlushLines()
		}
	}
}

// Baselines is empty for the same reason as the dot grid's: a square is not a
// writing rule.
func (g *graph) Baselines(geom.Rect) []geom.Tick { return nil }

// NaturalHeight is 0: a graph grid fills whatever it is given.
func (g *graph) NaturalHeight() geom.Tick { return 0 }
