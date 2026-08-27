// The --debug-layout overlay.
//
// This draws every node's four rectangles over the finished artwork in
// distinguishable colours. It earns its place for one reason above all: it lets
// you SEE whether a border sits inside its declared rect or straddles it, which
// is the single most common way a print layout goes subtly wrong and the
// hardest thing to diagnose from numbers alone.
package draw

import (
	"fmt"
	"strconv"

	"github.com/jclement/treekillbot/internal/decor"
	"github.com/jclement/treekillbot/internal/fonts"
	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/layout"
	"github.com/jclement/treekillbot/internal/paint"
	"github.com/jclement/treekillbot/internal/render"
	"github.com/jclement/treekillbot/internal/schema"
)

// The overlay palette. Fixed, saturated colours that no sensible document uses,
// so nothing on the page can be mistaken for the overlay.
var (
	debugContent = paint.RGB8(0xff, 0x00, 0xd0) // magenta: where children and text go
	debugPadding = paint.RGB8(0x00, 0xc8, 0xd0) // cyan: inside the border
	debugBorder  = paint.RGB8(0xff, 0x8a, 0x00) // orange: the declared box
	debugMargin  = paint.RGB8(0xd0, 0xc0, 0x00) // yellow: space claimed outside it
	debugBase    = paint.RGB8(0x00, 0xa0, 0x30) // green: text baselines
)

const debugStroke = geom.TicksPerPt / 4

// paintDebugOverlay draws the rectangle tree over the page.
func paintDebugOverlay(root *layout.Node, canvas render.Canvas) {
	root.Walk(func(n *layout.Node) bool {
		frame := n.Frame
		if frame.Border.W <= 0 || frame.Border.H <= 0 {
			return true
		}
		// Drawn outermost-first so the content box, which matters most, ends up
		// on top of the others where they coincide.
		outlineRect(canvas, frame.Margin, debugMargin, []geom.Tick{geom.Pt(1), geom.Pt(2)})
		outlineRect(canvas, frame.Border, debugBorder, nil)
		outlineRect(canvas, frame.Padding, debugPadding, []geom.Tick{geom.Pt(2), geom.Pt(2)})
		outlineRect(canvas, frame.Content, debugContent, nil)

		if wrapped := n.TextLayout(); wrapped != nil {
			for _, line := range wrapped.Lines {
				y := frame.Content.Y + line.Baseline
				canvas.SetStroke(render.Stroke{Color: debugBase, Width: debugStroke})
				canvas.MoveTo(frame.Content.X, y)
				canvas.LineTo(frame.Content.X+line.Width, y)
				canvas.Stroke()
			}
		}
		return true
	})
}

// outlineRect strokes a rectangle centred on its own edges, so the overlay
// shows where the boundary actually is rather than being inset like a real
// border would be.
func outlineRect(canvas render.Canvas, r geom.Rect, color paint.Color, dash []geom.Tick) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	canvas.SetStroke(render.Stroke{Color: color, Width: debugStroke, Dash: dash})
	canvas.AddRect(r, 0)
	canvas.Stroke()
}

// DumpLayout renders the computed rectangle tree as indented text.
//
// This, not the PDF, is the primary golden-file format. A diff here says what
// moved and by how much in the vocabulary of the design, and it does not churn
// when compression or PDF object ordering changes. It is also the fastest way
// to answer "why is that panel 3pt too short?" without opening a viewer.
func DumpLayout(root *layout.Node) string {
	var b []byte
	var walk func(n *layout.Node, depth int)
	walk = func(n *layout.Node, depth int) {
		for i := 0; i < depth; i++ {
			b = append(b, ' ', ' ')
		}
		b = append(b, n.Label()...)
		b = append(b, ' ')
		b = append(b, n.Frame.Border.String()...)
		if n.Frame.Content != n.Frame.Border {
			b = append(b, " content="...)
			b = append(b, n.Frame.Content.String()...)
		}
		if wrapped := n.TextLayout(); wrapped != nil && len(wrapped.Lines) > 0 {
			b = append(b, " lines="...)
			b = appendInt(b, len(wrapped.Lines))
			b = append(b, " size="...)
			b = appendInt(b, int(wrapped.SizeQpt/4))
			b = append(b, "pt"...)
		}
		b = append(b, '\n')
		for _, c := range n.Children {
			walk(c, depth+1)
		}
	}
	walk(root, 0)
	return string(b)
}

