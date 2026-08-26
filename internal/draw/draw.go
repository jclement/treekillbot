// Painting a laid-out tree onto a canvas.
//
// Everything here works from Frame rectangles that layout has already decided,
// so this file makes no sizing decisions at all — if something is in the wrong
// place, the bug is upstream. What it does own is paint order, the two stroke
// alignment rules, and the panel chrome.
//
// Paint order per node: background, then the title bar's fill, then the line
// decoration (clipped to the content box), then children, then the border, and
// finally the title text. The border comes after children deliberately: an
// overflowing child would otherwise paint over the frame that contains it, and
// a form whose borders are interrupted looks broken in a way that a form whose
// content is clipped does not.
package draw

import (
	"github.com/jclement/treekillbot/internal/fonts"
	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/layout"
	"github.com/jclement/treekillbot/internal/paint"
	"github.com/jclement/treekillbot/internal/render"
	"github.com/jclement/treekillbot/internal/schema"
)

// Decorator draws a line decoration into a content rectangle. It is an
// interface here so that the decoration package sits beside this one rather
// than beneath it, and so a canvas-level test can substitute a stub.
type Decorator interface {
	Draw(content geom.Rect, canvas render.Canvas)
	Baselines(content geom.Rect) []geom.Tick
}

// DecorFactory builds a decoration for a node's properties, returning nil when
// the node has none.
type DecorFactory func(props *schema.Props, content geom.Rect) Decorator

// Env is what painting needs beyond the tree.
type Env struct {
	Fonts     layout.FontResolver
	Decor     DecorFactory
	Grayscale bool
	// DebugLayout draws every box's rectangles over the artwork.
	DebugLayout bool

	// collapsed records borders another box already strokes. Filled by Paint;
	// nil when nothing on the page shares an edge.
	collapsed suppressed
}

// minStroke is the thinnest line that will be emitted. Below a quarter point a
// printer snaps a stroke to one device pixel, which makes its weight a property
// of the printer rather than of the document.
const minStroke = geom.TicksPerPt / 4

// Paint draws a whole page.
func Paint(root *layout.Node, canvas render.Canvas, env *Env) {
	// Shared edges are resolved across the whole page before anything is drawn,
	// because the boxes that touch are often not siblings and no single node
	// can see the pairing from where it sits.
	env.collapsed = collapseBorders(root, env)
	paintNode(root, canvas, env)
	if env.DebugLayout {
		paintDebugOverlay(root, canvas)
	}
}

func paintNode(n *layout.Node, canvas render.Canvas, env *Env) {
	frame := n.Frame
	if frame.Border.W <= 0 || frame.Border.H <= 0 {
		return
	}

	radius := n.Props.Tick(schema.PBorderRadius, 0)
	title := titleMetrics(n, env)

	paintBackground(n, canvas, env, radius)
	paintTitleBar(n, canvas, env, title, radius)
	paintDecoration(n, canvas, env, title)

	switch n.Kind {
	case layout.KindText:
		paintText(n, canvas, env)
	case layout.KindRule:
		paintRule(n, canvas, env)
	}

	for _, child := range n.Children {
		paintNode(child, canvas, env)
	}

	paintBorder(n, canvas, env, radius, title)
	paintTitleText(n, canvas, env, title)
}

// paintBackground fills the border box.
func paintBackground(n *layout.Node, canvas render.Canvas, env *Env, radius geom.Tick) {
	bg := colorOf(n.Props, schema.PBackground, paint.Transparent, env)
	if bg.IsInvisible() {
		return
	}
	canvas.SetFill(bg)
	canvas.AddRect(n.Frame.Border, radius)
	canvas.Fill()
}

// paintBorder strokes the border, following Rule A: the stroke path is inset by
// half the width so the stroke's OUTER edge lands on the declared rectangle and
// the box still measures exactly what it says it does.
func paintBorder(n *layout.Node, canvas render.Canvas, env *Env, radius geom.Tick, title titleInfo) {
	width := n.Props.Edges(schema.PBorderWidth, geom.Edges{})
	style := n.Props.Enum(schema.PBorderStyle, "solid")
	if style == "none" || width.Max() <= 0 {
		return
	}
	color := colorOf(n.Props, schema.PBorderColor, paint.GrayN(0.75), env)
	if color.IsInvisible() {
		return
	}

	skip := env.collapsed[n]
	if width.Uniform() && !skip.left && !skip.top {
		w := clampStroke(width.Top)
		canvas.SetStroke(strokeFor(color, w, style))
		canvas.AddRect(n.Frame.Border.InsetUniform(w/2), adjustRadius(radius, w))
		canvas.Stroke()
		return
	}
	if skip.left {
		width.Left = 0
	}
	if skip.top {
		width.Top = 0
	}
	paintPerSideBorder(n.Frame.Border, width, color, style, canvas)
}

