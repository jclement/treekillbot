// The line scanner.
//
// Pulp is line-oriented: every line is a node, written as `name [argument]`
// with an optional indented block beneath it. That means the scanner's job is
// small and well defined — classify each physical line, find the identifier,
// and hand the argument onward as an unparsed span. The argument is not
// tokenised here, because whether it should be read as a list of typed values
// or as raw text to end-of-line depends on the schema, which does not exist yet
// at scan time. Both readings stay available because the span is kept.
package pulp

import "strings"

// lineKind classifies a scanned physical line.
type lineKind uint8

const (
	lineBlank lineKind = iota
	lineComment
	lineNode
)

// scannedLine is one logical line: a node, a comment, or blank. Continuation
// lines ending in a backslash have already been joined into one of these.
type scannedLine struct {
	kind   lineKind
	number int  // 1-based line number of the line's first physical line
	indent int  // leading spaces; tabs are rejected before this is set
	span   Span // the whole logical line, excluding the newline

	nameSpan Span // identifier, for lineNode
	name     string

	argSpan  Span   // the raw argument's source range; approximate for joined lines
	argText  string // the resolved argument text, authoritative over argSpan
	hasArg   bool
	joined   bool // true when backslash continuations were folded into this line
	colon    bool // true when written `name: arg` rather than `name arg`
	blockArg byte // '|' or '>' when the argument is a block-string marker, else 0
}

// blockStringKeep and blockStringFold are the two block-string markers: `|`
// preserves newlines, `>` folds them into spaces.
const (
	blockStringKeep = '|'
	blockStringFold = '>'
)

// scanner walks a Source producing scannedLines.
type scanner struct {
	src   *Source
	diags *Diagnostics
	pos   int // byte offset of the next line start
	line  int // 1-based number of the next line
}

func newScanner(src *Source, diags *Diagnostics) *scanner {
	return &scanner{src: src, diags: diags, line: 1}
}

// atEOF reports whether every line has been consumed.
func (s *scanner) atEOF() bool { return s.pos >= len(s.src.Text) }

