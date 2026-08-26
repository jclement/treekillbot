// Panel titles.
//
// A title is the one piece of chrome that changes a box's geometry: it takes a
// band out of the content area, and in the `notch` style it interrupts the
// border. Keeping all four styles' metrics in one file means the question "how
// much room does the title take?" has a single answer that layout and painting
// both use.
package draw

import (
	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/layout"
	"github.com/jclement/treekillbot/internal/paint"
	"github.com/jclement/treekillbot/internal/render"
	"github.com/jclement/treekillbot/internal/schema"
)

// titleInfo is a resolved title: its text, the band it occupies, and how it
// meets the border.
type titleInfo struct {
	text     string
	style    string // plain | bar | notch | underline
	position string // top | left | bottom | none
	align    string
	height   geom.Tick // band height for top/bottom
	width    geom.Tick // band width for left
	run      render.TextRun
	padding  geom.Edges
	baseline geom.Tick // from the top of the band
	textWide geom.Tick
}

// present reports whether there is a title to draw.
func (t titleInfo) present() bool { return t.text != "" && t.position != "none" }

// titleMetrics resolves a node's title and the space it claims.
func titleMetrics(n *layout.Node, env *Env) titleInfo {
	info := titleInfo{
		text:     n.Title,
		style:    n.Props.Enum(schema.PTitleStyle, "plain"),
		position: n.Props.Enum(schema.PTitlePosition, "top"),
		align:    n.Props.Enum(schema.PTitleAlign, "left"),
		padding:  n.Props.Edges(schema.PTitlePadding, geom.EdgesVH(geom.Pt(2), 0)),
	}
	if info.text == "" || info.position == "none" {
		return titleInfo{}
	}

	face := faceFor(env, titleFamily(n), titleFaceStyle(n))
	if face == nil {
		return titleInfo{}
	}
	size := titleSizeQpt(n)
	info.run = render.TextRun{
		Text:     info.text,
		Face:     face,
		SizeQpt:  size,
		Color:    colorOf(n.Props, schema.PTitleColor, paint.GrayN(0.35), env),
		Tracking: n.Props.Tick(schema.PTitleTracking, 0),
	}
	info.textWide = info.run.Width()

	// Geometry comes from the band layout already computed and reserved, so the
	// text cannot land anywhere other than the room set aside for it.
	band := n.TitleBand()
	info.height, info.width, info.baseline = band.Height, band.Width, band.Baseline
	return info
}

// paintTitleBar fills the band behind a `bar`-style title. It runs before the
// decoration so ruled lines cannot show through the bar.
func paintTitleBar(n *layout.Node, canvas render.Canvas, env *Env, title titleInfo, radius geom.Tick) {
	if !title.present() || title.style != "bar" {
		return
	}
	fill := colorOf(n.Props, schema.PTitleBackground, paint.GrayN(0.92), env)
	if fill.IsInvisible() {
		return
	}
	band := titleBand(n, title)
	canvas.SetFill(fill)
	// The bar shares the panel's top corners, so it uses the same radius there
	// and square corners where it meets the content.
	canvas.AddRect(band, geom.MinTick(radius, band.H/2))
	canvas.Fill()
}

// titleBand returns the rectangle the title occupies.
//
// The `notch` style is the exception: its title straddles the border rather
// than sitting inside the padding, so its band is centred on the top border
// line. Positioning it like the other styles put the text inside the padding
// while the knockout stayed on the border, so on any panel with `padding-top`
// the gap appeared where the text was not and the frame simply looked broken.
func titleBand(n *layout.Node, title titleInfo) geom.Rect {
	content := n.Frame.Content
	if title.style == "notch" && title.position == "top" {
		border := n.Frame.Border
		// Centre the band on the border stroke, and inset it horizontally by
		// the radius so the notch cannot land on a rounded corner.
		inset := geom.MaxOf(n.Props.Tick(schema.PBorderRadius, 0), geom.Pt(6))
		return geom.Rect{
			X: border.X + inset,
			Y: border.Y - title.height/2,
			W: border.W - inset*2,
			H: title.height,
		}
	}
	switch title.position {
	case "bottom":
		return geom.Rect{X: content.X, Y: content.Bottom() - title.height, W: content.W, H: title.height}
	case "left":
		return geom.Rect{X: content.X, Y: content.Y, W: title.width, H: content.H}
	default:
		return geom.Rect{X: content.X, Y: content.Y, W: content.W, H: title.height}
	}
}

