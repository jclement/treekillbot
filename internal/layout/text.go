// Text wrapping and baseline placement.
//
// Line breaking is greedy, not Knuth–Plass. That is a deliberate downgrade: an
// algorithm whose output shifts when you tune a penalty constant makes a poor
// basis for golden files, and the paragraphs on a planner page are short
// enough that global optimisation would rarely change a break anyway. What we
// borrow from TeX is not the algorithm but the idea of refusing a bad line:
// a justified line that would need absurd word spacing falls back to ragged
// right rather than being set with rivers through it.
package layout

import (
	"strings"
	"unicode"

	"github.com/jclement/treekillbot/internal/fonts"
	"github.com/jclement/treekillbot/internal/geom"
)

// Line is one laid-out line of text.
type Line struct {
	Text  string
	Width geom.Tick
	// Baseline is the distance from the top of the text block to this line's
	// baseline. Text is positioned from its baseline, not its top, because that
	// is what makes two runs at different sizes sit on the same line.
	Baseline geom.Tick
	// Justify is set when this line should be set to the full measure. The last
	// line of a justified paragraph never is.
	Justify bool
}

// TextLayout is the result of wrapping a string to a width.
type TextLayout struct {
	Lines      []Line
	Height     geom.Tick
	LineHeight geom.Tick
	SizeQpt    int32
	Face       *fonts.Face
	Tracking   geom.Tick

	// Shrunk records that auto-shrink reduced the size to make the text fit,
	// which always deserves a warning: it means the document asked for
	// something that did not fit and got something else.
	Shrunk bool
	// Clipped records that text was dropped.
	Clipped bool
}

// FitHeight is what decides whether text fits its box: the sum of its line
// boxes, which is exactly the number Measure reported to the layout engine.
//
// Using the same quantity for both is the whole point, and it took three
// attempts to see that. Judging by the last baseline instead looks more precise
// and is worse: with `line-height: 1.0` the final baseline sits BELOW the
// line-box total, so text allotted exactly its own natural height was reported
// as clipped. Judging by ink extent (baseline plus the face's nominal descent)
// warned on overshoots of a hundredth of a point, because a box sized to its
// text is over by exactly the descent — and half the strings in question have
// no descenders at all.
//
// What actually works is the last line's baseline, less one descent of slack.
// In words: text is clipped when a line's baseline falls more than a
// descender's depth below the box — which is precisely when a whole line has
// been lost, and is what a person means by "it does not fit". A descender
// grazing the bottom edge is not that.
func (t TextLayout) FitHeight() geom.Tick {
	if len(t.Lines) == 0 || t.Face == nil {
		return 0
	}
	depth := t.Lines[len(t.Lines)-1].Baseline - t.Face.Descent(t.SizeQpt)
	if depth < 0 {
		return 0
	}
	return depth
}

// InkHeight is how far down the block's ink can reach: the last baseline plus
// the face's descent. It is reported in diagnostics, where the full extent is
// the more useful number to act on, but it is not what decides fit.
func (t TextLayout) InkHeight() geom.Tick {
	if len(t.Lines) == 0 || t.Face == nil {
		return 0
	}
	last := t.Lines[len(t.Lines)-1]
	return last.Baseline + t.Face.Descent(t.SizeQpt)
}

// TextStyle is the subset of resolved properties that affects wrapping.
type TextStyle struct {
	Face       *fonts.Face
	SizeQpt    int32
	LineHeight float64 // multiple of size; zero means use the font's natural leading
	Tracking   geom.Tick
	Align      string
	Wrap       bool
	// AutoShrink is the smallest fraction of SizeQpt the text may shrink to.
	// Zero disables shrinking.
	AutoShrink float64
}

// minFontQpt is the floor for auto-shrink, in quarter-points. Below 6pt a form
// stops being fillable by hand, so shrinking past it trades one problem for a
// worse one.
const minFontQpt = 24

// lineHeightFor returns the line box height for a style at a given size.
func lineHeightFor(st TextStyle, sizeQpt int32) geom.Tick {
	if st.Face == nil {
		return geom.Tick(sizeQpt) * geom.TicksPerPt / 4
	}
	if st.LineHeight <= 0 {
		return st.Face.NaturalLineHeight(sizeQpt)
	}
	base := geom.Tick(int64(sizeQpt) * int64(geom.TicksPerPt) / 4)
	return geom.Tick(float64(base)*st.LineHeight + 0.5)
}

// WrapText lays out a string to a measure, shrinking to fit a height when the
// style allows it.
//
// maxHeight of zero means unbounded, which is the normal case: a box that sizes
// itself to its text has no height to fit into yet.
func WrapText(text string, st TextStyle, measure, maxHeight geom.Tick) TextLayout {
	if text == "" || st.Face == nil || measure <= 0 {
		return TextLayout{SizeQpt: st.SizeQpt, Face: st.Face, LineHeight: lineHeightFor(st, st.SizeQpt)}
	}

	layout := wrapAt(text, st, measure, st.SizeQpt)
	if maxHeight <= 0 || layout.FitHeight() <= maxHeight {
		return layout
	}
	if st.AutoShrink <= 0 {
		// Shrinking is off, so the text simply does not fit. Reporting that is
		// not optional: returning early here made over-long text silently
		// overflow its box, which is the one outcome DESIGN.md D9 exists to
		// prevent.
		layout.Clipped = true
		return layout
	}

	// Shrink in quarter-point steps. A discrete search space matters: a
	// continuous binary search on a float size would make the chosen size
	// depend on rounding, and two runs could disagree.
	floor := int32(float64(st.SizeQpt) * st.AutoShrink)
	if floor < minFontQpt {
		floor = minFontQpt
	}
	for size := st.SizeQpt - 1; size >= floor; size-- {
		candidate := wrapAt(text, st, measure, size)
		if candidate.FitHeight() <= maxHeight {
			candidate.Shrunk = true
			return candidate
		}
	}

	smallest := wrapAt(text, st, measure, floor)
	smallest.Shrunk = smallest.SizeQpt != st.SizeQpt
	smallest.Clipped = smallest.FitHeight() > maxHeight
	return smallest
}

