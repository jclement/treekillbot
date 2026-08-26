package ui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/jclement/treekillbot/internal/pulp"
)

const sample = `section
  panel "Notes"
    line-style: ruled
    height: 200
`

// diagnose produces a real diagnostic from real source, so the tests exercise
// the same spans the compiler would produce rather than hand-built ones.
func diagnose(t *testing.T, src, code, message, label string, span pulp.Span) *pulp.Diagnostic {
	t.Helper()
	source := pulp.NewSource("weekly.pulp", src)
	return &pulp.Diagnostic{
		Code: code, Severity: pulp.SeverityError, Message: message,
		Label: label, Span: span, Source: source,
	}
}

// spanOf finds the byte range of a substring, which is how these tests name the
// token they expect underlined.
func spanOf(src, text string) pulp.Span {
	i := strings.Index(src, text)
	return pulp.Span{Start: i, End: i + len(text)}
}

// Non-TTY output is the format compilers have used for decades, so editors and
// CI parse it for free. It must contain no escape sequences at all.
func TestPlainOutputIsGCCFormat(t *testing.T) {
	var buf bytes.Buffer
	console := &Console{Out: &buf, Err: &buf, Color: false}
	console.PrintDiagnostic(diagnose(t, sample, "E021",
		"`height` needs a length, but `200` has no unit", "add a unit", spanOf(sample, "200")))

	got := buf.String()
	if strings.ContainsRune(got, 0x1b) {
		t.Fatalf("plain output must contain no escape sequences: %q", got)
	}
	if !strings.HasPrefix(got, "weekly.pulp:4:13: error: E021 ") {
		t.Fatalf("got %q, want a gcc-style prefix", got)
	}
}

// The underline must span the offending token exactly. A caret under a
// ten-character property name reads as an off-by-one, which is why this is an
// underline in the first place.
func TestUnderlineCoversTheTokenExactly(t *testing.T) {
	var buf bytes.Buffer
	console := &Console{Out: &buf, Err: &buf, Color: true, Palette: NewPalette(true, true), Width: 80}
	console.PrintDiagnostic(diagnose(t, sample, "E021", "needs a unit", "add a unit", spanOf(sample, "200")))

	got := stripANSI(buf.String())
	line := findLineContaining(got, "━")
	if line == "" {
		t.Fatalf("no underline in:\n%s", got)
	}
	if count := strings.Count(line, "━"); count != 3 {
		t.Fatalf("underline is %d characters, want 3 (the width of `200`):\n%s", count, line)
	}

	// It must sit under the token: `200` starts at column 13, and the excerpt
	// is prefixed by six spaces of indent plus the gutter.
	source := findLineContaining(got, "height: 200")
	underlineAt := strings.Index(line, "━")
	tokenAt := strings.Index(source, "200")
	if underlineAt != tokenAt {
		t.Fatalf("underline starts at %d but the token is at %d:\n%s\n%s",
			underlineAt, tokenAt, source, line)
	}
}

func TestExcerptShowsSurroundingContext(t *testing.T) {
	var buf bytes.Buffer
	console := &Console{Out: &buf, Err: &buf, Color: true, Palette: NewPalette(true, true), Width: 80}
	console.PrintDiagnostic(diagnose(t, sample, "E021", "needs a unit", "", spanOf(sample, "200")))

	got := stripANSI(buf.String())
	for _, want := range []string{"line-style: ruled", "height: 200"} {
		if !strings.Contains(got, want) {
			t.Errorf("excerpt is missing %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "weekly.pulp:4:13") {
		t.Errorf("missing the location:\n%s", got)
	}
}

// A multi-byte line must not shift the underline: columns are counted in runes.
func TestUnderlineHandlesMultiByteText(t *testing.T) {
	src := "section\n  text \"café — naïve\"\n    height: 200\n"
	var buf bytes.Buffer
	console := &Console{Out: &buf, Err: &buf, Color: true, Palette: NewPalette(true, true), Width: 80}
	console.PrintDiagnostic(diagnose(t, src, "E021", "needs a unit", "", spanOf(src, "200")))

	got := stripANSI(buf.String())
	line := findLineContaining(got, "━")
	source := findLineContaining(got, "height: 200")
	if strings.Index(line, "━") != strings.Index(source, "200") {
		t.Fatalf("underline misaligned on a multi-byte line:\n%s\n%s", source, line)
	}
}

func TestGitHubAnnotations(t *testing.T) {
	var buf bytes.Buffer
	console := &Console{Out: &buf, Err: &buf, Color: false, GitHub: true}
	d := diagnose(t, sample, "W021", "text does not fit", "", spanOf(sample, "200"))
	d.Severity = pulp.SeverityWarning
	d.Help = "Give it more room."
	console.PrintDiagnostic(d)

	got := buf.String()
	if !strings.Contains(got, "::warning file=weekly.pulp,line=4,col=13::W021 text does not fit") {
		t.Fatalf("annotation missing or wrong:\n%s", got)
	}
}

func TestAnnotationEscapesNewlines(t *testing.T) {
	if got := escapeAnnotation("a\nb%c"); got != "a%0Ab%25c" {
		t.Fatalf("escapeAnnotation = %q", got)
	}
}

func TestDiagnosticWithNoSourceStillRenders(t *testing.T) {
	var buf bytes.Buffer
	console := &Console{Out: &buf, Err: &buf, Color: true, Palette: NewPalette(true, true), Width: 80}
	console.PrintDiagnostic(&pulp.Diagnostic{
		Code: "E211", Severity: pulp.SeverityError,
		Message: "a required variable was not supplied",
		Help:    "Pass --var client=…",
	})
	got := stripANSI(buf.String())
	if !strings.Contains(got, "required variable") || !strings.Contains(got, "--var client") {
		t.Fatalf("a source-less diagnostic must still render its message and help:\n%s", got)
	}
}

// Quiet suppresses the summary, never diagnostics: a warning you asked not to
// see is still a warning you needed.
func TestQuietDoesNotSuppressDiagnostics(t *testing.T) {
	var buf bytes.Buffer
	console := &Console{Out: &buf, Err: &buf, Color: false, Quiet: true}
	console.PrintDiagnostic(diagnose(t, sample, "E021", "needs a unit", "", spanOf(sample, "200")))
	if buf.Len() == 0 {
		t.Fatal("--quiet must not silence diagnostics")
	}
}

func TestVerboseGatesInfoOnly(t *testing.T) {
	var buf bytes.Buffer
	console := &Console{Out: &buf, Err: &buf, Color: false}
	console.Info("something")
	if buf.Len() != 0 {
		t.Fatal("Info must say nothing without --verbose")
	}
	console.Verbose = true
	console.Info("something")
	if buf.Len() == 0 {
		t.Fatal("Info must speak under --verbose")
	}
}

// stripANSI removes escape sequences so assertions can be about layout.
func stripANSI(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			inEscape = true
		case inEscape:
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func findLineContaining(text, needle string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return ""
}
