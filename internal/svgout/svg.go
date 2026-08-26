// Package svgout renders a laid-out document to SVG.
//
// It exists so the browser preview can be a faithful recreation of the PDF
// rather than an approximation of it. Both are render.Canvas implementations
// driven by the same painting code over the same computed rectangles, so there
// is no second layout engine to drift: if the PDF moves a rule, the preview
// moves the same rule by the same amount.
//
// SVG was chosen over HTML boxes for exactly that reason. It has PDF's
// primitives — arbitrary paths, text positioned by its baseline, clipping — so
// the op stream maps across one-to-one. A CSS-box recreation would have to
// re-derive the layout from properties and would be wrong in a hundred small
// ways, which is the failure mode this design exists to avoid.
package svgout

import (
	"encoding/base64"
	"fmt"
	"sort"
	"strings"

	"github.com/jclement/treekillbot/internal/fonts"
	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/paint"
	"github.com/jclement/treekillbot/internal/render"
)

// Options configure an SVG document.
type Options struct {
	// EmbedFonts writes the faces used into the file as base64 @font-face
	// rules, making it standalone. The editor turns this off because its page
	// already carries the fonts and re-sending 2MB on every keystroke would
	// make typing feel broken.
	EmbedFonts bool
	// ClassPrefix namespaces generated ids so several SVGs can share a page.
	ClassPrefix string
	// Background fills the page. Transparent leaves it unpainted, which shows
	// the viewer's own background — rarely what you want for paper.
	Background paint.Color
}

// Canvas implements render.Canvas by emitting SVG.
type Canvas struct {
	body    strings.Builder
	width   geom.Tick
	height  geom.Tick
	options Options

	path  strings.Builder
	state graphicsState
	stack []graphicsState

	// openGroups counts the <g> elements a Save must close on Restore. Only a
	// clip opens one; colour state is written per-element instead, which keeps
	// the output flat and diffable.
	openGroups []int
	clipCount  int

	// lines accumulates DrawLine calls so a ruled panel becomes one path
	// element rather than several hundred.
	lines strings.Builder
	lineP graphicsState
	batch bool

	usedFaces map[string]*fonts.Face
}

// graphicsState is the pen and brush at one point in the stack.
type graphicsState struct {
	stroke render.Stroke
	fill   paint.Color
}

// New returns a Canvas for a page of the given size.
func New(width, height geom.Tick, options Options) *Canvas {
	return &Canvas{
		width:     width,
		height:    height,
		options:   options,
		usedFaces: map[string]*fonts.Face{},
	}
}

// ---- render.Canvas ----

func (c *Canvas) Save() {
	c.FlushLines()
	c.stack = append(c.stack, c.state)
	c.openGroups = append(c.openGroups, 0)
}

func (c *Canvas) Restore() {
	c.FlushLines()
	if n := len(c.openGroups); n > 0 {
		for i := 0; i < c.openGroups[n-1]; i++ {
			c.body.WriteString("</g>")
		}
		c.openGroups = c.openGroups[:n-1]
	}
	if n := len(c.stack); n > 0 {
		c.state = c.stack[n-1]
		c.stack = c.stack[:n-1]
	}
}

func (c *Canvas) SetStroke(s render.Stroke) {
	if c.batch && !sameStroke(s, c.lineP.stroke) {
		c.FlushLines()
	}
	c.state.stroke = s
}

func (c *Canvas) SetFill(color paint.Color) { c.state.fill = color }

func (c *Canvas) MoveTo(x, y geom.Tick) {
	fmt.Fprintf(&c.path, "M%s %s", num(x), num(y))
}

func (c *Canvas) LineTo(x, y geom.Tick) {
	fmt.Fprintf(&c.path, "L%s %s", num(x), num(y))
}

func (c *Canvas) CurveTo(c1x, c1y, c2x, c2y, x, y geom.Tick) {
	fmt.Fprintf(&c.path, "C%s %s %s %s %s %s", num(c1x), num(c1y), num(c2x), num(c2y), num(x), num(y))
}

func (c *Canvas) ClosePath() { c.path.WriteByte('Z') }

// AddRect appends a rectangle, rounded to a radius whose OUTER silhouette
// matches r exactly — the same guarantee the PDF backend makes, so the two
// agree at the corners.
func (c *Canvas) AddRect(r geom.Rect, radius geom.Tick) {
	if radius <= 0 {
		fmt.Fprintf(&c.path, "M%s %sH%sV%sH%sZ",
			num(r.X), num(r.Y), num(r.Right()), num(r.Bottom()), num(r.X))
		return
	}
	if radius*2 > r.W {
		radius = r.W / 2
	}
	if radius*2 > r.H {
		radius = r.H / 2
	}
	// Arcs rather than Béziers: SVG has a native elliptical arc and it is
	// exactly a quarter circle, so there is no flattening error to reconcile.
	fmt.Fprintf(&c.path,
		"M%s %sH%sA%s %s 0 0 1 %s %sV%sA%s %s 0 0 1 %s %sH%sA%s %s 0 0 1 %s %sV%sA%s %s 0 0 1 %s %sZ",
		num(r.X+radius), num(r.Y), num(r.Right()-radius),
		num(radius), num(radius), num(r.Right()), num(r.Y+radius),
		num(r.Bottom()-radius),
		num(radius), num(radius), num(r.Right()-radius), num(r.Bottom()),
		num(r.X+radius),
		num(radius), num(radius), num(r.X), num(r.Bottom()-radius),
		num(r.Y+radius),
		num(radius), num(radius), num(r.X+radius), num(r.Y))
}

