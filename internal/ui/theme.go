// Terminal styling.
//
// Two rules govern everything in this package, and they are in tension only if
// you forget which is which:
//
//   - On a terminal, output should be a pleasure to read: colour, a box, an
//     underline under the exact token that is wrong.
//   - When nobody is watching — a pipe, a CI log, an editor's :make — output is
//     plain text in a format tools already parse, with no escape codes at all.
//
// The branch is decided once, here, from whether stdout is a terminal. Nothing
// downstream asks again, and nothing renders "partially styled".
package ui

import (
	"fmt"
	"io"
	"os"

	"charm.land/lipgloss/v2"
	"golang.org/x/term"
)

// Palette is the set of styles the CLI draws with. Colours are adaptive so the
// output survives a light terminal, which the house style asks for and which
// most tools get wrong.
type Palette struct {
	Brand     lipgloss.Style
	Heading   lipgloss.Style
	Dim       lipgloss.Style
	Path      lipgloss.Style
	Success   lipgloss.Style
	Warning   lipgloss.Style
	Error     lipgloss.Style
	Note      lipgloss.Style
	Code      lipgloss.Style
	Underline lipgloss.Style
	Gutter    lipgloss.Style
	Box       lipgloss.Style
	Value     lipgloss.Style
}

// The palette is defined as light/dark pairs and resolved once against the
// terminal's actual background. Lipgloss v2 dropped the self-resolving adaptive
// colour in favour of this explicit form, which is better: the decision is made
// once at startup rather than re-queried on every style application.
//
// The brand colour is the orange of a lumber crayon, which is the only joke
// this palette makes.
const (
	brandLight, brandDark     = "#b0530a", "#ff8a3d"
	textLight, textDark       = "#1c1c1c", "#e6e6e6"
	dimLight, dimDark         = "#767676", "#8a8a8a"
	successLight, successDark = "#1a7f37", "#3fb950"
	warningLight, warningDark = "#9a6700", "#d29922"
	errorLight, errorDark     = "#cf222e", "#f85149"
	accentLight, accentDark   = "#0969da", "#58a6ff"
)

// NewPalette returns the styles for a given colour mode. isDark selects the
// half of each pair that will be legible on this terminal.
func NewPalette(color, isDark bool) Palette {
	if !color {
		return Palette{} // every zero Style renders its input unchanged
	}
	pick := lipgloss.LightDark(isDark)
	brand := pick(lipgloss.Color(brandLight), lipgloss.Color(brandDark))
	text := pick(lipgloss.Color(textLight), lipgloss.Color(textDark))
	dim := pick(lipgloss.Color(dimLight), lipgloss.Color(dimDark))
	success := pick(lipgloss.Color(successLight), lipgloss.Color(successDark))
	warning := pick(lipgloss.Color(warningLight), lipgloss.Color(warningDark))
	failure := pick(lipgloss.Color(errorLight), lipgloss.Color(errorDark))
	accent := pick(lipgloss.Color(accentLight), lipgloss.Color(accentDark))

	return Palette{
		Brand:     lipgloss.NewStyle().Foreground(brand).Bold(true),
		Heading:   lipgloss.NewStyle().Foreground(text).Bold(true),
		Dim:       lipgloss.NewStyle().Foreground(dim),
		Path:      lipgloss.NewStyle().Foreground(accent),
		Success:   lipgloss.NewStyle().Foreground(success).Bold(true),
		Warning:   lipgloss.NewStyle().Foreground(warning).Bold(true),
		Error:     lipgloss.NewStyle().Foreground(failure).Bold(true),
		Note:      lipgloss.NewStyle().Foreground(accent),
		Code:      lipgloss.NewStyle().Foreground(text),
		Underline: lipgloss.NewStyle().Foreground(failure).Bold(true),
		Gutter:    lipgloss.NewStyle().Foreground(dim),
		Box:       lipgloss.NewStyle().Foreground(dim),
		Value:     lipgloss.NewStyle().Foreground(brand),
	}
}

// terminalIsDark asks the terminal for its background colour, falling back to
// dark. The fallback matters: a wrong guess of "light" on a dark terminal is
// unreadable, whereas the reverse is merely a little low-contrast.
func terminalIsDark() bool {
	bg, err := lipgloss.BackgroundColor(os.Stdin, os.Stdout)
	if err != nil || bg == nil {
		return true
	}
	r, g, b, _ := bg.RGBA()
	luminance := (0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)) / 65535
	return luminance < 0.5
}

