// The --debug-layout overlay.
//
// This draws every node's four rectangles over the finished artwork in
// distinguishable colours. It earns its place for one reason above all: it lets
// you SEE whether a border sits inside its declared rect or straddles it, which
// is the single most common way a print layout goes subtly wrong and the
// hardest thing to diagnose from numbers alone.
package draw

import (
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