func appendInt(b []byte, v int) []byte {
	if v == 0 {
		return append(b, '0')
	}
	var tmp [20]byte
	i := len(tmp)
	neg := v < 0
	if neg {
		v = -v
	}
	for v > 0 {
		i--
		tmp[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		tmp[i] = '-'
	}
	return append(b, tmp[i:]...)
}

// titleFaceStyle resolves the weight and slant of a panel title.
func titleFaceStyle(n *layout.Node) fonts.Style {
	weight := 700
	if n.Props.Enum(schema.PTitleWeight, "bold") != "bold" {
		weight = 400
	}
	return fonts.StyleFor(weight, n.Props.Enum(schema.PFontStyle, "normal") == "italic")
}

// DumpInk renders the resolved drawing properties of every node as indented
// text: the second golden format, and the companion to DumpLayout.
//
// The two answer different questions and neither substitutes for the other.
// DumpLayout says where the boxes are; this says what ink lands in them. A
// cascade regression that leaves every rectangle exactly where it was — a rule
// drawn at the wrong weight, a checkbox that changed size, a heading that lost
// its pattern — moves nothing in the layout tree and is invisible to it.
//
// That is not hypothetical. `defaults panel { line-width: … }` once failed to
// reach the columns inside a panel, so a sheet ruled at two different weights;
// it was spotted by eye on a printout, having passed the whole suite.
//
// Only properties a node can actually use are printed, and only when they would
// put something on the page, so the file stays readable enough to diff by hand.
func DumpInk(root *layout.Node) string {
	var b []byte
	var walk func(n *layout.Node, depth int)
	walk = func(n *layout.Node, depth int) {
		for i := 0; i < depth; i++ {
			b = append(b, ' ', ' ')
		}
		b = append(b, n.Label()...)

		if w := n.Props.Tick(schema.PBorderWidth, 0); w > 0 {
			b = appendProp(b, "border", inkLen(w)+"/"+inkColor(n.Props.Color(schema.PBorderColor, paint.Black)))
			if r := n.Props.Tick(schema.PBorderRadius, 0); r > 0 {
				b = appendProp(b, "radius", inkLen(r))
			}
		}
		if bg := n.Props.Color(schema.PBackground, paint.Transparent); !bg.IsInvisible() {
			b = appendProp(b, "bg", inkColor(bg))
		}

		// A `rule` is a single stroke and carries no line-style, so the block
		// below would say nothing about it and a change in its weight would
		// diff clean.
		if n.Kind == layout.KindRule {
			b = appendProp(b, "stroke", inkLen(n.Props.Tick(schema.PLineWidth, 0))+
				"/"+inkColor(n.Props.Color(schema.PLineColor, paint.Black)))
		}

		if style := n.Props.Enum(schema.PLineStyle, "none"); style != "none" {
			b = appendProp(b, "rules", style+"@"+inkLen(n.Props.Tick(schema.PLinePitch, 0))+
				"/"+inkLen(n.Props.Tick(schema.PLineWidth, 0))+
				"/"+inkColor(n.Props.Color(schema.PLineColor, paint.Black)))
			if style == "checkbox" {
				b = appendProp(b, "box", inkLen(decor.CheckboxSide(
					n.Props.Tick(schema.PCheckboxSize, 0),
					n.Props.Tick(schema.PLinePitch, 0))))
			}
		}

		if n.Title != "" {
			b = appendProp(b, "title", inkLen(n.Props.Tick(schema.PTitleSize, 0))+
				"/"+inkColor(n.Props.Color(schema.PTitleColor, paint.Black))+
				"/"+n.Props.Enum(schema.PTitleStyle, "plain"))
			if p := n.Props.Enum(schema.PTitlePattern, "none"); p != "none" {
				b = appendProp(b, "pattern", p+"@"+inkLen(n.Props.Tick(schema.PPatternPitch, 0))+
					"/"+inkColor(n.Props.Color(schema.PPatternColor, paint.Black)))
			}
		}

		if wrapped := n.TextLayout(); wrapped != nil && len(wrapped.Lines) > 0 {
			b = appendProp(b, "text", inkLen(n.Props.Tick(schema.PFontSize, 0))+
				"/"+inkColor(n.Props.Color(schema.PColor, paint.Black))+
				"/"+n.Props.Enum(schema.PFontWeight, "normal"))
		}

		b = append(b, '\n')
		for _, c := range n.Children {
			walk(c, depth+1)
		}
	}
	walk(root, 0)
	return string(b)
}

func appendProp(b []byte, name, value string) []byte {
	b = append(b, ' ')
	b = append(b, name...)
	b = append(b, '=')
	return append(b, value...)
}

// inkLen and inkColor format a value for the ink dump. Both are deliberately
// exact and stable rather than pretty: this is a diff format, and a colour that
// prints as its source spelling would hide a theme swap that resolved to the
// same words but different ink.
func inkLen(t geom.Tick) string {
	return strconv.FormatFloat(t.Points(), 'f', 2, 64) + "pt"
}

func inkColor(c paint.Color) string {
	if c.IsInvisible() {
		return "none"
	}
	r, g, b := c.ToRGB8()
	out := fmt.Sprintf("#%02x%02x%02x", r, g, b)
	if c.Alpha < 1 {
		out += "@" + strconv.FormatFloat(c.Alpha, 'f', 2, 64)
	}
	return out
}