func (c *Canvas) Stroke()     { c.emitPath(false, true) }
func (c *Canvas) Fill()       { c.emitPath(true, false) }
func (c *Canvas) FillStroke() { c.emitPath(true, true) }

// Clip narrows the drawing region to the current path until the matching
// Restore, mirroring PDF's model.
func (c *Canvas) Clip() {
	d := c.path.String()
	c.path.Reset()
	if d == "" {
		return
	}
	c.clipCount++
	id := c.options.ClassPrefix + "clip" + itoa(c.clipCount)
	fmt.Fprintf(&c.body, `<clipPath id="%s"><path d="%s"/></clipPath><g clip-path="url(#%s)">`, id, d, id)
	if n := len(c.openGroups); n > 0 {
		c.openGroups[n-1]++
	}
}

// DrawText places a run with its leftmost glyph origin on the baseline.
//
// The run is emitted with textLength set to the width the layout engine
// measured, and lengthAdjust="spacing" so the browser distributes any
// disagreement between its shaper and ours across the gaps rather than letting
// the line end somewhere else. That single attribute is what makes the preview
// line up with the PDF instead of merely resembling it.
func (c *Canvas) DrawText(x, y geom.Tick, run render.TextRun) {
	c.FlushLines()
	if run.Text == "" || run.Face == nil {
		return
	}
	c.usedFaces[faceKey(run.Face)] = run.Face

	size := float64(run.SizeQpt) / 4
	fmt.Fprintf(&c.body,
		`<text x="%s" y="%s" font-family="%s" font-size="%g" fill="%s"`,
		num(x), num(y), escapeAttr(cssFamily(run.Face)), size, cssColor(run.Color))
	if weight := run.Face.Style; weight == fonts.Bold || weight == fonts.BoldItalic {
		c.body.WriteString(` font-weight="700"`)
	}
	if style := run.Face.Style; style == fonts.Italic || style == fonts.BoldItalic {
		c.body.WriteString(` font-style="italic"`)
	}
	if width := run.Width(); width > 0 {
		fmt.Fprintf(&c.body, ` textLength="%s" lengthAdjust="spacing"`, num(width))
	}
	if run.Color.Alpha < 1 {
		fmt.Fprintf(&c.body, ` fill-opacity="%g"`, run.Color.Alpha)
	}
	c.body.WriteString(`>` + escapeText(run.Text) + `</text>`)
}

// DrawLine accumulates a segment into the current batch.
func (c *Canvas) DrawLine(x1, y1, x2, y2 geom.Tick) {
	if x1 == x2 && y1 == y2 {
		// A zero-length segment with butt caps draws nothing in PDF, so it must
		// draw nothing here too or the preview would show dots the print does
		// not have.
		return
	}
	if !c.batch {
		c.batch = true
		c.lineP = c.state
	}
	fmt.Fprintf(&c.lines, "M%s %sL%s %s", num(x1), num(y1), num(x2), num(y2))
}

// FlushLines paints the accumulated segments as one path.
func (c *Canvas) FlushLines() {
	if !c.batch {
		return
	}
	c.batch = false
	d := c.lines.String()
	c.lines.Reset()
	if d == "" || !c.lineP.stroke.IsVisible() {
		return
	}
	fmt.Fprintf(&c.body, `<path d="%s" fill="none"%s/>`, d, strokeAttrs(c.lineP.stroke))
}

// emitPath writes the current path with the requested paint operations.
func (c *Canvas) emitPath(fill, stroke bool) {
	c.FlushLines()
	d := c.path.String()
	c.path.Reset()
	if d == "" {
		return
	}
	fillAttr := `fill="none"`
	if fill && !c.state.fill.IsInvisible() {
		fillAttr = fmt.Sprintf(`fill="%s"`, cssColor(c.state.fill))
		if c.state.fill.Alpha < 1 {
			fillAttr += fmt.Sprintf(` fill-opacity="%g"`, c.state.fill.Alpha)
		}
	} else if fill {
		return // an invisible fill with no stroke is nothing at all
	}
	strokeAttr := ""
	if stroke && c.state.stroke.IsVisible() {
		strokeAttr = strokeAttrs(c.state.stroke)
	}
	fmt.Fprintf(&c.body, `<path d="%s" %s%s/>`, d, fillAttr, strokeAttr)
}

