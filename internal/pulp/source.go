// Source files, byte spans, and the line index that turns a byte offset back
// into a line and column.
//
// Every token, every value inside an argument, and every node carries a Span.
// That is what lets a parse error put a caret under the offending *unit* rather
// than under the whole line — the difference between "something is wrong on
// line 27" and "`200` needs a unit". Spans are byte offsets into the original
// source, so they survive every later transformation.
package pulp

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// Span is a half-open byte range [Start, End) in a Source.
type Span struct {
	Start, End int
}

// Len returns the span's width in bytes.
func (s Span) Len() int { return s.End - s.Start }

// IsZero reports whether the span refers to nothing. A zero span is used for
// synthesised nodes that have no textual origin.
func (s Span) IsZero() bool { return s.Start == 0 && s.End == 0 }

// Sub returns a span covering a sub-range of s, given offsets relative to
// s.Start. Used when a value lexer runs over an argument and needs to report
// positions in whole-file coordinates.
func (s Span) Sub(relStart, relEnd int) Span {
	return Span{Start: s.Start + relStart, End: s.Start + relEnd}
}

// Join returns the smallest span covering both inputs, ignoring zero spans so
// that joining with an absent span is a no-op.
func (s Span) Join(o Span) Span {
	if s.IsZero() {
		return o
	}
	if o.IsZero() {
		return s
	}
	if o.Start < s.Start {
		s.Start = o.Start
	}
	if o.End > s.End {
		s.End = o.End
	}
	return s
}

// Position is a human-facing location: 1-based line, 1-based column counted in
// runes (not bytes, so a caret lands correctly under text containing accents or
// CJK), and the byte offset it came from.
type Position struct {
	Line, Column, Offset int
}

// Source is one parsed file, retained for the lifetime of a render so that
// diagnostics discovered late — an overflow during layout, an unresolved
// variable during render — can still quote the line that caused them.
type Source struct {
	Name string // as the user typed it, or "<stdin>"
	Text string

	// lineStarts[i] is the byte offset of line i+1. Built once at construction;
	// a binary search over it turns any offset into a Position.
	lineStarts []int
}

// NewSource indexes a file's line starts for position lookup.
func NewSource(name, text string) *Source {
	starts := []int{0}
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return &Source{Name: name, Text: text, lineStarts: starts}
}

// LineCount returns the number of lines in the source.
func (s *Source) LineCount() int { return len(s.lineStarts) }

// Position converts a byte offset to a line and column. Offsets past the end of
// the file clamp to the last position, so a diagnostic about a truncated file
// still points somewhere sensible instead of panicking.
func (s *Source) Position(offset int) Position {
	if offset < 0 {
		offset = 0
	}
	if offset > len(s.Text) {
		offset = len(s.Text)
	}
	// The line containing offset is the last one whose start is <= offset.
	line := sort.Search(len(s.lineStarts), func(i int) bool {
		return s.lineStarts[i] > offset
	})
	if line == 0 {
		line = 1
	}
	start := s.lineStarts[line-1]
	column := utf8.RuneCountInString(s.Text[start:offset]) + 1
	return Position{Line: line, Column: column, Offset: offset}
}

// Line returns the text of a 1-based line number, without its newline. An
// out-of-range line yields the empty string rather than panicking, because
// diagnostics must never be the thing that crashes the program.
func (s *Source) Line(n int) string {
	if n < 1 || n > len(s.lineStarts) {
		return ""
	}
	start := s.lineStarts[n-1]
	end := len(s.Text)
	if n < len(s.lineStarts) {
		end = s.lineStarts[n] - 1
	}
	if end > 0 && end <= len(s.Text) && end > start && s.Text[end-1] == '\r' {
		end--
	}
	if start > end {
		return ""
	}
	return s.Text[start:end]
}

// SpanText returns the source text a span covers.
func (s *Source) SpanText(sp Span) string {
	if sp.Start < 0 || sp.End > len(s.Text) || sp.Start > sp.End {
		return ""
	}
	return s.Text[sp.Start:sp.End]
}

// ExpandTabs replaces tabs with spaces for display purposes only. Tabs are
// illegal in Pulp's leading whitespace, but they are legal inside strings, and
// a tab in a quoted string would otherwise misalign the caret under it.
func ExpandTabs(line string, width int) string {
	if !strings.ContainsRune(line, '\t') {
		return line
	}
	var b strings.Builder
	col := 0
	for _, r := range line {
		if r == '\t' {
			n := width - col%width
			b.WriteString(strings.Repeat(" ", n))
			col += n
			continue
		}
		b.WriteRune(r)
		col++
	}
	return b.String()
}
