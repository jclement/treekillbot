// How much room a panel title takes.
//
// This lives in layout rather than in the painter because the title is not
// decoration: it occupies a band that the node's children must be placed below.
// Computing it at paint time, as this originally did, meant layout never
// reserved the space and every panel with both a title and children painted the
// two on top of each other.
//
// The band is computed once, during Arrange, and stored on the node. The painter
// reads it back rather than recomputing — two computations of the same number is
// how they end up different.
package layout

import (
	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/schema"
)

// TitleBand is the space a node's title occupies inside its content box.
type TitleBand struct {
	// Height is the band's extent for a top or bottom title, Width for a left
	// one. The unused one is zero.
	Height, Width geom.Tick
	// Baseline is the distance from the band's top edge to the title's baseline.
	Baseline geom.Tick
	Position string
}

// Reserved reports whether the band takes room from the content box.
//
// A `notch` title straddles the border rather than sitting inside the padding,
// so it reserves nothing — that is the whole point of the style, and reserving
// for it would leave a gap under a title that is not there.
func (b TitleBand) Reserved() bool {
	return b.Height > 0 || b.Width > 0
}

// TitleBand returns the band computed for this node during Arrange.
func (n *Node) TitleBand() TitleBand { return n.titleBand }

// measureTitle computes the band a node's title claims.
func measureTitle(n *Node, env *Env) TitleBand {
	if n.Title == "" {
		return TitleBand{}
	}
	position := n.Props.Enum(schema.PTitlePosition, "top")
	if position == "none" {
		return TitleBand{}
	}

	style := n.Props.Enum(schema.PTitleStyle, "plain")
	band := TitleBand{Position: position}

	st := titleStyleFor(n, env)
	if st.Face == nil {
		return TitleBand{}
	}
	padding := n.Props.Edges(schema.PTitlePadding, geom.EdgesVH(geom.Pt(2), 0))
	ascent, descent := st.Face.Ascent(st.SizeQpt), st.Face.Descent(st.SizeQpt)

	if position == "left" {
		band.Width = st.Face.Width(n.Title, st.SizeQpt, st.Tracking) + padding.Horizontal()
		band.Baseline = ascent
		return band
	}

	// A notch title sits on the border, not in the content box, so it claims no
	// room from the children.
	if style == "notch" {
		band.Baseline = padding.Top + ascent
		return band
	}

	band.Height = ascent + descent + padding.Vertical()
	band.Baseline = padding.Top + ascent
	return band
}

// contentAfterTitle returns the content box with the title band removed.
func contentAfterTitle(content geom.Rect, band TitleBand) geom.Rect {
	if !band.Reserved() {
		return content
	}
	switch band.Position {
	case "bottom":
		return geom.Rect{X: content.X, Y: content.Y, W: content.W, H: content.H - band.Height}
	case "left":
		return geom.Rect{X: content.X + band.Width, Y: content.Y, W: content.W - band.Width, H: content.H}
	default:
		return geom.Rect{X: content.X, Y: content.Y + band.Height, W: content.W, H: content.H - band.Height}
	}
}
