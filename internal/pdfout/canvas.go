// The render.Canvas implementation: one Canvas per page.
//
// Everything arriving here is in ticks, top-left origin, y-down. It leaves as
// gopdf calls in points. gopdf's drawing API is itself top-left origin — it
// writes "pageHeight - y" into the content stream — so this file performs no
// y-flip of its own; see the note on toPDFPoint in path.go.
//
// Where gopdf cannot express what render.Canvas asks for, the compromise is
// commented at the call site rather than hidden. The three that matter:
// an open stroked subpath becomes consecutive segments (no joins), curves are
// flattened, and a batch of DrawLine calls becomes one pen setup followed by
// N stroke operators rather than one path and one operator.
package pdfout

import (
	"errors"

	"github.com/signintech/gopdf"

	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/paint"
	"github.com/jclement/treekillbot/internal/render"
)

var _ render.Canvas = (*Canvas)(nil)

// minStrokeWidth is the thinnest line this renderer will emit.
//
// PDF's "0 w" means "the thinnest line the device can draw", so its weight
// becomes a property of the printer rather than of the document — one device
// pixel, which is 0.24pt on a 300dpi laser and 0.03pt on an imagesetter.
// DESIGN.md D10 refuses anything below a quarter point for the same reason.
const minStrokeWidth = geom.TicksPerPt / 4

// errUnbalancedRestore means a Restore arrived with no matching Save. It is a
// painting bug rather than a document problem, so it is latched on the
// Document and surfaces at Bytes rather than being silently ignored — an
// unbalanced graphics state stack corrupts every clip after it.
var errUnbalancedRestore = errors.New("pdfout: Restore without a matching Save")

// Canvas paints one page. It is not safe for concurrent use.
type Canvas struct {
	doc  *Document
	pdf  *gopdf.GoPdf
	size PageSize

	path  pathBuilder
	state canvasState
	stack []canvasState

	// pending holds DrawLine endpoints, four ticks per segment, until
	// FlushLines commits them.
	pending []geom.Tick
}

// canvasState is the part of the graphics state this package tracks.
//
// Both the requested values and the ones actually emitted are kept, so that
// setting the same pen twice costs nothing in the content stream. The whole
// struct is pushed on Save and restored on Restore, which is correct because
// PDF's Q operator reverts colour, width and dash to exactly what they were at
// the matching q.
type canvasState struct {
	pen  render.Stroke
	fill paint.Color

	appliedPen  render.Stroke
	penApplied  bool
	appliedFill paint.Color
	fillApplied bool

	// alphaApplied is the alpha currently installed as a gopdf transparency.
	alphaApplied float64
}

func newCanvas(doc *Document, size PageSize) *Canvas {
	return &Canvas{
		doc:  doc,
		pdf:  doc.pdf,
		size: size,
		state: canvasState{
			pen:          render.Stroke{Color: paint.Black, Width: geom.TicksPerPt / 4},
			fill:         paint.Black,
			alphaApplied: 1,
		},
	}
}

// Size returns the page's media box, for callers that need to lay out against
// it after the page has been created.
func (c *Canvas) Size() PageSize { return c.size }

// ---- Graphics state ----

func (c *Canvas) Save() {
	c.FlushLines()
	c.stack = append(c.stack, c.state)
	c.pdf.SaveGraphicsState()
}

func (c *Canvas) Restore() {
	c.FlushLines()
	if len(c.stack) == 0 {
		c.doc.fail(errUnbalancedRestore)
		return
	}
	c.state = c.stack[len(c.stack)-1]
	c.stack = c.stack[:len(c.stack)-1]
	c.pdf.RestoreGraphicsState()
}

// SetStroke installs the pen. Any lines batched under the previous pen are
// committed first — changing the pen mid-batch would otherwise repaint the
// whole ruled panel at the new weight.
//
// Installing the pen that is already in force does nothing at all, flush
// included. That is not an optimisation: the natural way to draw a ruled panel
// is a loop that sets its pen and adds a rule, and if that broke the batch on
// every iteration there would be no batch.
func (c *Canvas) SetStroke(pen render.Stroke) {
	if sameStroke(c.state.pen, pen) {
		return
	}
	c.FlushLines()
	c.state.pen = pen
}

// SetFill installs the fill colour for subsequent Fill and FillStroke calls.
func (c *Canvas) SetFill(color paint.Color) {
	c.state.fill = color
}