// String returns the finished SVG document.
func (c *Canvas) String() string {
	c.FlushLines()
	for len(c.openGroups) > 0 {
		c.Restore()
	}

	var out strings.Builder
	fmt.Fprintf(&out,
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %s %s" width="100%%" height="100%%" preserveAspectRatio="xMidYMid meet" shape-rendering="geometricPrecision" text-rendering="geometricPrecision">`,
		num(c.width), num(c.height))

	if c.options.EmbedFonts {
		out.WriteString(c.fontFaceStyle())
	}
	if !c.options.Background.IsInvisible() {
		fmt.Fprintf(&out, `<rect x="0" y="0" width="%s" height="%s" fill="%s"/>`,
			num(c.width), num(c.height), cssColor(c.options.Background))
	}
	out.WriteString(c.body.String())
	out.WriteString(`</svg>`)
	return out.String()
}

// Faces returns the faces the document drew with, sorted, so the page that
// hosts a preview can embed exactly those and no more.
func (c *Canvas) Faces() []*fonts.Face {
	keys := make([]string, 0, len(c.usedFaces))
	for key := range c.usedFaces {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]*fonts.Face, 0, len(keys))
	for _, key := range keys {
		out = append(out, c.usedFaces[key])
	}
	return out
}

// fontFaceStyle emits @font-face rules carrying the TTF bytes inline.
func (c *Canvas) fontFaceStyle() string {
	faces := c.Faces()
	if len(faces) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<style>`)
	for _, face := range faces {
		b.WriteString(FontFaceRule(face))
	}
	b.WriteString(`</style>`)
	return b.String()
}

// FontFaceRule renders one @font-face rule with the face's bytes base64-encoded.
//
// The TTF is sent as-is rather than converted to WOFF2: every browser that can
// render SVG can also load a bare TrueType face, and adding a compressor to a
// tool whose whole promise is "no external tools" would be a poor trade for
// bytes that only ever travel over loopback.
func FontFaceRule(face *fonts.Face) string {
	weight := "400"
	if face.Style == fonts.Bold || face.Style == fonts.BoldItalic {
		weight = "700"
	}
	style := "normal"
	if face.Style == fonts.Italic || face.Style == fonts.BoldItalic {
		style = "italic"
	}
	return fmt.Sprintf(
		`@font-face{font-family:"%s";font-weight:%s;font-style:%s;font-display:block;src:url(data:font/ttf;base64,%s) format("truetype");}`,
		cssFamily(face), weight, style, base64.StdEncoding.EncodeToString(face.Data))
}

// cssFamily returns the family name as CSS should see it. Style is carried by
// font-weight and font-style rather than by separate families, so that a
// browser falling back still picks something sensible.
func cssFamily(face *fonts.Face) string { return face.Name }

func faceKey(face *fonts.Face) string { return face.Name + "/" + face.Style.String() }

// strokeAttrs renders a pen as SVG attributes.
func strokeAttrs(s render.Stroke) string {
	var b strings.Builder
	fmt.Fprintf(&b, ` stroke="%s" stroke-width="%s"`, cssColor(s.Color), num(s.Width))
	if s.Color.Alpha < 1 {
		fmt.Fprintf(&b, ` stroke-opacity="%g"`, s.Color.Alpha)
	}
	if len(s.Dash) > 0 {
		parts := make([]string, len(s.Dash))
		for i, d := range s.Dash {
			parts[i] = num(d)
		}
		fmt.Fprintf(&b, ` stroke-dasharray="%s"`, strings.Join(parts, " "))
		if s.Phase != 0 {
			fmt.Fprintf(&b, ` stroke-dashoffset="%s"`, num(s.Phase))
		}
	}
	// PDF's default is a butt cap, and the backend cannot change it, so the
	// preview must match rather than take SVG's own default.
	b.WriteString(` stroke-linecap="butt"`)
	return b.String()
}

// cssColor renders a colour. Grey is emitted as an explicit rgb() triple rather
// than a keyword so that what the browser shows is the same number the PDF
// carries in DeviceGray.
func cssColor(c paint.Color) string {
	r, g, b := c.ToRGB8()
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

// num formats a length in points, trimmed. Two decimals matches what the PDF
// backend emits, so the two outputs quantise identically and a coordinate
// cannot differ between them.
func num(t geom.Tick) string {
	s := fmt.Sprintf("%.2f", t.Points())
	s = strings.TrimSuffix(strings.TrimRight(s, "0"), ".")
	if s == "" || s == "-0" {
		return "0"
	}
	return s
}

func itoa(v int) string { return fmt.Sprintf("%d", v) }

// escapeText escapes the three characters that matter inside an SVG text node.
func escapeText(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// escapeAttr escapes for an attribute value.
func escapeAttr(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

// sameStroke reports whether two pens are identical, which is what lets a run
// of rules share one path.
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
