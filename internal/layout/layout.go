// The two-pass layout algorithm.
//
// Measure runs bottom-up and answers "how tall does this node want to be at
// this width?". Arrange runs top-down and answers "here is your rectangle;
// place your children in it". Neither pass ever asks a node how *wide* it wants
// to be, and that omission is what keeps the engine small: because everything
// fills the width it is given, width is always known before a node is measured,
// and the whole min-content/max-content machinery that makes CSS layout hard
// simply does not arise.
//
// The one horizontal construct is a row, formed by a run of consecutive sibling
// columns. Column widths resolve before any column is measured, so the tight-
// width property holds there too.
package layout

import (
	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/schema"
)

// Layout measures and arranges a tree into a page rectangle.
func Layout(root *Node, page geom.Rect, env *Env) {
	Measure(root, page.W, env)
	Arrange(root, page, env)
}

// Measure returns a node's natural border-box height at a given border-box
// width, caching the answer.
//
// The result is the height the node would take if nothing constrained it. A
// node with an explicit height still reports its natural height here — honouring
// the declared height is Arrange's job, and conflating the two makes a fixed-
// height container unable to tell whether its contents actually fit.
func Measure(n *Node, width geom.Tick, env *Env) geom.Tick {
	if n.measured && n.naturalWidth == width {
		return n.natural
	}
	n.naturalWidth = width
	n.measured = true
	n.natural = measureNode(n, width, env)
	return n.natural
}

func measureNode(n *Node, width geom.Tick, env *Env) geom.Tick {
	chrome := n.chrome()
	contentWidth := width - chrome.Horizontal()
	if contentWidth < 0 {
		contentWidth = 0
	}

	switch n.Kind {
	case KindText:
		return measureText(n, contentWidth, env) + chrome.Vertical()
	case KindRule:
		return ruleThickness(n) + chrome.Vertical()
	case KindSpacer:
		// A spacer's whole purpose is its height property, so its natural size
		// is zero and Arrange gives it whatever it asked for.
		return chrome.Vertical()
	case KindImage:
		return chrome.Vertical()
	}

	if !n.Kind.IsContainer() {
		return chrome.Vertical()
	}

	// A container with a line decoration and no children is a writing area:
	// its natural height is however many ruled lines were asked for, or zero
	// when it is meant to fill whatever it is given.
	if len(n.Children) == 0 {
		return decorationNaturalHeight(n) + chrome.Vertical()
	}

	gap := n.Props.Tick(schema.PGap, 0)
	groups := groupChildren(n.Children)
	var total geom.Tick
	for i, g := range groups {
		total += measureGroup(g, contentWidth, gap, env)
		if i < len(groups)-1 {
			total += gap
		}
	}
	return total + chrome.Vertical()
}

// measureText wraps a text node and records the layout for Arrange to reuse.
func measureText(n *Node, contentWidth geom.Tick, env *Env) geom.Tick {
	st := textStyleFor(n, env)
	layout := WrapText(n.Text, st, contentWidth, 0)
	n.text = &layout
	return layout.Height
}

// ruleThickness returns how tall a `rule` node is: its line width, floored so
// that a rule is never invisible.
func ruleThickness(n *Node) geom.Tick {
	w := n.Props.Tick(schema.PLineWidth, geom.Pt(0.4))
	if w < minStrokeTicks {
		w = minStrokeTicks
	}
	return w
}

// minStrokeTicks is the thinnest line we will draw, in ticks. Below a quarter
// point, printers snap a stroke to one device pixel, which means its weight
// becomes a property of the printer rather than of the document — the opposite
// of what this tool promises.
const minStrokeTicks = geom.TicksPerPt / 4

// group is one row of the vertical flow: either a single child, or a run of
// consecutive columns that sit side by side.
type group struct {
	single  *Node
	columns []*Node
}

