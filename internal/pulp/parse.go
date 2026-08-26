// Turning scanned lines into a tree.
//
// The whole parser is an indentation stack. There is no configurable indent
// unit: the first child of a block fixes that block's column, siblings must
// match it exactly, and a dedent must land on a column that some enclosing
// block is already open at. That rule accepts the ragged 3/5/7-space
// indentation people actually type while still rejecting the misalignment that
// silently reparents a node — which is the failure mode that matters, because
// it produces a valid document that is not the one you wrote.
package pulp

import "strings"

// Parse scans and parses a source file. It returns a Document even when
// diagnostics contain errors, so callers that want to report several problems
// at once — `check`, an editor — can keep walking a partial tree.
func Parse(src *Source) (*Document, Diagnostics) {
	var diags Diagnostics
	p := &parser{src: src, diags: &diags}
	doc := p.parse()
	diags.Sort()
	return doc, diags
}

// ParseString is a convenience wrapper for tests and stdin.
func ParseString(name, text string) (*Document, Diagnostics) {
	return Parse(NewSource(name, text))
}

// openBlock is one level of the indentation stack.
type openBlock struct {
	col    int   // the column every node in this block starts at
	parent *Node // the node these are children of
	last   *Node // the most recent node added here, and so the only legal target of a deeper indent
}

type parser struct {
	src   *Source
	diags *Diagnostics
	sc    *scanner
	stack []openBlock
}

func (p *parser) parse() *Document {
	root := &Node{Source: p.src, Name: ""}
	p.sc = newScanner(p.src, p.diags)
	p.stack = []openBlock{{col: 0, parent: root}}

	for !p.sc.atEOF() {
		line := p.sc.next()
		if line.kind != lineNode {
			continue
		}
		p.addLine(line)
	}

	setSubtreeSpans(root)
	return &Document{Source: p.src, Root: root}
}

// addLine places one scanned node line into the tree.
func (p *parser) addLine(line scannedLine) {
	// Snapshot the open columns before popping: by the time a misaligned dedent
	// is detected, the stack no longer holds the columns that would have been
	// legal, which are exactly what the error needs to name.
	openColumns := make([]int, len(p.stack))
	for i, b := range p.stack {
		openColumns[i] = b.col
	}
	dedented := false
	for len(p.stack) > 1 && line.indent < p.top().col {
		p.stack = p.stack[:len(p.stack)-1]
		dedented = true
	}
	top := p.top()

	switch {
	case line.indent == top.col:
		// Ordinary sibling.

	case line.indent > top.col:
		// A deeper indent opens a new block under the previous node. After a
		// dedent it cannot, because the column we landed on is not one any
		// enclosing block is open at — that is the silent-reparenting case.
		if dedented {
			p.misalignedDedent(line, openColumns)
			return
		}
		if top.last == nil {
			p.diags.Errorf(p.src, line.nameSpan, "E003", "unexpected indentation").
				WithLabel("indented with nothing to belong to").
				WithHelp("This line is indented further than the one above it, but the line above it is not a block that can contain children.")
			return
		}
		p.stack = append(p.stack, openBlock{col: line.indent, parent: top.last})

	default:
		p.misalignedDedent(line, openColumns)
		return
	}

	node := p.newNode(line)
	top = p.top()
	top.parent.Children = append(top.parent.Children, node)
	node.Parent = top.parent
	p.stack[len(p.stack)-1].last = node

	if line.blockArg != 0 {
		p.consumeBlockString(node, line)
	}
}

// misalignedDedent reports a dedent that does not line up with any open block,
// naming the columns that would have been legal.
func (p *parser) misalignedDedent(line scannedLine, openColumns []int) {
	legal := make([]string, 0, len(openColumns))
	for _, col := range openColumns {
		legal = append(legal, itoa(col))
	}
	p.diags.Errorf(p.src, line.nameSpan, "E003",
		"this line's indentation does not match any enclosing block").
		WithLabel("column %d", line.indent+1).
		WithHelp("Indent it to one of these columns: %s. Run `treekillbot fmt` to normalise the whole file to two spaces.",
			strings.Join(legal, ", "))
}