// ---- Path construction ----

func (c *Canvas) MoveTo(x, y geom.Tick) { c.path.moveTo(x, y) }

func (c *Canvas) LineTo(x, y geom.Tick) { c.path.lineTo(x, y) }

func (c *Canvas) CurveTo(c1x, c1y, c2x, c2y, x, y geom.Tick) {
	c.path.curveTo(c1x, c1y, c2x, c2y, x, y)
}

func (c *Canvas) ClosePath() { c.path.close() }

// AddRect appends a rectangle whose outer silhouette is exactly r. See
// pathBuilder.addRect for the corner construction.
func (c *Canvas) AddRect(r geom.Rect, radius geom.Tick) { c.path.addRect(r, radius) }

// ---- Painting ----

// Stroke paints the current path's outline.
//
// Closed subpaths become one gopdf polygon, which is a single path with proper
// joins. Open ones become consecutive segments, because gopdf's polygon always
// closes the path and there is no operator-level path builder to reach for; on
// an open polyline that means butt ends at every vertex instead of a mitre.
// Nothing in the shipped decorations draws an open multi-segment stroke.
func (c *Canvas) Stroke() {
	c.FlushLines()
	subpaths := c.takePath()
	if !c.state.pen.IsVisible() {
		return
	}
	c.applyPen()
	for _, sp := range subpaths {
		if sp.closed {
			c.pdf.Polygon(sp.points, "D")
			continue
		}
		c.strokePolyline(sp.points)
	}
}

// Fill paints the current path's interior with the fill colour.
func (c *Canvas) Fill() {
	c.FlushLines()
	subpaths := c.takePath()
	if c.state.fill.IsInvisible() {
		return
	}
	c.applyFill()
	for _, sp := range subpaths {
		c.pdf.Polygon(sp.points, "F")
	}
}

// FillStroke paints interior and outline from the same path, so a panel's
// background and its border share one silhouette and no sliver of the page
// shows between them.
func (c *Canvas) FillStroke() {
	c.FlushLines()
	subpaths := c.takePath()
	visiblePen := c.state.pen.IsVisible()
	visibleFill := !c.state.fill.IsInvisible()
	if !visiblePen && !visibleFill {
		return
	}
	if visibleFill {
		c.applyFill()
	}
	if visiblePen {
		c.applyPen()
	}
	for _, sp := range subpaths {
		switch {
		case visibleFill && visiblePen && sp.closed:
			c.pdf.Polygon(sp.points, "FD")
		case visibleFill && sp.closed:
			c.pdf.Polygon(sp.points, "F")
		case visibleFill:
			c.pdf.Polygon(sp.points, "F")
			if visiblePen {
				c.strokePolyline(sp.points)
			}
		case sp.closed:
			c.pdf.Polygon(sp.points, "D")
		default:
			c.strokePolyline(sp.points)
		}
	}
}

// Clip narrows the clip region to the current path until the matching Restore.
//
// gopdf offers only a polygon clip, which closes each subpath and intersects
// it with what is already in force. A path of several subpaths therefore
// intersects them successively rather than unioning them under the non-zero
// winding rule that render.Canvas specifies. Every clip in this tool is a
// single rectangle, where the two agree.
func (c *Canvas) Clip() {
	c.FlushLines()
	for _, sp := range c.takePath() {
		c.pdf.ClipPolygon(sp.points)
	}
}

// takePath flattens and consumes the current path, which every painting
// operation does exactly once.
func (c *Canvas) takePath() []flatSubpath {
	subpaths := c.path.flatten()
	c.path.reset()
	return subpaths
}

// strokePolyline draws an open run of segments. Degenerate segments are
// skipped: gopdf has no SetLineCap, so every line has butt caps and a
// zero-length one deposits no ink at all.
func (c *Canvas) strokePolyline(points []gopdf.Point) {
	for i := 1; i < len(points); i++ {
		from, to := points[i-1], points[i]
		if from == to {
			continue
		}
		c.pdf.Line(from.X, from.Y, to.X, to.Y)
	}
}

// ---- Text ----