// groupChildren splits children into vertical-flow groups. A run of consecutive
// `column` siblings becomes one row; everything else stands alone.
//
// This is the rule that makes the original sketch's columns-inside-a-section
// mean what it looks like, and it composes with loops for free: seven generated
// columns form a row exactly as seven hand-written ones do.
func groupChildren(children []*Node) []group {
	var out []group
	for i := 0; i < len(children); {
		if children[i].Kind != KindColumn {
			out = append(out, group{single: children[i]})
			i++
			continue
		}
		start := i
		for i < len(children) && children[i].Kind == KindColumn {
			i++
		}
		out = append(out, group{columns: children[start:i]})
	}
	return out
}

// measureGroup returns a group's natural height at a content width.
func measureGroup(g group, contentWidth, gap geom.Tick, env *Env) geom.Tick {
	if g.single != nil {
		margin := g.single.Props.Edges(schema.PMargin, geom.Edges{})
		return childContribution(g.single, contentWidth-margin.Horizontal(), env) + margin.Vertical()
	}

	widths := resolveColumnWidths(g.columns, contentWidth, gap)
	var tallest geom.Tick
	for i, col := range g.columns {
		margin := col.Props.Edges(schema.PMargin, geom.Edges{})
		h := childContribution(col, widths[i]-margin.Horizontal(), env) + margin.Vertical()
		if h > tallest {
			tallest = h
		}
	}
	return tallest
}

// childContribution is how much height a child demands of a parent that is
// sizing itself to its contents.
//
// A child with an explicit height contributes THAT, not its intrinsic size.
// Measuring the intrinsic size here would undersize the parent, and the symptom
// is confusing: an `auto` section holding a `height: 40pt` panel reports that
// its content does not fit, naming two numbers that both look right on their
// own. A percentage or a `fill` has nothing to resolve against inside an
// auto-sized parent, so those fall back to the intrinsic size — which is the
// only answer available and, as it happens, the one that makes
// "fill inside auto" behave like "auto".
func childContribution(n *Node, width geom.Tick, env *Env) geom.Tick {
	natural := Measure(n, width, env)
	dim := n.Props.Dimension(schema.PHeight, defaultHeight(n.Kind))
	if dim.Mode != geom.SizeFixed {
		return natural
	}
	return geom.Clamp(dim.Length, n.Props.Tick(schema.PMinHeight, 0), n.Props.Tick(schema.PMaxHeight, 0))
}

// resolveColumnWidths distributes a content width across a row of columns.
//
// A column with `width: auto` is treated as `fill`. There is no intrinsic width
// in this engine — nothing ever reports how wide it would like to be — so auto
// has no other sensible meaning on the horizontal axis, and silently doing
// something arbitrary would be worse than defining it.
func resolveColumnWidths(columns []*Node, contentWidth, gap geom.Tick) []geom.Tick {
	items := make([]AxisItem, len(columns))
	for i, col := range columns {
		dim := col.Props.Dimension(schema.PWidth, geom.Fill)
		if dim.IsAuto() {
			dim = geom.Fill
		}
		margin := col.Props.Edges(schema.PMargin, geom.Edges{})
		if dim.Mode == geom.SizeFixed {
			dim.Length += margin.Horizontal()
		}
		items[i] = AxisItem{Dim: dim, Min: margin.Horizontal()}
	}
	return ResolveAxis(contentWidth, gap, items).Sizes
}

// Arrange places a node and its descendants, given the border-box rectangle the
// parent has decided on.
func Arrange(n *Node, border geom.Rect, env *Env) {
	n.Frame = n.buildFrame(border)
	if !n.Kind.IsContainer() || len(n.Children) == 0 {
		arrangeLeaf(n, env)
		return
	}

	content := n.Frame.Content
	gap := n.Props.Tick(schema.PGap, 0)
	groups := groupChildren(n.Children)

	items := make([]AxisItem, len(groups))
	for i, g := range groups {
		items[i] = AxisItem{
			Dim:     groupHeight(g),
			Natural: measureGroup(g, content.W, gap, env),
			Min:     groupMinHeight(g),
			Max:     groupMaxHeight(g),
		}
	}

	result := ResolveAxis(content.H, gap, items)
	reportOverflow(n, result, env)

	offsets := Offsets(content.Y, result.Sizes, gap)

	// When nothing on the axis was flexible there is space left over, and the
	// container's own valign decides where it goes rather than it being
	// silently dropped at the bottom.
	shift := geom.Tick(0)
	if result.Leftover > 0 {
		shift = VAlignOffset(n.Props.Enum(schema.PValign, "top"), result.Used, content.H)
	}

	for i, g := range groups {
		rect := geom.Rect{X: content.X, Y: offsets[i] + shift, W: content.W, H: result.Sizes[i]}
		arrangeGroup(g, rect, gap, env)
	}
}

