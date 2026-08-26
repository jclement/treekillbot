// The build summary.
//
// This is the thing you see a hundred times a day, so it earns its space by
// answering the questions you actually have — did it work, what did it make,
// how big is it, and where did the time go — in one glance, and by getting out
// of the way entirely when nobody is watching.
package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/jclement/treekillbot/internal/pipeline"
	"github.com/jclement/treekillbot/internal/taglines"
)

// Summary is what to say about a finished build.
type Summary struct {
	Source string
	Output string
	Theme  string
	Date   string

	Result  *pipeline.Result
	Bytes   int
	Fonts   int
	Elapsed time.Duration
	Tagline string
}

// PrintBanner writes the tool's name and version, once per invocation.
func (c *Console) PrintBanner(version string) {
	if c.Quiet || !c.Color {
		return
	}
	fmt.Fprintf(c.Err, "\n  %s %s\n", c.Palette.Brand.Render("🪓 treekillbot"), c.Palette.Dim.Render(version))
}

// PrintSummary writes the result of a build.
func (c *Console) PrintSummary(s Summary) {
	if c.Quiet {
		return
	}
	if !c.Color {
		// One line, on stderr, in a shape a log scraper can read. No box, no
		// colour, no emoji, and never a partially-styled hybrid.
		fmt.Fprintf(c.Err, "treekillbot: built %s (%d page%s, %d bytes, %s)\n",
			s.Output, s.Result.PageCount, plural(s.Result.PageCount), s.Bytes, roundDuration(s.Elapsed))
		return
	}
	p := c.Palette
	var b strings.Builder

	b.WriteString("\n  " + p.Dim.Render(s.Source) + " " + p.Dim.Render("→") + " " + p.Path.Render(s.Output))
	if s.Theme != "" || s.Date != "" {
		var meta []string
		if s.Theme != "" {
			meta = append(meta, "theme "+s.Theme)
		}
		if s.Date != "" {
			meta = append(meta, s.Date)
		}
		b.WriteString("   " + p.Dim.Render(strings.Join(meta, " · ")))
	}
	b.WriteString("\n\n")

	rows := []string{
		p.Heading.Render(s.Output),
		fmt.Sprintf("%d page%s · %s", s.Result.PageCount, plural(s.Result.PageCount), describePage(s.Result)),
		fmt.Sprintf("%s · %d font%s embedded", humanBytes(s.Bytes), s.Fonts, plural(s.Fonts)),
		p.Dim.Render(describeTimings(s.Result.Timings)),
	}
	b.WriteString(c.box(rows))

	b.WriteString("\n  " + p.Success.Render("✓") + " built in " + p.Value.Render(roundDuration(s.Elapsed)))
	if s.Tagline == "" {
		s.Tagline = taglines.Pick(int(s.Elapsed.Nanoseconds()))
	}
	b.WriteString(" " + p.Dim.Render("· "+s.Tagline) + "\n\n")

	fmt.Fprint(c.Err, b.String())
}

// box draws rounded box-drawing characters around a set of rows, padding them
// to a common width measured in visible cells rather than bytes — styled text
// carries escape sequences that must not count toward the width.
func (c *Console) box(rows []string) string {
	widest := 0
	for _, row := range rows {
		if w := visibleWidth(row); w > widest {
			widest = w
		}
	}
	inner := widest + 4

	var b strings.Builder
	b.WriteString("  " + c.Palette.Box.Render("╭"+strings.Repeat("─", inner)+"╮") + "\n")
	for _, row := range rows {
		pad := strings.Repeat(" ", inner-visibleWidth(row)-4)
		b.WriteString("  " + c.Palette.Box.Render("│") + "  " + row + pad + "  " + c.Palette.Box.Render("│") + "\n")
	}
	b.WriteString("  " + c.Palette.Box.Render("╰"+strings.Repeat("─", inner)+"╯") + "\n")
	return b.String()
}

// describePage renders the trim size in whichever unit the size was named in,
// because someone who asked for A4 wants to read millimetres back.
func describePage(r *pipeline.Result) string {
	w, h := r.PageSize.Width, r.PageSize.Height
	orientation := "portrait"
	if w > h {
		orientation = "landscape"
	}
	name := r.PageSize.Name
	if name == "" {
		name = "custom"
	}
	if isMetricSize(name) {
		return fmt.Sprintf("%.0f × %.0f mm · %s %s", w.Mm(), h.Mm(), name, orientation)
	}
	return fmt.Sprintf("%s × %s in · %s %s", trimFloat(w.Inches()), trimFloat(h.Inches()), name, orientation)
}

// isMetricSize reports whether a page-size name belongs to an ISO series.
func isMetricSize(name string) bool {
	if name == "" {
		return false
	}
	switch name[0] {
	case 'a', 'b':
		return len(name) >= 2 && name[1] >= '0' && name[1] <= '9'
	}
	switch name {
	case "pocket", "travellers":
		return true
	}
	return false
}

// describeTimings shows where the time went, omitting stages too fast to have
// an opinion about.
func describeTimings(t pipeline.Timings) string {
	stages := []struct {
		name string
		d    time.Duration
	}{
		{"parse", t.Parse},
		{"check", t.Validate},
		{"compile", t.Compile},
		{"layout", t.Layout},
		{"render", t.Render},
	}
	var parts []string
	for _, s := range stages {
		parts = append(parts, s.name+" "+roundDuration(s.d))
	}
	return strings.Join(parts, " · ")
}

// roundDuration formats a duration at a precision a human can act on.
func roundDuration(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return fmt.Sprintf("%dµs", d.Microseconds())
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	default:
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
}

// humanBytes formats a file size.
func humanBytes(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.0f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}

func trimFloat(v float64) string {
	s := fmt.Sprintf("%.2f", v)
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// visibleWidth counts display cells, skipping ANSI escape sequences so a styled
// row lines up inside the box with an unstyled one.
func visibleWidth(s string) int {
	width, inEscape := 0, false
	for _, r := range s {
		switch {
		case r == 0x1b:
			inEscape = true
		case inEscape:
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
		default:
			width++
		}
	}
	return width
}
