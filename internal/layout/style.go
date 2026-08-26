// Turning resolved properties into the concrete style values layout needs.
//
// The cascade produces a bag of typed property values; this file translates
// that bag into the handful of things measurement and painting actually ask
// for — a font face, a size in quarter-points, a stroke. Keeping the
// translation here rather than at each call site means "what does font-weight
// bold mean when the family has no bold?" is answered once.
package layout

import (
	"strconv"
	"strings"

	"github.com/jclement/treekillbot/internal/fonts"
	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/paint"
	"github.com/jclement/treekillbot/internal/render"
	"github.com/jclement/treekillbot/internal/schema"
)

// defaultFontSizeQpt is 9pt in quarter-points, matching the schema default. It
// is the fallback when a document somehow reaches layout with no size at all.
const defaultFontSizeQpt = 36

// quarterPoints converts a length to quarter-points, the unit font sizes are
// carried in.
//
// Sizes are quantised to a quarter of a point rather than kept as a length so
// that auto-shrink searches a discrete space: a continuous size would make the
// chosen value depend on float rounding, and two runs could pick differently.
func quarterPoints(t geom.Tick) int32 {
	q := int32((int64(t)*4 + int64(geom.TicksPerPt)/2) / int64(geom.TicksPerPt))
	if q < 1 {
		q = 1
	}
	return q
}

// fontSizeQpt returns a node's resolved type size in quarter-points.
func fontSizeQpt(p *schema.Props, id schema.PropID, fallback int32) int32 {
	t := p.Tick(id, 0)
	if t <= 0 {
		return fallback
	}
	return quarterPoints(t)
}

// faceStyle reads font-weight and font-style into a concrete face style.
func faceStyle(p *schema.Props, weightID, styleID schema.PropID) fonts.Style {
	weight := 400
	switch w := p.Enum(weightID, "regular"); w {
	case "bold":
		weight = 700
	case "regular":
		weight = 400
	default:
		if n, err := strconv.Atoi(w); err == nil {
			weight = n
		}
	}
	italic := p.Enum(styleID, "normal") == "italic"
	return fonts.StyleFor(weight, italic)
}

// textStyleFor builds the wrapping style for a text node.
func textStyleFor(n *Node, env *Env) TextStyle {
	p := n.Props
	size := fontSizeQpt(p, schema.PFontSize, defaultFontSizeQpt)
	st := TextStyle{
		SizeQpt:    size,
		LineHeight: p.Num(schema.PLineHeight, 1.35),
		Tracking:   p.Tick(schema.PTracking, 0),
		Align:      p.Enum(schema.PAlign, "left"),
		Wrap:       p.Bool(schema.PWrap, true),
		AutoShrink: p.Num(schema.PAutoShrink, 0),
	}
	if env != nil && env.Fonts != nil {
		st.Face = env.Fonts.Resolve(p.Str(schema.PFont, "IBM Plex Mono"),
			faceStyle(p, schema.PFontWeight, schema.PFontStyle))
	}
	return st
}

// titleStyleFor builds the style for a panel title, falling back to the node's
// body typography for anything the title does not override.
func titleStyleFor(n *Node, env *Env) TextStyle {
	p := n.Props
	size := fontSizeQpt(p, schema.PTitleSize, fontSizeQpt(p, schema.PFontSize, defaultFontSizeQpt))
	st := TextStyle{
		SizeQpt:    size,
		LineHeight: 1,
		Tracking:   p.Tick(schema.PTitleTracking, 0),
		Align:      p.Enum(schema.PTitleAlign, "left"),
		Wrap:       false,
		AutoShrink: 0.7,
	}
	family := p.Str(schema.PTitleFont, "")
	if family == "" {
		family = p.Str(schema.PFont, "IBM Plex Mono")
	}
	if env != nil && env.Fonts != nil {
		st.Face = env.Fonts.Resolve(family, faceStyle(p, schema.PTitleWeight, schema.PFontStyle))
	}
	return st
}

// strokeFor builds a pen from a colour, a width and a border style.
func strokeFor(color paint.Color, width geom.Tick, style string) render.Stroke {
	if width > 0 && width < minStrokeTicks {
		width = minStrokeTicks
	}
	s := render.Stroke{Color: color, Width: width}
	switch style {
	case "dashed":
		s.Dash = []geom.Tick{width * 3, width * 3}
	case "dotted":
		// Dots are drawn as short dashes because the PDF backend has no round
		// line cap, and a zero-length segment with a butt cap draws nothing.
		s.Dash = []geom.Tick{width, width * 2}
	}
	return s
}

// applyTransform applies text-transform to a string.
func applyTransform(s, transform string) string {
	switch transform {
	case "upper":
		return strings.ToUpper(s)
	case "lower":
		return strings.ToLower(s)
	case "title":
		return titleCase(s)
	}
	return s
}

// titleCase upper-cases the first letter of each word. Go's strings.Title is
// deprecated and x/text's caser is locale-aware in ways this tool explicitly
// scopes out (DESIGN.md D12), so a plain ASCII-minded implementation is both
// smaller and more predictable here.
func titleCase(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	atWordStart := true
	for _, r := range s {
		if atWordStart {
			b.WriteString(strings.ToUpper(string(r)))
		} else {
			b.WriteRune(r)
		}
		atWordStart = r == ' ' || r == '\t' || r == '-' || r == '/'
	}
	return b.String()
}

// formatPt renders a length in points for a diagnostic, trimmed so that whole
// numbers do not carry pointless decimals.
func formatPt(t geom.Tick) string {
	v := t.Points()
	s := strconv.FormatFloat(v, 'f', 2, 64)
	s = strings.TrimSuffix(strings.TrimRight(s, "0"), ".")
	return s + "pt"
}