// arrangeGroup places one vertical-flow group.
func arrangeGroup(g group, rect geom.Rect, gap geom.Tick, env *Env) {
	if g.single != nil {
		margin := g.single.Props.Edges(schema.PMargin, geom.Edges{})
		Arrange(g.single, rect.Inset(margin), env)
		return
	}

	widths := resolveColumnWidths(g.columns, rect.W, gap)
	offsets := Offsets(rect.X, widths, gap)
	for i, col := range g.columns {
		margin := col.Props.Edges(schema.PMargin, geom.Edges{})
		cell := geom.Rect{X: offsets[i], Y: rect.Y, W: widths[i], H: rect.H}
		// Columns in a row are always the height of the row. Letting each one
		// size itself would make a row of day boxes ragged, which is never what
		// anyone wants from a form.
		Arrange(col, cell.Inset(margin), env)
	}
}

// arrangeLeaf finalises a node with no children, re-wrapping text against the
// width it actually received.
func arrangeLeaf(n *Node, env *Env) {
	if n.Kind != KindText {
		return
	}
	content := n.Frame.Content
	st := textStyleFor(n, env)
	maxHeight := geom.Tick(0)
	if n.Props.Dimension(schema.PHeight, geom.Auto).Mode != geom.SizeAuto {
		maxHeight = content.H
	}
	layout := WrapText(n.Text, st, content.W, maxHeight)
	n.text = &layout

	if layout.Shrunk && n.Source != nil {
		env.Diags.Warnf(n.Source, n.Span, "W020",
			"text was shrunk to %.2fpt to fit", float64(layout.SizeQpt)/4).
			WithLabel("shrunk to fit").
			WithHelp("Give the box more height, shorten the text, or set `auto-shrink: none` to make this an error instead.")
	}
	if layout.Clipped && n.Source != nil {
		env.Diags.Warnf(n.Source, n.Span, "W021",
			"text does not fit and was clipped").
			WithLabel("clipped").
			WithHelp("The box is %.2fpt tall but the text needs %.2fpt.", content.H.Points(), layout.Height.Points())
	}
}

// groupHeight returns the height demand a group makes on its parent's vertical
// axis. For a row, the most flexible column wins: if any column wants to fill,
// the row fills, because a row is only as tall as the space its tallest member
// is entitled to.
func groupHeight(g group) geom.Dimension {
	if g.single != nil {
		return heightDim(g.single)
	}
	best := geom.Auto
	for _, col := range g.columns {
		dim := heightDim(col)
		if flexRank(dim) > flexRank(best) {
			best = dim
			continue
		}
		// Among equally flexible demands, the largest wins, so a row is tall
		// enough for every column in it.
		if flexRank(dim) == flexRank(best) {
			switch dim.Mode {
			case geom.SizeFixed:
				if dim.Length > best.Length {
					best = dim
				}
			case geom.SizePercent:
				if dim.Pct > best.Pct {
					best = dim
				}
			case geom.SizeFill:
				if dim.Weight > best.Weight {
					best = dim
				}
			}
		}
	}
	return best
}

// defaultHeight returns the height a node takes when it declares none.
//
// A column defaults to `fill` rather than `auto` because a column IS a
// full-height strip — that is what the word means on a form, and it is what
// makes the original sketch's `column` + `panel: height fill` do the obvious
// thing without the author having to say `height: fill` twice. Everything else
// defaults to auto: sections stack, and a stack whose members all grabbed the
// leftover would have nothing to stack.
func defaultHeight(kind Kind) geom.Dimension {
	if kind == KindColumn {
		return geom.Fill
	}
	return geom.Auto
}