func (p *parser) top() *openBlock { return &p.stack[len(p.stack)-1] }

func (p *parser) newNode(line scannedLine) *Node {
	n := &Node{
		Name:     line.name,
		NameSpan: line.nameSpan,
		Colon:    line.colon,
		Source:   p.src,
		Line:     line.number,
		Span:     line.span,
	}
	if line.hasArg && line.blockArg == 0 {
		n.Arg = line.argText
		n.ArgSpan = line.argSpan
		n.HasArg = true
	}
	return n
}

// consumeBlockString reads the indented block beneath a `|` or `>` node as the
// node's argument.
//
// A block-string node takes its ENTIRE child block as the body and has no child
// nodes. The alternative — trying to tell a property line apart from a body
// line that happens to look like one — cannot be done reliably, because
// "1. What changed since Friday" is legitimate body text and `font-size: 8pt`
// is legitimate body text too. When a node needs both text and properties, the
// text goes in an explicit `content:` block:
//
//	text
//	  font-size: 8pt
//	  content: |
//	    line one
//	    line two
func (p *parser) consumeBlockString(node *Node, marker scannedLine) {
	var (
		body       []string
		stripCol   = -1
		firstStart = -1
		lastEnd    = -1
	)
	for !p.sc.atEOF() {
		save, saveLine := p.sc.pos, p.sc.line
		line, raw := p.sc.nextRaw()

		if line.kind == lineBlank {
			// A blank line inside a block string is part of it; a blank line
			// after it is trivia. We cannot tell yet, so hold it and decide
			// when the next non-blank line arrives.
			body = append(body, "")
			continue
		}
		if line.indent <= marker.indent {
			p.sc.pos, p.sc.line = save, saveLine // put it back for the main loop
			break
		}
		if stripCol < 0 {
			stripCol = line.indent
			firstStart = line.span.Start
		}
		text := raw
		if len(text) >= stripCol {
			text = text[stripCol:]
		} else {
			text = strings.TrimLeft(text, " ")
		}
		body = append(body, text)
		lastEnd = line.span.End
	}

	// Trailing blank lines belong to the document, not to the string.
	for len(body) > 0 && body[len(body)-1] == "" {
		body = body[:len(body)-1]
	}

	node.BlockString = true
	node.HasArg = true
	if firstStart >= 0 {
		node.ArgSpan = Span{Start: firstStart, End: lastEnd}
	} else {
		node.ArgSpan = marker.argSpan
	}

	if marker.blockArg == blockStringKeep {
		node.Arg = strings.Join(body, "\n")
		return
	}
	node.Arg = foldLines(body)
}

// foldLines implements the `>` marker: consecutive non-blank lines join with a
// space, and a blank line becomes a paragraph break.
func foldLines(lines []string) string {
	var out strings.Builder
	pendingBreak := false
	first := true
	for _, l := range lines {
		if l == "" {
			pendingBreak = true
			continue
		}
		switch {
		case first:
			first = false
		case pendingBreak:
			out.WriteString("\n")
		default:
			out.WriteString(" ")
		}
		pendingBreak = false
		out.WriteString(strings.TrimRight(l, " "))
	}
	return out.String()
}

// setSubtreeSpans fills in each node's SubtreeSpan bottom-up, so a diagnostic
// about an element can underline the element and everything inside it.
func setSubtreeSpans(n *Node) Span {
	span := n.Span
	for _, c := range n.Children {
		span = span.Join(setSubtreeSpans(c))
	}
	n.SubtreeSpan = span
	return span
}

// itoa avoids pulling strconv into this file's import list for one use.
func itoa(v int) string {
	if v == 0 {
		return "1"
	}
	// Columns are reported 1-based to match editors.
	v++
	var buf [12]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