// paintPerSideBorder strokes each side separately when the widths differ.
// Each side is offset inward by half its own width, so every edge's outer face
// still lands on the declared rectangle.
func paintPerSideBorder(r geom.Rect, width geom.Edges, color paint.Color, style string, canvas render.Canvas) {
	sides := []struct {
		w              geom.Tick
		x1, y1, x2, y2 geom.Tick
	}{
		{width.Top, r.X, r.Y + width.Top/2, r.Right(), r.Y + width.Top/2},
		{width.Bottom, r.X, r.Bottom() - width.Bottom/2, r.Right(), r.Bottom() - width.Bottom/2},
		{width.Left, r.X + width.Left/2, r.Y, r.X + width.Left/2, r.Bottom()},
		{width.Right, r.Right() - width.Right/2, r.Y, r.Right() - width.Right/2, r.Bottom()},
	}
	for _, side := range sides {
		if side.w <= 0 {
			continue
		}
		canvas.SetStroke(strokeFor(color, clampStroke(side.w), style))
		canvas.MoveTo(side.x1, side.y1)
		canvas.LineTo(side.x2, side.y2)
		canvas.Stroke()
	}
}

// paintDecoration draws the line decoration, clipped to the content box so a
// dot lattice anchored to the page cannot leak past the panel that shows it.
func paintDecoration(n *layout.Node, canvas render.Canvas, env *Env, title titleInfo) {
	if env.Decor == nil {
		return
	}
	content := decorationArea(n, title)
	if content.IsEmpty() {
		return
	}
	decorator := env.Decor(n.Props, content)
	if decorator == nil {
		return
	}
	canvas.Save()
	canvas.AddRect(content, 0)
	canvas.Clip()
	decorator.Draw(content, canvas)
	canvas.FlushLines()
	canvas.Restore()
}

// decorationArea returns the rectangle a decoration fills: the content box,
// less whatever the title took.
//
// It deliberately does NOT apply line-inset. The decoration applies that itself,
// and doing it here as well would inset twice — a `line-inset: 9pt` panel would
// silently lose 18pt. Leaving the clip rect un-inset is also the better choice
// on its own terms: the inset is about where rules are drawn, not about what the
// panel is allowed to paint.
func decorationArea(n *layout.Node, title titleInfo) geom.Rect {
	area := n.Frame.Content
	if title.height > 0 {
		switch title.position {
		case "top":
			area.Y += title.height
			area.H -= title.height
		case "bottom":
			area.H -= title.height
		case "left":
			area.X += title.width
			area.W -= title.width
		}
	}
	return area
}

// paintText draws a text node's wrapped lines.
//
// Text that overflows its box is clipped to it rather than painted past the
// border and off the page. Without this, `overflow: clip` clipped the box's
// decorations and let its text escape, which is the opposite of what the
// property says.
func paintText(n *layout.Node, canvas render.Canvas, env *Env) {
	wrapped := n.TextLayout()
	if wrapped == nil || len(wrapped.Lines) == 0 || wrapped.Face == nil {
		return
	}
	content := n.Frame.Content

	if wrapped.Clipped || n.Props.Enum(schema.POverflow, "error") == "clip" {
		canvas.Save()
		defer canvas.Restore()
		// A little slack below the last baseline so descenders are not shaved
		// off a line that does fit.
		clip := content
		clip.H += wrapped.LineHeight / 4
		canvas.AddRect(clip, 0)
		canvas.Clip()
	}
	color := colorOf(n.Props, schema.PColor, paint.Black, env)
	align := n.Props.Enum(schema.PAlign, "left")
	shift := layout.VAlignOffset(n.Props.Enum(schema.PValign, "top"), wrapped.Height, content.H)

	for _, line := range wrapped.Lines {
		if line.Text == "" {
			continue
		}
		run := render.TextRun{
			Text:     line.Text,
			Face:     wrapped.Face,
			SizeQpt:  wrapped.SizeQpt,
			Color:    color,
			Tracking: wrapped.Tracking,
		}
		x := content.X + layout.AlignOffset(align, line.Width, content.W)
		if line.Justify {
			paintJustifiedLine(canvas, run, content, line, shift)
			continue
		}
		canvas.DrawText(x, content.Y+shift+line.Baseline, run)
	}
}

