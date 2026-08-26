// The canonical formatter.
//
// `treekillbot fmt` settles indentation permanently, which in a language where
// indentation is structure is worth more than it would be elsewhere. It also
// makes the one syntactic ambiguity in Pulp unreachable: comments are rewritten
// as `# `, so the `#deadbeef` case where a comment's first word is eight hex
// digits cannot arise in a formatted file.
//
// Formatting works over physical lines rather than by printing the AST. That is
// deliberate: the AST drops comments and blank lines, and a formatter that
// silently deletes someone's comments is not a formatter. The tree is consulted
// only for depth, and every line is then re-emitted from its own text.
package pulp

import "strings"

// FormatOptions configure the formatter.
type FormatOptions struct {
	// Canonical resolves a name to its preferred spelling and reports whether
	// it is an element. It is a callback because the schema package sits above
	// this one, and inverting that dependency to normalise `colour` to `color`
	// would be a poor trade.
	Canonical func(name string) (canonical string, isElement bool, known bool)
	// Indent is the number of spaces per level. Zero means two.
	Indent int
}

// Format rewrites a document into canonical form.
//
// A file that does not parse is returned unchanged along with its diagnostics.
// Reformatting a broken file would move the very text the error message is
// pointing at, and a formatter that makes an error harder to find has done
// worse than nothing.
func Format(src *Source, options FormatOptions) (string, Diagnostics) {
	doc, diags := Parse(src)
	if diags.HasErrors() {
		return src.Text, diags
	}

	indent := options.Indent
	if indent <= 0 {
		indent = 2
	}

	f := &formatter{src: src, options: options, indent: indent}
	f.index(doc)
	return f.run(), diags
}

// formatter holds the per-line facts gathered from the tree.
type formatter struct {
	src     *Source
	options FormatOptions
	indent  int

	// depthOf maps a 1-based line number to its nesting depth.
	depthOf map[int]int
	// nodeOf maps a line number to the node that starts there.
	nodeOf map[int]*Node
	// indentOf records each node line's original indent, used to place a
	// trailing comment at the level its author wrote it at.
	indentOf map[int]int
	// inBlock marks lines that are the body of a block string, which are copied
	// with their relative indentation preserved and nothing else touched.
	inBlock map[int]blockLine
}

type blockLine struct {
	depth int
	strip int
}

func (f *formatter) index(doc *Document) {
	f.depthOf = map[int]int{}
	f.nodeOf = map[int]*Node{}
	f.indentOf = map[int]int{}
	f.inBlock = map[int]blockLine{}

	var walk func(n *Node, depth int)
	walk = func(n *Node, depth int) {
		if n.Name != "" {
			f.depthOf[n.Line] = depth
			f.nodeOf[n.Line] = n
			f.indentOf[n.Line] = leadingSpaces(f.src.Line(n.Line))
			if n.BlockString && !n.ArgSpan.IsZero() {
				f.markBlockBody(n, depth+1)
			}
		}
		for _, c := range n.Children {
			if n.Name == "" {
				walk(c, depth)
				continue
			}
			walk(c, depth+1)
		}
	}
	for _, top := range doc.TopLevel() {
		walk(top, 0)
	}
}

// markBlockBody records the lines a block string covers and the common indent
// to strip from them.
func (f *formatter) markBlockBody(n *Node, depth int) {
	first := f.src.Position(n.ArgSpan.Start).Line
	last := f.src.Position(n.ArgSpan.End).Line
	strip := leadingSpaces(f.src.Line(first))
	for line := first; line <= last; line++ {
		f.inBlock[line] = blockLine{depth: depth, strip: strip}
	}
}

// run emits the formatted document.
func (f *formatter) run() string {
	var (
		out           strings.Builder
		blankRun      int
		wroteAnything bool
	)
	total := f.src.LineCount()

	for line := 1; line <= total; line++ {
		text := f.src.Line(line)

		if body, ok := f.inBlock[line]; ok {
			out.WriteString(f.formatBlockBody(text, body))
			out.WriteByte('\n')
			blankRun = 0
			wroteAnything = true
			continue
		}

		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			blankRun++
			continue
		}

		// Runs of blank lines collapse to one, and leading blanks are dropped.
		// A single blank line is a paragraph break the author meant; three are
		// an accident of editing.
		if blankRun > 0 && wroteAnything {
			out.WriteByte('\n')
		}
		blankRun = 0
		wroteAnything = true

		if node, ok := f.nodeOf[line]; ok {
			out.WriteString(f.formatNodeLine(node, f.depthOf[line], text))
			out.WriteByte('\n')
			continue
		}

		// A comment-only line takes the depth of the next node line, so a
		// comment introducing a block sits with the block rather than trailing
		// the one above it.
		out.WriteString(strings.Repeat(" ", f.indent*f.commentDepth(line, total)))
		out.WriteString(normalizeComment(trimmed))
		out.WriteByte('\n')
	}

	return out.String()
}

