// `dotted` — the bullet-journal dot grid.
package decor

import (
	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/render"
)

// dotted paints a lattice of dots, anchored to the page by default so that a
// spread of seven day boxes reads as one sheet of dot-grid paper.
type dotted struct{ params }

// Draw fills one shape per lattice point.
//
// The dots are FILLED, not stroked: the PDF backend has no SetLineCap, so a
// zero-length stroke with a butt cap deposits no ink at all and the page comes
// out blank (DESIGN.md D7). Every dot is appended to one path and painted with
// a single Fill, so a dense grid is one operator.
//
// Each dot is a square of side dot-size with a radius of half that, which is a
// circle whose outer silhouette is exactly dot-size across — the same
// silhouette contract AddRect gives a rounded box.
func (d *dotted) Draw(content geom.Rect, grid Grid, dst render.Canvas) {
	band := content.Inset(d.inset)
	if band.IsEmpty() || d.dotSize <= 0 || d.color.IsInvisible() {
		return
	}
	lat := d.lattice(band, grid)
	if lat.Pitch <= 0 {
		return
	}

	// Inset by the radius so no dot is clipped by the content edge.
	radius := d.dotSize / 2
	firstCol, lastCol := indices(lat.X, lat.Pitch, band.X+radius, band.Right()-radius)
	firstRow, lastRow := indices(lat.Y, lat.Pitch, band.Y+radius, band.Bottom()-radius)
	if lastCol < firstCol || lastRow < firstRow {
		return
	}

	dst.SetFill(d.color)
	for row := firstRow; row <= lastRow; row++ {
		y := lat.Y + geom.Tick(row)*lat.Pitch
		for col := firstCol; col <= lastCol; col++ {
			x := lat.X + geom.Tick(col)*lat.Pitch
			dst.AddRect(geom.Rect{X: x - radius, Y: y - radius, W: d.dotSize, H: d.dotSize}, radius)
		}
	}
	dst.Fill()
}

// Baselines is empty: a dot grid has rows, but nothing on it is a writing rule,
// and text in a dot-grid panel should flow normally rather than snap to dots.
func (d *dotted) Baselines(geom.Rect) []geom.Tick { return nil }

// NaturalHeight is 0: a dot grid fills whatever it is given.
func (d *dotted) NaturalHeight() geom.Tick { return 0 }