// paintJustifiedLine spreads a line's words to the full measure, distributing
// the slack exactly so the last word lands on the right margin rather than a
// rounding error away from it.
func paintJustifiedLine(canvas render.Canvas, run render.TextRun, content geom.Rect, line layout.Line, shift geom.Tick) {
	words := splitWords(line.Text)
	if len(words) < 2 {
		canvas.DrawText(content.X, content.Y+shift+line.Baseline, run)
		return
	}

	var wordsWidth geom.Tick
	widths := make([]geom.Tick, len(words))
	for i, word := range words {
		widths[i] = run.Face.Width(word, run.SizeQpt, run.Tracking)
		wordsWidth += widths[i]
	}
	slack := content.W - wordsWidth
	if slack < 0 {
		canvas.DrawText(content.X, content.Y+shift+line.Baseline, run)
		return
	}
	gaps := geom.DistributeEqual(slack, len(words)-1)

	x := content.X
	y := content.Y + shift + line.Baseline
	for i, word := range words {
		wordRun := run
		wordRun.Text = word
		canvas.DrawText(x, y, wordRun)
		x += widths[i]
		if i < len(gaps) {
			x += gaps[i]
		}
	}
}

// paintRule draws a horizontal line across the content box, following Rule B:
// the line is centred on its path, so changing its weight does not move it.
func paintRule(n *layout.Node, canvas render.Canvas, env *Env) {
	content := n.Frame.Content
	width := clampStroke(n.Props.Tick(schema.PLineWidth, geom.Pt(0.4)))
	color := colorOf(n.Props, schema.PLineColor, paint.GrayN(0.75), env)
	if color.IsInvisible() {
		return
	}
	y := content.Y + width/2
	canvas.SetStroke(strokeFor(color, width, n.Props.Enum(schema.PBorderStyle, "solid")))
	canvas.MoveTo(content.X, y)
	canvas.LineTo(content.Right(), y)
	canvas.Stroke()
}

// colorOf reads a colour property, applying the grayscale conversion when this
// particular print run asked for it. Grayscale is a flag rather than a theme
// because it is a property of the run, not of the document.
func colorOf(props *schema.Props, id schema.PropID, fallback paint.Color, env *Env) paint.Color {
	c := props.Color(id, fallback)
	if env != nil && env.Grayscale {
		return c.Desaturate()
	}
	return c
}

// clampStroke refuses a zero-width stroke, whose rendered weight would depend
// on the output device.
func clampStroke(w geom.Tick) geom.Tick {
	if w <= 0 {
		return 0
	}
	if w < minStroke {
		return minStroke
	}
	return w
}

// strokeFor builds a pen, expanding the dashed and dotted styles into patterns
// proportional to the line weight so a hairline dash does not look like a solid
// line and a heavy one does not look like a row of bricks.
func strokeFor(color paint.Color, width geom.Tick, style string) render.Stroke {
	s := render.Stroke{Color: color, Width: width}
	switch style {
	case "dashed":
		s.Dash = []geom.Tick{width * 3, width * 3}
	case "dotted":
		s.Dash = []geom.Tick{width, width * 2}
	}
	return s
}

// adjustRadius shrinks a corner radius to match a stroke path that has been
// inset by half the border width, so the stroked corner stays concentric with
// the filled one.
func adjustRadius(radius, width geom.Tick) geom.Tick {
	if radius <= 0 {
		return 0
	}
	if r := radius - width/2; r > 0 {
		return r
	}
	return 0
}

func splitWords(text string) []string {
	var out []string
	start := -1
	for i := 0; i < len(text); i++ {
		if text[i] == ' ' || text[i] == '\t' {
			if start >= 0 {
				out = append(out, text[start:i])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, text[start:])
	}
	return out
}

// faceFor resolves a family and style through the environment's registry.
func faceFor(env *Env, family string, style fonts.Style) *fonts.Face {
	if env == nil || env.Fonts == nil {
		return nil
	}
	return env.Fonts.Resolve(family, style)
}
