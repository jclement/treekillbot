// Package decor draws the ruled lines, dot grids, graph squares, checkbox rows
// and time grids that live inside a panel. They are the reason the tool exists:
// everything else on the page is a box, and this is the ink a person writes on.
//
// A decoration is a pure function of geometry. It reads a resolved property set
// once, at construction, and thereafter depends on nothing but the rectangle it
// is handed and the page-global lattice — never on a sibling, never on what was
// drawn before it. That is what makes every one of them testable against a
// recording canvas with no fonts and no PDF.
//
// Two rules run through the whole package and are named again in each function
// that depends on them (DESIGN.md D4):
//
//   - Rule A — box borders are edge-aligned. A stroke of width w on a declared
//     rect runs along the rect inset by w/2, so the outer silhouette is the
//     declared size. Only the checkbox uses this.
//   - Rule B — line decorations are centre-aligned. A rule at y covers
//     [y-w/2, y+w/2]. A writing rule *is* the line; changing its weight must
//     not move it. Everything else here uses this.
//
// The package deliberately does not import internal/layout: layout sits above
// decor and asks it questions, so the dependency runs one way only.
package decor

import (
	"fmt"

	"github.com/jclement/treekillbot/internal/fonts"
	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/paint"
	"github.com/jclement/treekillbot/internal/render"
	"github.com/jclement/treekillbot/internal/schema"
)

// Grid is the page-global lattice that dot and graph decorations anchor to.
//
// Anchoring to the page rather than to each box is the difference between a
// sheet of dot-grid paper with boxes drawn on it and a collection of unrelated
// grids: adjacent panels show one continuous lattice, and the pair of rows at
// an arbitrary sub-pitch distance that the eye reads instantly as a mistake
// never appears. `grid-origin: box` is one word away for the isolated box where
// centring wins instead.
type Grid struct {
	// Origin is the rectangle the lattice is indexed from — normally the page's
	// content rect, so the first dot column sits on the left margin.
	Origin geom.Rect
	// Pitch is the document-wide lattice spacing, or 0 when the document has
	// not declared one and each decoration should use its own dot-pitch.
	Pitch geom.Tick
}

// FontResolver hands out faces by family and style.
//
// It is declared here rather than imported so that decor does not depend on
// layout; layout's own resolver satisfies it structurally.
type FontResolver interface {
	Resolve(family string, style fonts.Style) *fonts.Face
}

// Decoration is one line-style, resolved and ready to paint.
type Decoration interface {
	// Draw paints into content, which is the box's content rect. grid supplies
	// the page-global lattice for the styles that anchor to it.
	Draw(content geom.Rect, grid Grid, dst render.Canvas)

	// Baselines returns the y of every writing rule inside content, top-down.
	// Text asks for these rather than recomputing them: two computations of the
	// same number is how they end up different.
	Baselines(content geom.Rect) []geom.Tick

	// NaturalHeight is the height this decoration wants when its box is `auto`,
	// or 0 when it fills whatever it is given.
	NaturalHeight() geom.Tick
}

// New builds the decoration a node's resolved properties describe, wrapping it
// in the margin-rule modifier when one is asked for. An error means a property
// value the validator could not have caught on its own — a time-grid whose
// hours do not divide into whole slots is the only current case.
func New(p *schema.Props, resolver FontResolver) (Decoration, error) {
	if p == nil {
		return none{}, nil
	}
	prm := readParams(p, resolver)

	var (
		base Decoration
		err  error
	)
	switch prm.style {
	case "none":
		base = none{}
	case "ruled":
		base = &ruled{prm}
	case "dotted":
		base = &dotted{prm}
	case "graph":
		base = &graph{prm}
	case "checkbox":
		base = &checkbox{prm}
	case "cornell":
		base = &cornell{prm}
	case "time-grid":
		base, err = newTimeGrid(prm)
	default:
		return nil, fmt.Errorf("unknown line-style %q", prm.style)
	}
	if err != nil {
		return nil, err
	}
	if prm.marginRule {
		return &marginRule{params: prm, inner: base}, nil
	}
	return base, nil
}

// ---- Pens ----

// minStrokeTicks is the thinnest line we will draw. Below a quarter point an
// engine snaps the line to one device pixel, which makes its weight a property
// of the printer rather than of the document (DESIGN.md D10). Kept identical to
// layout's constant of the same name.
const minStrokeTicks = geom.TicksPerPt / 4

// pen returns a stroke of the given colour and width, floored at the thinnest
// weight a printer renders predictably.
func pen(color paint.Color, width geom.Tick) render.Stroke {
	if width > 0 && width < minStrokeTicks {
		width = minStrokeTicks
	}
	return render.Stroke{Color: color, Width: width}
}

// rulePen is the pen every writing rule is drawn with.
func (p *params) rulePen() render.Stroke { return pen(p.color, p.width) }

// ---- Integer lattice arithmetic ----
//
// Go's / truncates toward zero, which is not floor for a negative numerator —
// and a lattice index is routinely negative, because a panel above the page
// grid origin has negative offsets. Getting this wrong shifts one panel's dots
// by a whole pitch relative to its neighbour's, which is exactly the artefact
// the page-global lattice exists to prevent.

func floorDiv(a, b geom.Tick) int64 {
	q, r := int64(a)/int64(b), int64(a)%int64(b)
	if r != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

func ceilDiv(a, b geom.Tick) int64 {
	q, r := int64(a)/int64(b), int64(a)%int64(b)
	if r != 0 && (a < 0) == (b < 0) {
		q++
	}
	return q
}

// ---- none ----

// none draws nothing. It still answers the interface, so text placed in an
// undecorated panel falls back to normal flow line-boxing instead of the caller
// having to special-case a nil decoration.
type none struct{}

func (none) Draw(geom.Rect, Grid, render.Canvas) {}
func (none) Baselines(geom.Rect) []geom.Tick     { return nil }
func (none) NaturalHeight() geom.Tick            { return 0 }
