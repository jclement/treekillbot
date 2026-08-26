// Diagnostics: the errors and warnings treekillbot reports about a document.
//
// Parse errors are a first-class feature of this tool, not an afterthought, so
// a Diagnostic carries everything needed to render a good one: a stable code
// the user can look up, the exact byte span of the offending token, an optional
// short label to sit under the underline, and a separate help line for the
// suggested fix. Rendering is deliberately split out — this file produces the
// plain-text and gcc-style forms that editors and CI parse, while the coloured
// terminal form lives in the CLI, so nothing down here needs to know whether
// stdout is a terminal.
package pulp

import (
	"fmt"
	"sort"
	"strings"
)

// Severity ranks a diagnostic.
type Severity uint8

const (
	// SeverityError means the document cannot be rendered.
	SeverityError Severity = iota
	// SeverityWarning means it can, but something is probably wrong — a clipped
	// panel, an unused variable, a fill too dark to write on.
	SeverityWarning
	// SeverityNote is supporting information attached to another diagnostic.
	SeverityNote
)

// String returns the lowercase word used in gcc-style output.
func (s Severity) String() string {
	switch s {
	case SeverityWarning:
		return "warning"
	case SeverityNote:
		return "note"
	default:
		return "error"
	}
}

// Diagnostic is one problem found in one document.
type Diagnostic struct {
	Code     string // stable, e.g. "E021"; looked up with `treekillbot docs errors E021`
	Severity Severity
	Message  string   // the headline, lowercase, no trailing period
	Span     Span     // what to underline
	Label    string   // short text under the underline, e.g. `add a unit`
	Help     string   // the suggested fix, rendered on its own line
	Notes    []string // extra context lines

	// Source is retained so a diagnostic raised during layout — long after
	// parsing — can still quote the line that caused it.
	Source *Source
}

// Error implements error so a single diagnostic can be returned directly.
func (d *Diagnostic) Error() string { return d.Plain() }

// Position returns the diagnostic's start position, or the zero Position when
// it has no source attached.
func (d *Diagnostic) Position() Position {
	if d.Source == nil {
		return Position{Line: 1, Column: 1}
	}
	return d.Source.Position(d.Span.Start)
}

// Plain renders a one-line description without colour or source quoting.
func (d *Diagnostic) Plain() string {
	if d.Source == nil {
		return fmt.Sprintf("%s: %s", d.Code, d.Message)
	}
	p := d.Position()
	return fmt.Sprintf("%s:%d:%d: %s: %s %s", d.Source.Name, p.Line, p.Column, d.Severity, d.Code, d.Message)
}

// GCC renders the diagnostic in the format compilers have used for decades:
//
//	weekly.pulp:34:5: warning: W021 panel overflows its section
//
// Editors, CI log scrapers and `vim :make` all parse this for free, which is
// why the non-TTY output uses it verbatim rather than something prettier.
func (d *Diagnostic) GCC() string {
	s := d.Plain()
	if d.Help != "" {
		s += "\n" + strings.Repeat(" ", 0) + "  help: " + d.Help
	}
	return s
}

// Diagnostics is an ordered collection of diagnostics.
type Diagnostics []*Diagnostic

// Add appends a diagnostic.
func (ds *Diagnostics) Add(d *Diagnostic) { *ds = append(*ds, d) }

// Errorf appends an error diagnostic at a span.
func (ds *Diagnostics) Errorf(src *Source, span Span, code, format string, args ...any) *Diagnostic {
	d := &Diagnostic{
		Code:     code,
		Severity: SeverityError,
		Message:  fmt.Sprintf(format, args...),
		Span:     span,
		Source:   src,
	}
	*ds = append(*ds, d)
	return d
}

// Warnf appends a warning diagnostic at a span.
func (ds *Diagnostics) Warnf(src *Source, span Span, code, format string, args ...any) *Diagnostic {
	d := &Diagnostic{
		Code:     code,
		Severity: SeverityWarning,
		Message:  fmt.Sprintf(format, args...),
		Span:     span,
		Source:   src,
	}
	*ds = append(*ds, d)
	return d
}

// WithLabel sets the short text rendered under the underline and returns the
// diagnostic, so it can be chained onto an Errorf call.
func (d *Diagnostic) WithLabel(format string, args ...any) *Diagnostic {
	d.Label = fmt.Sprintf(format, args...)
	return d
}

// WithHelp sets the suggested-fix line and returns the diagnostic.
func (d *Diagnostic) WithHelp(format string, args ...any) *Diagnostic {
	d.Help = fmt.Sprintf(format, args...)
	return d
}

// WithNote appends a context line and returns the diagnostic.
func (d *Diagnostic) WithNote(format string, args ...any) *Diagnostic {
	d.Notes = append(d.Notes, fmt.Sprintf(format, args...))
	return d
}

// HasErrors reports whether any diagnostic is fatal.
func (ds Diagnostics) HasErrors() bool {
	for _, d := range ds {
		if d.Severity == SeverityError {
			return true
		}
	}
	return false
}

// Errors returns just the fatal diagnostics.
func (ds Diagnostics) Errors() Diagnostics {
	var out Diagnostics
	for _, d := range ds {
		if d.Severity == SeverityError {
			out = append(out, d)
		}
	}
	return out
}

// Sort orders diagnostics by file, then position, then code. Diagnostics are
// discovered in whatever order the compiler happens to walk the tree, which is
// not the order a human wants to read them or a golden file wants to record
// them.
func (ds Diagnostics) Sort() {
	sort.SliceStable(ds, func(i, j int) bool {
		a, b := ds[i], ds[j]
		an, bn := "", ""
		if a.Source != nil {
			an = a.Source.Name
		}
		if b.Source != nil {
			bn = b.Source.Name
		}
		if an != bn {
			return an < bn
		}
		if a.Span.Start != b.Span.Start {
			return a.Span.Start < b.Span.Start
		}
		return a.Code < b.Code
	})
}

// Err returns the collection as an error when it contains any error-severity
// diagnostic, and nil otherwise, so callers can use the ordinary Go idiom.
func (ds Diagnostics) Err() error {
	if !ds.HasErrors() {
		return nil
	}
	return diagError(ds.Errors())
}

type diagError Diagnostics

func (e diagError) Error() string {
	if len(e) == 1 {
		return e[0].Plain()
	}
	parts := make([]string, 0, len(e))
	for _, d := range e {
		parts = append(parts, d.Plain())
	}
	return strings.Join(parts, "\n")
}

// Diags exposes the underlying diagnostics of an error returned by Err, so a
// caller that wants to render them richly can recover the structured form.
func (e diagError) Diags() Diagnostics { return Diagnostics(e) }

// AsDiagnostics recovers the structured diagnostics from an error produced by
// Diagnostics.Err, reporting false for any other error.
func AsDiagnostics(err error) (Diagnostics, bool) {
	if de, ok := err.(diagError); ok {
		return Diagnostics(de), true
	}
	return nil, false
}