// DrawText places a run with its leftmost glyph origin on the baseline at
// (x, y).
//
// Tracking is emitted as PDF's Tc, which adds to the advance after every
// glyph including the last, where fonts.Face measures it between glyphs only
// (DESIGN.md D8). The two agree on where every glyph lands — Tc after the last
// glyph moves only the pen, and the pen position is never used, because each
// run sets its own origin — so the ink matches the measurement exactly.
func (c *Canvas) DrawText(x, y geom.Tick, run render.TextRun) {
	c.FlushLines()
	if run.Face == nil || run.Text == "" || run.Color.IsInvisible() {
		return
	}
	family, err := c.doc.useFont(run.Face)
	if err != nil {
		c.doc.fail(err)
		return
	}
	if err := c.pdf.SetFontWithStyle(family, gopdfStyle(run.Face.Style), float64(run.SizeQpt)/4); err != nil {
		c.doc.fail(err)
		return
	}
	if err := c.pdf.SetCharSpacing(run.Tracking.Points()); err != nil {
		c.doc.fail(err)
		return
	}
	c.applyAlpha(run.Color.Alpha)
	c.applyTextColor(run.Color)
	c.pdf.SetXY(x.Points(), y.Points())
	if err := c.pdf.Text(run.Text); err != nil {
		c.doc.fail(err)
	}
}

// ---- Batched lines ----

// DrawLine accumulates one line segment to be painted by FlushLines.
//
// A segment whose endpoints are identical is dropped rather than drawn: gopdf
// has no SetLineCap, so butt caps make a zero-length line invisible, and
// emitting one would put an operator in the file that provably paints nothing.
// Dots — dot grids, bullet decorations — are filled shapes, not degenerate
// lines.
func (c *Canvas) DrawLine(x1, y1, x2, y2 geom.Tick) {
	if x1 == x2 && y1 == y2 {
		return
	}
	c.pending = append(c.pending, x1, y1, x2, y2)
}

// FlushLines paints the accumulated segments with the current pen.
//
// render.Canvas exists to let an implementation collapse a ruled panel into
// one path and one stroke operator. gopdf cannot: its Line emits a complete
// q/m/l/S/Q block per call and there is no public way to append raw operators.
// What batching still buys is the pen setup — one width, colour and dash for
// the whole panel instead of one per rule — and the correct commit point, so
// that swapping in a backend that can emit a single path is a change to this
// function and nothing else.
func (c *Canvas) FlushLines() {
	if len(c.pending) == 0 {
		return
	}
	segments := c.pending
	c.pending = c.pending[:0]
	if !c.state.pen.IsVisible() {
		return
	}
	c.applyPen()
	for i := 0; i+3 < len(segments); i += 4 {
		c.pdf.Line(
			segments[i].Points(), segments[i+1].Points(),
			segments[i+2].Points(), segments[i+3].Points(),
		)
	}
}

// ---- Graphics state emission ----

// applyPen emits the width, stroke colour and dash pattern if they differ from
// what is already in force. The dash is always written, never left implicit:
// gopdf's dash state is sticky, so a solid rule after a dashed one inherits the
// dash unless it is reset (DESIGN.md D7).
func (c *Canvas) applyPen() {
	pen := c.state.pen
	if c.state.penApplied && sameStroke(c.state.appliedPen, pen) {
		return
	}
	c.applyAlpha(pen.Color.Alpha)
	c.pdf.SetLineWidth(clampStrokeWidth(pen.Width).Points())
	c.setStrokeColor(pen.Color)
	if len(pen.Dash) > 0 {
		c.pdf.SetCustomLineType(dashPoints(pen.Dash), pen.Phase.Points())
	} else {
		c.pdf.SetLineType("")
	}
	c.state.appliedPen = pen
	c.state.penApplied = true
}

func (c *Canvas) applyFill() {
	fill := c.state.fill
	if c.state.fillApplied && c.state.appliedFill == fill {
		return
	}
	c.applyAlpha(fill.Alpha)
	c.setFillColor(fill)
	c.state.appliedFill = fill
	c.state.fillApplied = true
}

// applyTextColor installs a text colour and is deliberately not cached.
//
// gopdf decides at Text() time whether to emit a colour operator inside the
// text object, from a mode flag that SetGrayFill and SetTextColor each set as
// a side effect. Skipping a redundant call would leave that flag describing
// some earlier run. Eight bytes per text run is the right price for not having
// to reason about it.
func (c *Canvas) applyTextColor(color paint.Color) {
	color = c.mapColor(color)
	switch color.Space {
	case paint.SpaceGray:
		// This is the only route by which grey text reaches the file as a
		// DeviceGray "g" operator: gopdf's gray text mode emits no colour of
		// its own inside BT/ET and lets the text inherit the fill. It also
		// changes the shape fill colour, so the cache has to know.
		c.pdf.SetGrayFill(color.Gray)
		c.state.appliedFill, c.state.fillApplied = color, true
	case paint.SpaceCMYK:
		c.pdf.SetTextColorCMYK(cmykByte(color.C), cmykByte(color.M), cmykByte(color.Y), cmykByte(color.K))
	default:
		r, g, b := color.ToRGB8()
		c.pdf.SetTextColor(r, g, b)
	}
}

