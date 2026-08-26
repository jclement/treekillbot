// The layout tree: what the engine actually measures and arranges.
//
// This is a separate tree from the parsed one. The parse tree records what the
// author wrote; this records what is on the page, after variables are
// substituted, loops are expanded, `when` has dropped what it drops, and the
// cascade has resolved every property. Keeping them separate means layout never
// has to ask "is this node real?", and it means a loop that produces seven day
// cells produces seven ordinary nodes rather than a special case that every
// later stage must know about.
package layout

import (
	"github.com/jclement/treekillbot/internal/fonts"
	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/pulp"
	"github.com/jclement/treekillbot/internal/schema"
)

// Kind is what sort of thing a node is.
type Kind uint8

const (
	KindPage Kind = iota
	KindSection
	KindColumn
	KindPanel
	KindBox
	KindGrid
	KindText
	KindRule
	KindSpacer
	KindImage
)

// String returns the kind's DSL name.
func (k Kind) String() string {
	switch k {
	case KindPage:
		return "page"
	case KindSection:
		return "section"
	case KindColumn:
		return "column"
	case KindPanel:
		return "panel"
	case KindBox:
		return "box"
	case KindGrid:
		return "grid"
	case KindText:
		return "text"
	case KindRule:
		return "rule"
	case KindSpacer:
		return "spacer"
	case KindImage:
		return "image"
	}
	return "?"
}

// IsContainer reports whether children are meaningful.
func (k Kind) IsContainer() bool {
	switch k {
	case KindPage, KindSection, KindColumn, KindPanel, KindBox, KindGrid:
		return true
	}
	return false
}

// KindFor maps an element name onto a layout kind.
func KindFor(element string) (Kind, bool) {
	switch element {
	case schema.EPage:
		return KindPage, true
	case schema.ESection:
		return KindSection, true
	case schema.EColumn:
		return KindColumn, true
	case schema.EPanel:
		return KindPanel, true
	case schema.EBox:
		return KindBox, true
	case schema.EGrid:
		return KindGrid, true
	case schema.EText:
		return KindText, true
	case schema.ERule:
		return KindRule, true
	case schema.ESpacer:
		return KindSpacer, true
	case schema.EImage:
		return KindImage, true
	}
	return 0, false
}

// Frame is a node's four nested rectangles, valid only after Arrange.
//
// Sizing is border-box throughout: a declared height is the height of Border.
// Padding and border eat into Content; they never inflate the box. See
// DESIGN.md D4 for why this is the only mode offered.
type Frame struct {
	Margin  geom.Rect // Border grown by the margin
	Border  geom.Rect // the declared box — what `height: 100pt` means
	Padding geom.Rect // inside the border stroke
	Content geom.Rect // inside the padding: where children and text go
}

// Node is one box in the layout tree.
type Node struct {
	Kind     Kind
	Props    *schema.Props
	Children []*Node
	Parent   *Node

	// Text is the content of a text node, already interpolated and
	// case-transformed.
	Text string
	// Title is a panel's title, already interpolated.
	Title string

	// Source position, retained so an overflow discovered during layout can
	// point at the line that declared the box.
	Span   pulp.Span
	Source *pulp.Source

	// Frame is filled by Arrange.
	Frame Frame

	// natural caches the measured intrinsic height, keyed by the width it was
	// measured at. Measurement is pure, and a container measures its children
	// once to size itself and again to place them, so the cache turns an
	// accidentally quadratic walk back into a linear one.
	natural      geom.Tick
	naturalWidth geom.Tick
	measured     bool

	// titleBand is the room the node's title claims, computed during Arrange
	// and read back by the painter.
	titleBand TitleBand

	// text holds the wrapped lines once measured, so Arrange and Paint use the
	// same line breaks that Measure sized the box against. Recomputing them is
	// how a box ends up one line short of its own content.
	text *TextLayout
}

// NewNode returns a node of the given kind with an empty property set.
func NewNode(kind Kind) *Node {
	return &Node{Kind: kind, Props: schema.NewProps()}
}

// Append adds a child and links it back to its parent.
func (n *Node) Append(child *Node) {
	child.Parent = n
	n.Children = append(n.Children, child)
}

// Walk visits the node and its descendants depth-first in document order.
func (n *Node) Walk(fn func(*Node) bool) {
	if !fn(n) {
		return
	}
	for _, c := range n.Children {
		c.Walk(fn)
	}
}

// TextLayout returns the wrapped lines computed during layout, or nil for a
// node that holds no text.
//
// Painting must use this rather than re-wrapping: two wraps of the same string
// can disagree if anything about the style resolution differs by a hair, and
// the visible result is a box one line short of its own contents.
func (n *Node) TextLayout() *TextLayout { return n.text }

// Label returns a human name for the node, used in diagnostics and in the
// layout dump. A panel's title is far more useful than "panel #4".
func (n *Node) Label() string {
	if id := n.Props.Str(schema.PID, ""); id != "" {
		return n.Kind.String() + " #" + id
	}
	if n.Title != "" {
		return n.Kind.String() + ` "` + n.Title + `"`
	}
	return n.Kind.String()
}

// Env is everything layout needs that is not the tree itself.
type Env struct {
	// Fonts resolves a family and style to a face. It is an interface so the
	// engine can be tested against a synthetic font whose metrics are round
	// numbers, without embedding anything.
	Fonts FontResolver
	// Diags collects overflow and fit warnings raised during layout.
	Diags *pulp.Diagnostics
	// PageGrid anchors dot and graph lattices so they line up across adjacent
	// panels rather than restarting inside each one.
	PageGrid PageGrid
	// AllowOverflow downgrades overflow errors to warnings.
	AllowOverflow bool
}

// FontResolver hands out faces by family and style.
type FontResolver interface {
	Resolve(family string, style fonts.Style) *fonts.Face
}

// PageGrid is the page-global lattice for dot and graph decorations.
type PageGrid struct {
	Origin geom.Rect
	Pitch  geom.Tick
}

// buildFrame derives the four nested rectangles from a border-box rectangle and
// the node's own spacing properties.
func (n *Node) buildFrame(border geom.Rect) Frame {
	margin := n.Props.Edges(schema.PMargin, geom.Edges{})
	borderWidth := n.borderWidth()
	padding := n.Props.Edges(schema.PPadding, geom.Edges{})

	paddingRect := border.Inset(borderWidth)
	return Frame{
		Margin:  border.Outset(margin),
		Border:  border,
		Padding: paddingRect,
		Content: paddingRect.Inset(padding),
	}
}

// borderWidth returns the node's per-side border thickness, honouring
// border-style: none.
func (n *Node) borderWidth() geom.Edges {
	if n.Props.Enum(schema.PBorderStyle, "solid") == "none" {
		return geom.Edges{}
	}
	return n.Props.Edges(schema.PBorderWidth, geom.Edges{})
}

// chrome returns the total space per axis that the border and padding take out
// of a node's declared size, which is what Measure must add back when turning a
// content height into a border-box height.
func (n *Node) chrome() geom.Edges {
	bw := n.borderWidth()
	pad := n.Props.Edges(schema.PPadding, geom.Edges{})
	return geom.Edges{
		Top:    bw.Top + pad.Top,
		Right:  bw.Right + pad.Right,
		Bottom: bw.Bottom + pad.Bottom,
		Left:   bw.Left + pad.Left,
	}
}
