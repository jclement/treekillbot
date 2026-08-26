// Package render defines the drawing surface the layout engine paints onto.
//
// Nothing above this interface knows what a PDF is. That separation buys three
// things: the layout engine can be tested without producing a document, the
// --debug-layout overlay and the --emit-ops structural golden files are just
// alternative Canvas implementations rather than special cases threaded through
// the painting code, and swapping the PDF library later touches one package.
//
// All coordinates crossing this interface are in the layout engine's space:
// ticks, top-left origin, y increasing downward. Implementations that need
// PDF's bottom-left origin flip at their own boundary and nowhere else.
package render

import (
	"github.com/jclement/treekillbot/internal/fonts"
	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/paint"
)

// LineCap and LineJoin mirror the PDF graphics state parameters.
type LineCap uint8

const (
	CapButt LineCap = iota
	CapRound
	CapSquare
)

// Stroke describes how a path outline is drawn. A zero Stroke draws nothing.
//
// Width is honoured exactly; the renderer refuses to emit a zero width, which
// in PDF means "thinnest line the device can draw" and so renders differently
// on a 300dpi laser and a 2400dpi imagesetter. A form that looks right on one
// printer and wrong on another is exactly what this tool exists to prevent.
type Stroke struct {
	Color paint.Color
	Width geom.Tick
	Dash  []geom.Tick // empty means solid
	Phase geom.Tick
	Cap   LineCap
}

// IsVisible reports whether stroking with this pen would deposit ink.
func (s Stroke) IsVisible() bool {
	return s.Width > 0 && !s.Color.IsInvisible()
}

// TextRun is a single run of text in one face at one size, drawn from a point
// on its baseline. Runs never wrap: the text layout stage has already decided
// where every line breaks and what each line contains.
type TextRun struct {
	Text     string
	Face     *fonts.Face
	SizeQpt  int32 // quarter-points, so 8.5pt is 34 and comparisons stay exact
	Color    paint.Color
	Tracking geom.Tick // extra advance between glyphs, applied n-1 times
}

// Width returns the run's advance width, delegating to the face so that the
// number used for alignment is produced by the same code that produced the
// number used for wrapping.
func (r TextRun) Width() geom.Tick {
	if r.Face == nil {
		return 0
	}
	return r.Face.Width(r.Text, r.SizeQpt, r.Tracking)
}

// Canvas is a stateful drawing surface with a path builder and a graphics
// state stack, modelled directly on PDF's content stream operators so that no
// implementation has to emulate a mismatched model.
//
// Path construction and painting are separate: build a path with MoveTo and
// friends or with AddRect, then call exactly one of Stroke, Fill, FillStroke or
// Clip, which consumes the path.
type Canvas interface {
	// Save and Restore push and pop the graphics state, including the clip
	// region. They must nest.
	Save()
	Restore()

	// SetStroke and SetFill install the pen and the fill colour for subsequent
	// painting operations.
	SetStroke(Stroke)
	SetFill(paint.Color)

	// Path construction. Coordinates are absolute, in layout space.
	MoveTo(x, y geom.Tick)
	LineTo(x, y geom.Tick)
	CurveTo(c1x, c1y, c2x, c2y, x, y geom.Tick)
	ClosePath()

	// AddRect appends a rectangle to the current path. A positive radius
	// produces a rounded rectangle whose OUTER silhouette matches r exactly.
	AddRect(r geom.Rect, radius geom.Tick)

	// Painting operations, each of which consumes the current path.
	Stroke()
	Fill()
	FillStroke()
	// Clip intersects the current path with the clip region using the non-zero
	// winding rule, then discards the path without painting it. The narrowed
	// clip lasts until the matching Restore.
	Clip()

	// DrawText places a run with its leftmost glyph origin on the baseline at
	// (x, y). The caller has already resolved alignment into x.
	DrawText(x, y geom.Tick, run TextRun)

	// DrawLine is the hot path — line decorations emit hundreds of these — and
	// exists so implementations can batch a whole ruled panel into one path and
	// one stroke operator rather than one per rule.
	DrawLine(x1, y1, x2, y2 geom.Tick)
	// FlushLines paints any lines accumulated by DrawLine with the current pen.
	FlushLines()
}

// Recorder is an optional Canvas capability. Implementations that can report
// the operations they received back them the --emit-ops structural golden
// files, which are the tier of test a human can actually read: a failure shows
// "y moved from 72.000 to 72.500" instead of "the hash changed".
type Recorder interface {
	Ops() []Op
}

// Op is one recorded drawing operation. Numeric fields are in points, rounded
// to four decimals, because that is lossless for tick-quantised values and
// keeps the golden files diffable.
type Op struct {
	Kind  string    `json:"op"`
	Args  []float64 `json:"args,omitempty"`
	Text  string    `json:"text,omitempty"`
	Font  string    `json:"font,omitempty"`
	Size  float64   `json:"size,omitempty"`
	Color string    `json:"color,omitempty"`
	Width float64   `json:"width,omitempty"`
	Dash  []float64 `json:"dash,omitempty"`
}