// setStrokeColor and setFillColor emit a colour in the space it was authored
// in: gray(0.85) becomes DeviceGray 0.85 rather than an RGB triple a RIP would
// have to convert back. DESIGN.md D10 is the whole reason paint.Color carries
// a space at all.
func (c *Canvas) setStrokeColor(color paint.Color) {
	color = c.mapColor(color)
	switch color.Space {
	case paint.SpaceGray:
		c.pdf.SetGrayStroke(color.Gray)
	case paint.SpaceCMYK:
		c.pdf.SetStrokeColorCMYK(cmykByte(color.C), cmykByte(color.M), cmykByte(color.Y), cmykByte(color.K))
	default:
		r, g, b := color.ToRGB8()
		c.pdf.SetStrokeColor(r, g, b)
	}
}

func (c *Canvas) setFillColor(color paint.Color) {
	color = c.mapColor(color)
	switch color.Space {
	case paint.SpaceGray:
		c.pdf.SetGrayFill(color.Gray)
	case paint.SpaceCMYK:
		c.pdf.SetFillColorCMYK(cmykByte(color.C), cmykByte(color.M), cmykByte(color.Y), cmykByte(color.K))
	default:
		r, g, b := color.ToRGB8()
		c.pdf.SetFillColor(r, g, b)
	}
}

// applyAlpha installs or clears a transparency group. Alpha below 1 costs an
// extended graphics state object, which is why it is only touched when the
// value actually changes.
//
// PDF's extended graphics state carries one alpha for stroking and one for
// filling, but gopdf sets both from a single value, so a FillStroke whose fill
// and pen disagree about opacity gets whichever was applied last. Alpha is
// discouraged on a print path anyway (see paint.Color) and no shipped theme
// uses it.
func (c *Canvas) applyAlpha(alpha float64) {
	if alpha >= 1 {
		alpha = 1
	}
	if alpha == c.state.alphaApplied {
		return
	}
	c.state.alphaApplied = alpha
	if alpha >= 1 {
		c.pdf.ClearTransparency()
		return
	}
	if err := c.pdf.SetTransparency(gopdf.Transparency{Alpha: alpha, BlendModeType: gopdf.NormalBlendMode}); err != nil {
		c.doc.fail(err)
	}
}

// mapColor applies the document-wide --grayscale conversion, if any.
func (c *Canvas) mapColor(color paint.Color) paint.Color {
	if c.doc.opts.Grayscale {
		return color.Desaturate()
	}
	return color
}

// ---- Small conversions ----

// clampStrokeWidth refuses a stroke thinner than a quarter point. See
// minStrokeWidth for why "0 w" is not an option.
func clampStrokeWidth(w geom.Tick) geom.Tick {
	if w < minStrokeWidth {
		return minStrokeWidth
	}
	return w
}

// cmykByte converts a normalised channel to the 0-100 integer gopdf wants,
// which is the resolution its content writer emits anyway.
func cmykByte(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 100
	}
	return uint8(v*100 + 0.5)
}

func dashPoints(dash []geom.Tick) []float64 {
	out := make([]float64, len(dash))
	for i, d := range dash {
		out[i] = d.Points()
	}
	return out
}

// sameStroke compares two pens. Stroke contains a slice, so it is not
// comparable with ==, and getting this wrong shows up as a whole panel of
// rules drawn at the wrong weight.
//
// Cap participates even though gopdf has no SetLineCap and every line is drawn
// with butt caps: the field is part of the pen's identity, and comparing it
// keeps this honest if a backend that honours it ever arrives.
func sameStroke(a, b render.Stroke) bool {
	if a.Color != b.Color || a.Width != b.Width || a.Phase != b.Phase || a.Cap != b.Cap {
		return false
	}
	if len(a.Dash) != len(b.Dash) {
		return false
	}
	for i := range a.Dash {
		if a.Dash[i] != b.Dash[i] {
			return false
		}
	}
	return true
}
