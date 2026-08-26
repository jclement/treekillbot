// The page-global lattice that `dotted` and `graph` index against.
//
// The whole argument for a page-global lattice is in the Grid doc comment; this
// file is the arithmetic that realises it. All of it is exact integer division
// on ticks, because the failure mode of getting it wrong is subtle and
// expensive: one panel's dots offset by a tick from its neighbour's is the kind
// of thing that survives review and is obvious only in print.
package decor

import "github.com/jclement/treekillbot/internal/geom"

// minLatticePitch is the tightest dot or graph spacing we will honour. Below a
// point a grid is a grey wash rather than a guide, and a page of them is a few
// million shapes — enough to make the PDF writer look hung. Clamping up is
// preferable to drawing nothing, which would look like a bug in the tool.
const minLatticePitch = geom.TicksPerPt

// lattice is a resolved grid: an anchor point that lattice index 0 sits on, and
// the spacing between indices.
type lattice struct {
	X, Y  geom.Tick
	Pitch geom.Tick
}

// resolve returns the lattice a dot or graph decoration should index against.
//
// Pitch order of preference: an explicit `dot-pitch`, then `line-pitch` — the
// schema promises that a square dot grid is one number — and only then the
// document's page grid. It is the page grid's ORIGIN that delivers cross-panel
// continuity; its pitch is a fallback, because line-pitch inherits and so
// adjacent panels already agree on it unless an author deliberately disagreed.
func (p *params) lattice(band geom.Rect, grid Grid) lattice {
	pitch := p.dotPitch
	if pitch <= 0 {
		pitch = p.pitch
	}
	if pitch <= 0 && !p.originBox {
		pitch = grid.Pitch
	}
	if pitch <= 0 {
		return lattice{}
	}
	if pitch < minLatticePitch {
		pitch = minLatticePitch
	}

	// `grid-origin: box` gives up cross-panel continuity, so the thing it is
	// bought for is centring: the leftover is split evenly and the lattice sits
	// squarely in the box.
	if p.originBox {
		return lattice{X: centredAnchor(band.X, band.W, pitch), Y: centredAnchor(band.Y, band.H, pitch), Pitch: pitch}
	}
	return lattice{X: grid.Origin.X, Y: grid.Origin.Y, Pitch: pitch}
}

// centredAnchor places index 0 so the lattice is centred within extent.
func centredAnchor(start, extent, pitch geom.Tick) geom.Tick {
	if extent <= 0 {
		return start
	}
	leftover := extent - geom.Tick(floorDiv(extent, pitch))*pitch
	return start + leftover/2
}

// indices returns the inclusive range of lattice indices whose coordinate falls
// within [lo, hi] along one axis, relative to the anchor.
func indices(anchor, pitch, lo, hi geom.Tick) (first, last int64) {
	if pitch <= 0 || hi < lo {
		return 0, -1
	}
	return ceilDiv(lo-anchor, pitch), floorDiv(hi-anchor, pitch)
}

// isMajor reports whether a lattice index falls on a major line, counting from
// the anchor rather than from the panel — which is the whole reason for a global
// origin, since it makes the heavy lines align across panels too.
func isMajor(index int64, every int) bool {
	if every <= 1 {
		return every == 1
	}
	return index%int64(every) == 0
}