// next scans one logical line, joining backslash continuations.
func (s *scanner) next() scannedLine {
	start := s.pos
	startLine := s.line
	text, end := s.physicalLine()
	joined := false

	// Join continuations. A line ending in a single backslash continues onto
	// the next; the continuation's leading whitespace collapses to one space.
	// This exists only so a long `text` argument can wrap, so it is deliberately
	// simple: no continuation inside a block string, no escaped trailing
	// backslash handling beyond the doubled form.
	for strings.HasSuffix(text, `\`) && !strings.HasSuffix(text, `\\`) && end < len(s.src.Text) {
		s.pos = end
		s.line++
		more, moreEnd := s.physicalLine()
		text = strings.TrimRight(strings.TrimSuffix(text, `\`), " \t") + " " + strings.TrimLeft(more, " \t")
		end = moreEnd
		joined = true
	}
	s.pos = end
	s.line++

	ln := scannedLine{number: startLine, joined: joined,
		span: Span{Start: start, End: start + len(text)}}
	if joined {
		// The joined text exists in no single source range, so the span covers
		// every physical line the logical line consumed.
		ln.span = Span{Start: start, End: end}
	}

	indent, indentBytes, ok := s.measureIndent(text, start)
	if !ok {
		// A tab in the indentation is fatal for this line, but scanning
		// continues so that one stray tab does not hide every other error.
		ln.kind = lineBlank
		return ln
	}
	ln.indent = indent

	rest := text[indentBytes:]
	restOffset := start + indentBytes
	if strings.TrimSpace(rest) == "" {
		ln.kind = lineBlank
		return ln
	}
	if commentAt(rest, 0) {
		ln.kind = lineComment
		return ln
	}

	ln.kind = lineNode
	s.scanNode(&ln, rest, restOffset)
	return ln
}

// nextRaw scans one physical line without interpreting it as a node. Block
// string bodies use this: their contents are arbitrary text, and running the
// node scanner over "1. What changed since Friday" would report it as a
// malformed identifier.
func (s *scanner) nextRaw() (line scannedLine, text string) {
	start := s.pos
	startLine := s.line
	raw, end := s.physicalLine()
	s.pos = end
	s.line++

	line = scannedLine{number: startLine, span: Span{Start: start, End: start + len(raw)}}
	indent := 0
	i := 0
	for ; i < len(raw); i++ {
		if raw[i] == ' ' {
			indent++
			continue
		}
		if raw[i] == '\t' {
			// Tabs are still illegal, but inside a block string the body is
			// verbatim text, so this is only about the leading indentation we
			// are about to strip.
			indent++
			continue
		}
		break
	}
	line.indent = indent
	if strings.TrimSpace(raw) == "" {
		line.kind = lineBlank
	} else {
		line.kind = lineNode
	}
	return line, raw
}

// physicalLine returns the text of the line starting at s.pos, without its
// terminator, and the offset just past that terminator.
func (s *scanner) physicalLine() (string, int) {
	text := s.src.Text[s.pos:]
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		return strings.TrimSuffix(text[:i], "\r"), s.pos + i + 1
	}
	return text, len(s.src.Text)
}

// measureIndent counts leading spaces and rejects tabs.
//
// Tabs are a hard error rather than a configurable width because the entire
// class of "it looked aligned in my editor" bugs comes from pretending that
// question has an answer. Tabs inside strings are fine; only leading
// whitespace is policed.
func (s *scanner) measureIndent(text string, base int) (indent, bytes int, ok bool) {
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case ' ':
			indent++
		case '\t':
			s.diags.Errorf(s.src, Span{Start: base + i, End: base + i + 1}, "E002",
				"tab in indentation").
				WithLabel("tab character").
				WithHelp("Pulp indents with spaces only. Run `treekillbot fmt` to convert this file.")
			return 0, 0, false
		default:
			return indent, i, true
		}
	}
	return indent, len(text), true
}

// scanNode extracts the identifier and the argument span from a node line.
func (s *scanner) scanNode(ln *scannedLine, rest string, offset int) {
	nameLen := identLength(rest)
	if nameLen == 0 {
		s.diags.Errorf(s.src, Span{Start: offset, End: offset + 1}, "E001",
			"expected a name at the start of this line").
			WithLabel("not a name").
			WithHelp("A line must begin with a name like `panel`, `text` or `font-size`.")
		return
	}
	ln.nameSpan = Span{Start: offset, End: offset + nameLen}
	ln.name = rest[:nameLen]

	after := rest[nameLen:]
	afterOffset := offset + nameLen
	if after == "" {
		return
	}

	switch {
	case after[0] == ':':
		ln.colon = true
		after, afterOffset = after[1:], afterOffset+1
	case after[0] == ' ':
		// Bare form; the argument begins after the whitespace.
	default:
		s.diags.Errorf(s.src, Span{Start: afterOffset, End: afterOffset + 1}, "E001",
			"unexpected %q after the name %q", after[0], ln.name).
			WithLabel("unexpected character").
			WithHelp("Write `%s: value` or `%s value`.", ln.name, ln.name)
		return
	}

	arg, argOffset := trimArgument(after, afterOffset)
	if arg == "" {
		return
	}
	ln.hasArg = true
	ln.argText = arg
	ln.argSpan = Span{Start: argOffset, End: argOffset + len(arg)}
	if len(arg) == 1 && (arg[0] == blockStringKeep || arg[0] == blockStringFold) {
		ln.blockArg = arg[0]
	}
}

// trimArgument strips the inline comment and surrounding whitespace from an
// argument, returning the remaining text and its byte offset in the source.
func trimArgument(s string, offset int) (string, int) {
	if cut := inlineCommentIndex(s); cut >= 0 {
		s = s[:cut]
	}
	lead := len(s) - len(strings.TrimLeft(s, " \t"))
	s = strings.TrimSpace(s)
	return s, offset + lead
}

// identLength returns the length of the identifier at the start of s, or zero.
// Identifiers are letters, digits, hyphens and underscores, and must start with
// a letter or underscore.
func identLength(s string) int {
	if s == "" {
		return 0
	}
	if !isIdentStart(s[0]) {
		return 0
	}
	i := 1
	for i < len(s) && isIdentPart(s[i]) {
		i++
	}
	return i
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9') || c == '-'
}

// inlineCommentIndex finds where a trailing comment starts in s, or -1.
//
// Two rules keep `#` usable both as a comment marker and as the first character
// of a colour literal:
//
//  1. A `#` only starts a comment when it is at the start of the text or
//     preceded by whitespace. So `text: item#5` keeps its hash.
//  2. A `#` followed by exactly 3, 4, 6 or 8 hex digits and then a terminator
//     is a colour, not a comment. So `background: #ddd  # muted` reads as a
//     colour followed by a comment.
//
// The one collision left is a comment whose first word is eight hex digits, as
// in `#deadbeef`. `treekillbot fmt` writes `# ` on every comment it touches,
// which makes that unreachable in formatted files, and the resulting error
// names the case explicitly.
func inlineCommentIndex(s string) int {
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == '\\' && quote == '"' {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			continue
		}
		if c == '#' && commentAt(s, i) {
			return i
		}
	}
	return -1
}

// commentAt reports whether the `#` at index i begins a comment.
func commentAt(s string, i int) bool {
	if i >= len(s) || s[i] != '#' {
		return false
	}
	if i > 0 && s[i-1] != ' ' && s[i-1] != '\t' {
		return false
	}
	return !hexColorAt(s, i)
}

// hexColorAt reports whether a hex colour literal starts at index i.
func hexColorAt(s string, i int) bool {
	n := 0
	for i+1+n < len(s) && isHexDigit(s[i+1+n]) {
		n++
	}
	if n != 3 && n != 4 && n != 6 && n != 8 {
		return false
	}
	end := i + 1 + n
	if end >= len(s) {
		return true
	}
	switch s[end] {
	case ' ', '\t', ',', ')', '"', '\'', ';':
		return true
	}
	return false
}

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