// commentDepth finds the depth a free-standing comment should take.
func (f *formatter) commentDepth(line, total int) int {
	for next := line + 1; next <= total; next++ {
		if depth, ok := f.depthOf[next]; ok {
			return depth
		}
		if strings.TrimSpace(f.src.Line(next)) != "" && !isCommentLine(f.src.Line(next)) {
			break
		}
	}
	// A trailing comment has nothing to introduce, so it keeps the level its
	// author wrote it at: the nearest preceding node indented no deeper than
	// the comment itself. Using the immediately preceding node's depth instead
	// would pull a comment written at the outer level in under the last
	// property above it.
	own := leadingSpaces(f.src.Line(line))
	for previous := line - 1; previous >= 1; previous-- {
		depth, ok := f.depthOf[previous]
		if !ok {
			continue
		}
		if f.indentOf[previous] <= own {
			return depth
		}
	}
	return 0
}

// formatNodeLine re-emits one node line in canonical form.
//
// Elements take the bare form (`panel "Notes"`) and properties the colon form
// (`align: right`), which is the visual distinction people write naturally even
// though the grammar does not require it. Preserving it as a convention is the
// whole reason the formatter bothers to know which is which.
func (f *formatter) formatNodeLine(node *Node, depth int, original string) string {
	name := node.Name
	isElement := false
	if f.options.Canonical != nil {
		if canonical, element, known := f.options.Canonical(name); known {
			name, isElement = canonical, element
		}
	}

	var b strings.Builder
	b.WriteString(strings.Repeat(" ", f.indent*depth))
	b.WriteString(name)

	if node.BlockString {
		// The marker itself is not in Arg, so recover it from the source.
		marker := "|"
		if strings.Contains(original, ">") && !strings.Contains(original, "|") {
			marker = ">"
		}
		b.WriteString(": " + marker)
		return b.String() + trailingComment(original)
	}

	if node.HasArg {
		if isElement {
			b.WriteString(" " + node.Arg)
		} else {
			b.WriteString(": " + node.Arg)
		}
	} else if !isElement && node.Colon {
		// A property written with a colon and no value is a declaration, as in
		// a `vars` block. Keeping the colon is what distinguishes it from an
		// element.
		b.WriteString(":")
	}

	return b.String() + trailingComment(original)
}

// formatBlockBody re-indents one line of a block string, preserving whatever
// relative indentation the author used inside it.
func (f *formatter) formatBlockBody(text string, body blockLine) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	stripped := text
	if leadingSpaces(text) >= body.strip {
		stripped = text[body.strip:]
	} else {
		stripped = strings.TrimLeft(text, " ")
	}
	return strings.Repeat(" ", f.indent*body.depth) + strings.TrimRight(stripped, " \t")
}

// trailingComment recovers an inline comment from the original line, rendered
// canonically two spaces after the content.
func trailingComment(original string) string {
	// The scanner's own rule is reused, so a `#ddd` at the end of a line is
	// recognised as a colour here too rather than mistaken for a comment.
	index := inlineCommentIndex(original)
	if index < 0 {
		return ""
	}
	comment := strings.TrimSpace(original[index:])
	if comment == "" {
		return ""
	}
	return "  " + normalizeComment(comment)
}

// normalizeComment rewrites a comment as `# text`.
//
// This is what makes the one ambiguity in the language unreachable in formatted
// files: with a space after the hash, a comment can never be mistaken for an
// eight-digit hex colour.
func normalizeComment(text string) string {
	body := strings.TrimPrefix(text, "#")
	body = strings.TrimLeft(body, " \t")
	if body == "" {
		return "#"
	}
	return "# " + strings.TrimRight(body, " \t")
}

func isCommentLine(text string) bool {
	trimmed := strings.TrimSpace(text)
	return trimmed != "" && commentAt(trimmed, 0)
}

func leadingSpaces(text string) int {
	n := 0
	for n < len(text) && text[n] == ' ' {
		n++
	}
	return n
}
