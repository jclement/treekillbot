// Collapsing shared borders.
//
// Two boxes that touch each other exactly — a row of cells at `gap: 0`, a stack
// of bands — each stroke the edge they share, so the line between them comes out
// twice as heavy as the lines around them. On a form that reads as a mistake,
// because it is one.
//
// DESIGN.md D4 describes the fix as the container drawing the lattice while its
// children draw nothing. That turns out to be the wrong place to put it: the
// boxes whose borders touch are frequently not siblings — a row of columns each
// holding one bordered panel has the panels as grandchildren of the row, and
// their edges coincide without their parents' doing so.
//
// So the rule here is stated on the edges themselves rather than on the tree:
// a box does not stroke its left or top edge when another box, with an
// identical pen, already strokes that exact line. Whoever is above or to the
// left keeps the stroke, exactly one survives, and it works at any nesting
// depth.
package draw

import (
	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/layout"
	"github.com/jclement/treekillbot/internal/paint"
	"github.com/jclement/treekillbot/internal/schema"
)

// suppressed records which of a node's borders another box already draws.
type suppressed map[*layout.Node]sides

// sides names the edges to skip. Only left and top are ever suppressed: for a
// shared edge, dropping the same side every time leaves exactly one stroke.
type sides struct {
	left, top bool
}

// borderedBox is one box that strokes a border, with the pen it uses.
type borderedBox struct {
	node  *layout.Node
	rect  geom.Rect
	width geom.Tick
	color paint.Color
	style string
}

// collapseBorders finds every shared edge on the page and decides who keeps it.
//
// Edges must coincide exactly, which is a reasonable thing to require here only
// because the layout engine works in integer ticks: two boxes that tile are
// tick-identical at their boundary rather than nearly so (DESIGN.md D1). With
// float geometry this rule would fire almost at random.
func collapseBorders(root *layout.Node, env *Env) suppressed {
	boxes := collectBorderedBoxes(root, env)
	if len(boxes) < 2 {
		return nil
	}

	out := suppressed{}
	for i, box := range boxes {
		var skip sides
		for j, other := range boxes {
			if i == j || !samePen(box, other) {
				continue
			}
			if !skip.left && other.rect.Right() == box.rect.X && overlapsVertically(box.rect, other.rect) {
				skip.left = true
			}
			if !skip.top && other.rect.Bottom() == box.rect.Y && overlapsHorizontally(box.rect, other.rect) {
				skip.top = true
			}
			if skip.left && skip.top {
				break
			}
		}
		if skip.left || skip.top {
			out[box.node] = skip
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// collectBorderedBoxes gathers every node that strokes a uniform border.
//
// Only uniform borders take part. A box with different widths per side has
// already said something specific about its edges, and quietly dropping one of
// them would be a worse surprise than a doubled line.
func collectBorderedBoxes(root *layout.Node, env *Env) []borderedBox {
	var boxes []borderedBox
	root.Walk(func(n *layout.Node) bool {
		if !n.Props.Bool(schema.PBorderCollapse, true) {
			return true
		}
		width := n.Props.Edges(schema.PBorderWidth, geom.Edges{})
		style := n.Props.Enum(schema.PBorderStyle, "solid")
		if style == "none" || !width.Uniform() || width.Top <= 0 {
			return true
		}
		// A rounded corner has no straight edge to share, so collapsing would
		// leave a visible gap where the curve pulls away from the join.
		if n.Props.Tick(schema.PBorderRadius, 0) > 0 {
			return true
		}
		color := colorOf(n.Props, schema.PBorderColor, paint.GrayN(0.75), env)
		if color.IsInvisible() {
			return true
		}
		if n.Frame.Border.IsEmpty() {
			return true
		}
		boxes = append(boxes, borderedBox{
			node:  n,
			rect:  n.Frame.Border,
			width: clampStroke(width.Top),
			color: color,
			style: style,
		})
		return true
	})
	return boxes
}

// samePen reports whether two boxes would stroke an edge identically. Only
// then does dropping one of the two strokes leave the page unchanged.
func samePen(a, b borderedBox) bool {
	return a.width == b.width && a.color == b.color && a.style == b.style
}

func overlapsVertically(a, b geom.Rect) bool {
	return a.Y < b.Bottom() && b.Y < a.Bottom()
}

func overlapsHorizontally(a, b geom.Rect) bool {
	return a.X < b.Right() && b.X < a.Right()
}
