// Rendering diagnostics.
//
// A parse error is a first-class output of this tool, not an apology for
// failing. The terminal form quotes the source, underlines the exact token that
// is wrong, and puts the suggested fix on its own line. The non-terminal form
// is the format compilers have used for forty years, so editors and CI parse it
// for free.
package ui

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"

	"github.com/jclement/treekillbot/internal/pulp"
)

// lipglossStyle is an alias so the excerpt renderer can take a style without
// this file's signatures reading as though it owns the styling.
type lipglossStyle = lipgloss.Style

// contextLines is how many lines of source to show either side of the problem.
// One is enough to orient the reader and few enough that ten diagnostics still
// fit on a screen.
const contextLines = 1

// PrintDiagnostics writes every diagnostic in the appropriate format.
func (c *Console) PrintDiagnostics(diags pulp.Diagnostics) {
	for _, d := range diags {
		c.PrintDiagnostic(d)
	}
}

// PrintDiagnostic writes one diagnostic.
func (c *Console) PrintDiagnostic(d *pulp.Diagnostic) {
	if c.GitHub {
		fmt.Fprintln(c.Err, githubAnnotation(d))
	}
	if !c.Color {
		fmt.Fprintln(c.Err, d.Plain())
		if d.Help != "" {
			fmt.Fprintf(c.Err, "  help: %s\n", d.Help)
		}
		for _, note := range d.Notes {
			fmt.Fprintf(c.Err, "  note: %s\n", note)
		}
		return
	}
	fmt.Fprint(c.Err, c.renderDiagnostic(d))
}

// renderDiagnostic produces the styled multi-line form.
func (c *Console) renderDiagnostic(d *pulp.Diagnostic) string {
	p := c.Palette
	var b strings.Builder

	mark, style := "✗", p.Error
	switch d.Severity {
	case pulp.SeverityWarning:
		mark, style = "▲", p.Warning
	case pulp.SeverityNote:
		mark, style = "·", p.Note
	}

	b.WriteString("\n  " + style.Render(mark+" "+d.Message) + "\n")

	if d.Source == nil {
		if d.Help != "" {
			b.WriteString("\n    " + p.Dim.Render("help") + "  " + d.Help + "\n")
		}
		return b.String()
	}

	pos := d.Position()
	location := fmt.Sprintf("%s:%d:%d", d.Source.Name, pos.Line, pos.Column)
	b.WriteString("    " + p.Path.Render(location) + "\n\n")
	b.WriteString(c.renderExcerpt(d, pos, style))

	if d.Help != "" {
		b.WriteString("\n    " + p.Dim.Render("help") + "  " + d.Help + "\n")
	}
	for _, note := range d.Notes {
		b.WriteString("    " + p.Dim.Render("note") + "  " + note + "\n")
	}
	b.WriteString("\n    " + p.Dim.Render(d.Code+" · treekillbot docs errors "+d.Code) + "\n")
	return b.String()
}

// renderExcerpt quotes the offending lines with an underline under the span.
func (c *Console) renderExcerpt(d *pulp.Diagnostic, pos pulp.Position, style lipglossStyle) string {
	p := c.Palette
	var b strings.Builder

	first := pos.Line - contextLines
	if first < 1 {
		first = 1
	}
	last := pos.Line + contextLines
	if last > d.Source.LineCount() {
		last = d.Source.LineCount()
	}
	gutterWidth := len(strconv.Itoa(last))

	for line := first; line <= last; line++ {
		text := pulp.ExpandTabs(d.Source.Line(line), 4)
		number := fmt.Sprintf("%*d", gutterWidth, line)
		b.WriteString("      " + p.Gutter.Render(number+" │ ") + p.Code.Render(text) + "\n")

		if line != pos.Line {
			continue
		}
		if underline := c.underlineFor(d, text, pos, gutterWidth); underline != "" {
			b.WriteString(underline)
		}
	}
	return b.String()
}

// underlineFor builds the marker line beneath the offending token.
//
// The whole token is underlined rather than a single caret placed at its start.
// A caret under a ten-character property name reads as an off-by-one, and the
// reader spends a moment wondering which character is meant; an underline the
// width of the token says "this word" without ambiguity.
func (c *Console) underlineFor(d *pulp.Diagnostic, text string, pos pulp.Position, gutterWidth int) string {
	p := c.Palette
	width := runeWidthOfSpan(d, pos)
	if width <= 0 {
		return ""
	}
	// Columns are 1-based and counted in runes, and tabs were expanded above,
	// so the offset is the display column minus one.
	indent := pos.Column - 1
	if indent < 0 {
		indent = 0
	}
	if indent > utf8.RuneCountInString(text) {
		indent = utf8.RuneCountInString(text)
	}

	marker := strings.Repeat(" ", indent) + strings.Repeat("━", width)
	line := "      " + p.Gutter.Render(strings.Repeat(" ", gutterWidth)+" │ ") + p.Underline.Render(marker)
	if d.Label != "" {
		line += " " + p.Underline.Render(d.Label)
	}
	return line + "\n"
}

// runeWidthOfSpan returns how many runes the diagnostic's span covers on its
// first line, clamped so a span reaching to end-of-file does not draw a mile of
// underline.
func runeWidthOfSpan(d *pulp.Diagnostic, pos pulp.Position) int {
	text := d.Source.SpanText(d.Span)
	if text == "" {
		return 1
	}
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[:i]
	}
	width := utf8.RuneCountInString(pulp.ExpandTabs(text, 4))
	if width < 1 {
		width = 1
	}
	const maxUnderline = 60
	if width > maxUnderline {
		width = maxUnderline
	}
	return width
}

// githubAnnotation renders the workflow-command form that makes a diagnostic
// appear inline on a pull request.
func githubAnnotation(d *pulp.Diagnostic) string {
	level := "error"
	if d.Severity == pulp.SeverityWarning {
		level = "warning"
	}
	if d.Source == nil {
		return fmt.Sprintf("::%s::%s %s", level, d.Code, d.Message)
	}
	pos := d.Position()
	message := d.Code + " " + d.Message
	if d.Help != "" {
		message += " — " + d.Help
	}
	return fmt.Sprintf("::%s file=%s,line=%d,col=%d::%s",
		level, d.Source.Name, pos.Line, pos.Column, escapeAnnotation(message))
}

// escapeAnnotation encodes the characters GitHub's workflow-command parser
// treats specially.
func escapeAnnotation(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(s)
}