// Console is where the CLI writes human output.
//
// Everything human goes to stderr, including the summary box, even on a
// terminal. That is what lets `treekillbot build doc.pulp -o - > out.pdf` send
// a PDF down the pipe and still show you the summary — the artifact owns
// stdout, and nothing else may touch it.
type Console struct {
	Out     io.Writer // the artifact or requested data
	Err     io.Writer // everything human
	Palette Palette
	Color   bool
	// Quiet suppresses the summary but never suppresses diagnostics: a warning
	// you asked not to see is still a warning you needed.
	Quiet   bool
	Verbose bool
	// GitHub emits ::warning and ::error annotations alongside plain output.
	GitHub bool
	Width  int
}

// NewConsole builds a console, deciding the colour mode from the environment.
func NewConsole(forceNoColor bool) *Console {
	color := shouldUseColor(forceNoColor)
	width := 0
	if color {
		if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
			width = w
		}
	}
	if width <= 0 {
		width = 80
	}
	return &Console{
		Out:     os.Stdout,
		Err:     os.Stderr,
		Palette: NewPalette(color, color && terminalIsDark()),
		Color:   color,
		GitHub:  os.Getenv("GITHUB_ACTIONS") == "true",
		Width:   width,
	}
}

// shouldUseColor decides whether to emit escape sequences.
//
// CI forces plain output even when a terminal is attached, because a CI log is
// read later by a person scrolling a web page and by tools that do not decode
// ANSI. NO_COLOR is honoured because it is the standard and refusing it is
// rude.
func shouldUseColor(forceNoColor bool) bool {
	if forceNoColor {
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("CI") == "true" || os.Getenv("GITHUB_ACTIONS") == "true" {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	return term.IsTerminal(int(os.Stderr.Fd()))
}

// StdoutIsTerminal reports whether stdout is a terminal, which is what decides
// whether writing a PDF to it would be a mistake.
func StdoutIsTerminal() bool { return term.IsTerminal(int(os.Stdout.Fd())) }

// Fatal reports an error that stopped the run. It is separate from the
// diagnostic path because these are the tool's problems — a missing file, a
// bad flag — rather than the document's, and conflating the two makes users
// hunt through their source for something that was never there.
func (c *Console) Fatal(err error) {
	if !c.Color {
		fmt.Fprintf(c.Err, "treekillbot: %v\n", err)
		return
	}
	fmt.Fprintf(c.Err, "\n  %s %v\n\n", c.Palette.Error.Render("✗"), err)
}

// Warn reports something worth knowing that did not stop the run.
func (c *Console) Warn(message string) {
	if !c.Color {
		fmt.Fprintf(c.Err, "treekillbot: warning: %s\n", message)
		return
	}
	fmt.Fprintf(c.Err, "  %s %s\n", c.Palette.Warning.Render("▲"), message)
}

// Info reports progress, and only when asked with --verbose.
func (c *Console) Info(message string) {
	if !c.Verbose {
		return
	}
	if !c.Color {
		fmt.Fprintf(c.Err, "treekillbot: %s\n", message)
		return
	}
	fmt.Fprintf(c.Err, "  %s %s\n", c.Palette.Dim.Render("·"), c.Palette.Dim.Render(message))
}

// PrintFailure closes out a build that produced errors, so the run ends with a
// count rather than trailing off after the last diagnostic.
func (c *Console) PrintFailure(errorCount int) {
	if c.Quiet && !c.Color {
		return
	}
	word := "errors"
	if errorCount == 1 {
		word = "error"
	}
	if !c.Color {
		fmt.Fprintf(c.Err, "treekillbot: %d %s; nothing written\n", errorCount, word)
		return
	}
	fmt.Fprintf(c.Err, "\n  %s %s %s\n\n",
		c.Palette.Error.Render("✗"),
		c.Palette.Error.Render(fmt.Sprintf("%d %s", errorCount, word)),
		c.Palette.Dim.Render("· nothing written"))
}

// Ok reports a clean result.
func (c *Console) Ok(message string) {
	if c.Quiet {
		return
	}
	if !c.Color {
		fmt.Fprintf(c.Err, "treekillbot: %s\n", message)
		return
	}
	fmt.Fprintf(c.Err, "\n  %s %s\n\n", c.Palette.Success.Render("✓"), message)
}
