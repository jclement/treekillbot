// The syntax tree.
//
// The tree is deliberately untyped with respect to the DSL's meaning: a Node is
// just a name, an optional raw argument, and children, all carrying spans. The
// schema decides afterwards that `align` is a property of `text` and `panel` is
// an element of `column`. Keeping the tree this plain is what makes the "every
// line is a node" idea pay off — and it means nothing downstream is coupled to
// Pulp's syntax, so an alternative front-end would be an adapter rather than a
// rewrite.
package pulp

import "strings"

// Node is one line of a document, plus whatever was indented beneath it.
type Node struct {
	Name     string
	NameSpan Span

	// Arg is the raw argument text with any inline comment and surrounding
	// whitespace already removed. For a block string it is the assembled body.
	// It is left uninterpreted because whether it should be read as a list of
	// typed values or as text to end-of-line depends on the schema.
	Arg     string
	ArgSpan Span
	HasArg  bool

	// Colon records whether the author wrote `name: arg` or `name arg`. The
	// grammar treats them identically; `treekillbot fmt` uses this to normalise
	// elements to the bare form and properties to the colon form, which is how
	// the visual distinction survives as a convention.
	Colon bool

	// BlockString is set when the argument came from a `|` or `>` block.
	BlockString bool

	Children []*Node
	Parent   *Node
	Source   *Source
	Line     int

	// Span covers the node's own line; SubtreeSpan also covers its children,
	// and is what an error about a whole element underlines.
	Span        Span
	SubtreeSpan Span
}

// Document is a parsed .pulp file: a synthetic root whose children are the
// top-level nodes.
type Document struct {
	Source *Source
	Root   *Node
}

// TopLevel returns the document's top-level nodes.
func (d *Document) TopLevel() []*Node {
	if d.Root == nil {
		return nil
	}
	return d.Root.Children
}

// Child returns the first child with the given name, or nil.
func (n *Node) Child(name string) *Node {
	for _, c := range n.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// ChildrenNamed returns every child with the given name, in source order.
func (n *Node) ChildrenNamed(name string) []*Node {
	var out []*Node
	for _, c := range n.Children {
		if c.Name == name {
			out = append(out, c)
		}
	}
	return out
}

// Path returns the node's position as a slash-separated chain of names, used in
// diagnostics so the reader knows which of six identical panels is meant.
func (n *Node) Path() string {
	var parts []string
	for cur := n; cur != nil && cur.Name != ""; cur = cur.Parent {
		parts = append(parts, cur.Name)
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, "/")
}

// Walk calls fn for the node and every descendant, depth-first in source order.
// A fn returning false skips that node's children.
func (n *Node) Walk(fn func(*Node) bool) {
	if !fn(n) {
		return
	}
	for _, c := range n.Children {
		c.Walk(fn)
	}
}