// heightDim returns a node's height demand, expressed as a MARGIN-box size.
//
// The axis resolver works in margin boxes throughout, because margins are space
// the axis must account for even though they are not part of the declared size.
// Folding the margin into a fixed length here is what keeps `height: 100pt` with
// `margin: 10pt` a 100pt box inside a 120pt slot, rather than an 80pt box
// squeezed into a 100pt one.
func heightDim(n *Node) geom.Dimension {
	dim := n.Props.Dimension(schema.PHeight, defaultHeight(n.Kind))
	if dim.Mode == geom.SizeFixed {
		dim.Length += n.Props.Edges(schema.PMargin, geom.Edges{}).Vertical()
	}
	return dim
}

// flexRank orders sizing modes by how much say they have over leftover space.
func flexRank(d geom.Dimension) int {
	switch d.Mode {
	case geom.SizeAuto:
		return 0
	case geom.SizeFixed:
		return 1
	case geom.SizePercent:
		return 2
	case geom.SizeFill:
		return 3
	}
	return 0
}

func groupMinHeight(g group) geom.Tick {
	if g.single != nil {
		return g.single.Props.Tick(schema.PMinHeight, 0)
	}
	var largest geom.Tick
	for _, col := range g.columns {
		if v := col.Props.Tick(schema.PMinHeight, 0); v > largest {
			largest = v
		}
	}
	return largest
}

func groupMaxHeight(g group) geom.Tick {
	if g.single != nil {
		return g.single.Props.Tick(schema.PMaxHeight, 0)
	}
	// A row can be no taller than its most restrictive column allows.
	var smallest geom.Tick
	for _, col := range g.columns {
		v := col.Props.Tick(schema.PMaxHeight, 0)
		if v == 0 {
			continue
		}
		if smallest == 0 || v < smallest {
			smallest = v
		}
	}
	return smallest
}

// reportOverflow turns an over-committed axis into a diagnostic that names the
// numbers, because "it does not fit" without them is useless.
func reportOverflow(n *Node, result AxisResult, env *Env) {
	if result.Overflow <= 0 || n.Source == nil {
		return
	}
	mode := n.Props.Enum(schema.POverflow, "error")
	if mode == "clip" || mode == "visible" {
		return
	}

	content := n.Frame.Content
	needed := content.H + result.Overflow
	message := "content does not fit in this " + n.Kind.String()
	help := ""
	if needed > 0 {
		help = "It needs " + formatPt(needed) + " but has " + formatPt(content.H) +
			", short by " + formatPt(result.Overflow) + "."
	}

	if env.AllowOverflow || mode != "error" {
		env.Diags.Warnf(n.Source, n.Span, "W010", "%s", message).
			WithLabel("overflows by %s", formatPt(result.Overflow)).
			WithHelp("%s", help)
		return
	}
	env.Diags.Errorf(n.Source, n.Span, "E010", "%s", message).
		WithLabel("overflows by %s", formatPt(result.Overflow)).
		WithHelp("%s Give it more room, or set `overflow: clip` to allow it.", help)
}

// NaturalHeight reports a node's measured intrinsic height, for callers that
// need it after layout has run.
func (n *Node) NaturalHeight() geom.Tick { return n.natural }

// decorationNaturalHeight returns the height an empty writing area wants: the
// space its requested number of ruled lines occupies, or zero when it is meant
// to fill whatever it is given.
func decorationNaturalHeight(n *Node) geom.Tick {
	if n.Props.Enum(schema.PLineStyle, "none") == "none" {
		return 0
	}
	count := n.Props.Int(schema.PLineCount, 0)
	if count <= 0 {
		return 0
	}
	pitch := n.Props.Tick(schema.PLinePitch, geom.Mm(6))
	return pitch * geom.Tick(count)
}