// paintTitleText draws the title and, for the `underline` style, its rule.
//
// It runs last, after the border, so a title in the `notch` style sits on top
// of the frame rather than under it.
func paintTitleText(n *layout.Node, canvas render.Canvas, env *Env, title titleInfo) {
	if !title.present() {
		return
	}
	band := titleBand(n, title)

	if title.style == "notch" {
		paintNotch(n, canvas, env, title, band)
	}

	x := band.X + title.padding.Left
	switch title.align {
	case "center":
		x = band.X + (band.W-title.textWide)/2
	case "right":
		x = band.Right() - title.padding.Right - title.textWide
	}
	canvas.DrawText(x, band.Y+title.baseline, title.run)

	if title.style == "underline" {
		width := clampStroke(n.Props.Tick(schema.PBorderWidth, geom.Pt(0.5)))
		color := colorOf(n.Props, schema.PBorderColor, paint.GrayN(0.75), env)
		y := band.Bottom() - width/2
		canvas.SetStroke(render.Stroke{Color: color, Width: width})
		canvas.MoveTo(band.X, y)
		canvas.LineTo(band.Right(), y)
		canvas.Stroke()
	}
}

// paintNotch knocks a gap in the border where the title crosses it, the way a
// fieldset legend does.
//
// It is drawn by painting the page background over the border rather than by
// splitting the border into segments: the segment approach needs the border
// path to know about the title, which would couple two things that are
// otherwise independent, and the result is identical on an opaque page.
func paintNotch(n *layout.Node, canvas render.Canvas, env *Env, title titleInfo, band geom.Rect) {
	knockout := pageBackground(n, env)
	if knockout.IsInvisible() {
		return
	}
	const notchPaddingPt = 3
	pad := geom.Pt(notchPaddingPt)

	x := band.X + title.padding.Left - pad
	switch title.align {
	case "center":
		x = band.X + (band.W-title.textWide)/2 - pad
	case "right":
		x = band.Right() - title.padding.Right - title.textWide - pad
	}

	// The knockout is centred on the border stroke and made a little taller
	// than it, so a hairline border is reliably erased rather than left as a
	// faint remnant either side of the text.
	border := n.Frame.Border
	width := n.Props.Edges(schema.PBorderWidth, geom.Edges{}).Top
	height := geom.MaxOf(width*3, geom.Pt(2))
	gap := geom.Rect{
		X: x,
		Y: border.Y - height/2,
		W: title.textWide + pad*2,
		H: height,
	}
	canvas.SetFill(knockout)
	canvas.AddRect(gap, 0)
	canvas.Fill()
}

// pageBackground walks up to find the nearest opaque background, which is what
// a notch must be painted with.
func pageBackground(n *layout.Node, env *Env) paint.Color {
	for cur := n.Parent; cur != nil; cur = cur.Parent {
		if bg := colorOf(cur.Props, schema.PBackground, paint.Transparent, env); !bg.IsInvisible() {
			return bg
		}
	}
	return paint.White
}

// titleFamily returns the title's font family, falling back to the body font.
func titleFamily(n *layout.Node) string {
	if family := n.Props.Str(schema.PTitleFont, ""); family != "" {
		return family
	}
	return n.Props.Str(schema.PFont, "IBM Plex Mono")
}

// titleSizeQpt returns the title size in quarter-points.
func titleSizeQpt(n *layout.Node) int32 {
	size := n.Props.Tick(schema.PTitleSize, 0)
	if size <= 0 {
		size = n.Props.Tick(schema.PFontSize, geom.Pt(9))
	}
	q := int32((int64(size)*4 + int64(geom.TicksPerPt)/2) / int64(geom.TicksPerPt))
	if q < 1 {
		q = 1
	}
	return q
}