// wrapAt performs the greedy wrap at one specific size.
func wrapAt(text string, st TextStyle, measure geom.Tick, sizeQpt int32) TextLayout {
	lineHeight := lineHeightFor(st, sizeQpt)
	out := TextLayout{
		LineHeight: lineHeight,
		SizeQpt:    sizeQpt,
		Face:       st.Face,
		Tracking:   st.Tracking,
	}

	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		if !st.Wrap {
			lines = append(lines, paragraph)
			continue
		}
		lines = append(lines, wrapParagraph(paragraph, st.Face, sizeQpt, st.Tracking, measure)...)
	}

	// The first baseline sits an ascent below the top of its line box, with the
	// leading split evenly above and below. Placing it at top+fontSize instead
	// — the tempting shortcut — puts short text visibly too low and makes two
	// adjacent panels at different sizes fail to align.
	ascent := st.Face.Ascent(sizeQpt)
	descent := st.Face.Descent(sizeQpt)
	halfLead := (lineHeight - ascent - descent) / 2
	if halfLead < 0 {
		halfLead = 0
	}

	out.Lines = make([]Line, 0, len(lines))
	for i, text := range lines {
		out.Lines = append(out.Lines, Line{
			Text:     text,
			Width:    st.Face.Width(text, sizeQpt, st.Tracking),
			Baseline: geom.Tick(i)*lineHeight + halfLead + ascent,
			Justify:  st.Align == "justify" && i < len(lines)-1 && text != "",
		})
	}
	out.Height = geom.Tick(len(out.Lines)) * lineHeight
	return out
}

// wrapParagraph greedily breaks one paragraph into lines that fit the measure.
func wrapParagraph(text string, face *fonts.Face, sizeQpt int32, tracking, measure geom.Tick) []string {
	words := splitWords(text)
	if len(words) == 0 {
		return []string{""}
	}

	var (
		lines   []string
		current strings.Builder
	)
	flush := func() {
		lines = append(lines, current.String())
		current.Reset()
	}
	for _, word := range words {
		candidate := word
		if current.Len() > 0 {
			candidate = current.String() + " " + word
		}
		if face.Width(candidate, sizeQpt, tracking) <= measure {
			current.Reset()
			current.WriteString(candidate)
			continue
		}
		if current.Len() > 0 {
			flush()
		}
		// A single word too long for the measure is broken by character. The
		// alternative — letting it overhang — puts ink outside the box it was
		// measured into, and on a bordered form that reads as a bug.
		if face.Width(word, sizeQpt, tracking) > measure {
			for _, piece := range breakWord(word, face, sizeQpt, tracking, measure) {
				if current.Len() > 0 {
					flush()
				}
				current.WriteString(piece)
			}
			continue
		}
		current.WriteString(word)
	}
	if current.Len() > 0 || len(lines) == 0 {
		flush()
	}
	return lines
}

// splitWords breaks on whitespace, preserving nothing but the words themselves.
func splitWords(text string) []string {
	return strings.FieldsFunc(text, unicode.IsSpace)
}

// breakWord splits an over-long word into measure-sized pieces.
func breakWord(word string, face *fonts.Face, sizeQpt int32, tracking, measure geom.Tick) []string {
	var (
		pieces  []string
		current strings.Builder
	)
	for _, r := range word {
		candidate := current.String() + string(r)
		if current.Len() > 0 && face.Width(candidate, sizeQpt, tracking) > measure {
			pieces = append(pieces, current.String())
			current.Reset()
		}
		current.WriteRune(r)
	}
	if current.Len() > 0 {
		pieces = append(pieces, current.String())
	}
	return pieces
}

// AlignOffset returns how far to move a line of the given width from the left
// edge of a measure, for a horizontal alignment.
func AlignOffset(align string, lineWidth, measure geom.Tick) geom.Tick {
	switch align {
	case "right":
		return measure - lineWidth
	case "center":
		// Integer halving keeps this exact and reproducible; the half-tick a
		// centred odd width loses is 1/32pt, which no printer resolves.
		return (measure - lineWidth) / 2
	}
	return 0
}

// VAlignOffset returns how far down to move a block of the given height inside
// a box, for a vertical alignment.
func VAlignOffset(valign string, blockHeight, boxHeight geom.Tick) geom.Tick {
	switch valign {
	case "middle", "center":
		return (boxHeight - blockHeight) / 2
	case "bottom":
		return boxHeight - blockHeight
	}
	return 0
}
