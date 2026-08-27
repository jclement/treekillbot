// Rendering several pages from one document.
//
// A planner template is only useful once: you want the next thirteen weeks, not
// this one. `--repeat 13 --step 1w` compiles the document once per page with the
// date anchor advanced each time, so every page is a different week without the
// document knowing anything about it.
//
// Each page is a full, independent compilation. That costs a few milliseconds
// per page and buys correctness: `{week.number}` and every other built-in
// resolve against that page's own anchor, with no state carried between them
// and nothing to reset.
package pipeline

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jclement/treekillbot/internal/pdfout"
	"github.com/jclement/treekillbot/internal/pulp"
)

// stepUnit is the calendar unit a repeat advances by.
type stepUnit uint8

const (
	stepDays stepUnit = iota
	stepWeeks
	stepMonths
	stepYears
)

// stepOffset is how far one page's anchor sits from the run's anchor.
type stepOffset struct {
	unit  stepUnit
	count int
}

// apply moves a time by the offset.
//
// Months and years go through AddDate rather than a duration, because "one
// month later" is a calendar operation: adding 30 days to 31 January gives a
// date in March, which is not what anyone means by the next month's page.
func (s stepOffset) apply(t time.Time) time.Time {
	if s.count == 0 {
		return t
	}
	switch s.unit {
	case stepWeeks:
		return t.AddDate(0, 0, 7*s.count)
	case stepMonths:
		return t.AddDate(0, s.count, 0)
	case stepYears:
		return t.AddDate(s.count, 0, 0)
	default:
		return t.AddDate(0, 0, s.count)
	}
}

// ParseStep reads the --step form: a count and a unit, as in 1d, 2w, 1m, 1y.
// An empty string means one week, which is what a planner repeats by.
func ParseStep(text string) (stepOffset, error) {
	trimmed := strings.TrimSpace(strings.ToLower(text))
	if trimmed == "" {
		return stepOffset{unit: stepWeeks, count: 1}, nil
	}

	digits := 0
	for digits < len(trimmed) && trimmed[digits] >= '0' && trimmed[digits] <= '9' {
		digits++
	}
	count := 1
	if digits > 0 {
		parsed, err := strconv.Atoi(trimmed[:digits])
		if err != nil || parsed <= 0 {
			return stepOffset{}, fmt.Errorf("the count must be a positive whole number, got %q", text)
		}
		count = parsed
	}

	switch strings.TrimSpace(trimmed[digits:]) {
	case "d", "day", "days":
		return stepOffset{unit: stepDays, count: count}, nil
	case "w", "week", "weeks", "":
		return stepOffset{unit: stepWeeks, count: count}, nil
	case "m", "month", "months":
		return stepOffset{unit: stepMonths, count: count}, nil
	case "y", "year", "years":
		return stepOffset{unit: stepYears, count: count}, nil
	}
	return stepOffset{}, fmt.Errorf("the unit must be d, w, m or y, got %q", text)
}

// ParseSpan reads a --next value: a count and a unit, as in "4w", "30d",
// "3 months". It returns the page count and the per-page step.
//
// This is the ergonomic spelling of --repeat N --step 1<unit>, and it exists
// because pre-printing is the common case and "the next four weeks" should not
// require two flags and a mental multiplication.
func ParseSpan(text string) (count int, step string, err error) {
	offset, err := ParseStep(text)
	if err != nil {
		// The flag the reader typed is the one the message should name.
		return 0, "", fmt.Errorf("--next %s: %w", text, err)
	}
	if offset.count > maxRepeat {
		return 0, "", fmt.Errorf("--next %s is more than the limit of %d pages", text, maxRepeat)
	}
	return offset.count, "1" + offset.unit.suffix(), nil
}

// suffix returns the single-letter spelling of a unit.
func (u stepUnit) suffix() string {
	switch u {
	case stepWeeks:
		return "w"
	case stepMonths:
		return "m"
	case stepYears:
		return "y"
	default:
		return "d"
	}
}

// maxRepeat bounds a run. A thousand pages is already an unreasonable print
// job; past that, a mistyped flag becomes a hang rather than an error.
const maxRepeat = 1000

// BuildDocument renders every page of a run into one PDF.
//
// For a single page this is the same work Build does, plus the PDF writer. The
// loop exists so that the repeated case shares exactly one code path with the
// ordinary one — a separate fast path for `--repeat 1` would be the thing that
// eventually diverged.
func BuildDocument(src *pulp.Source, opts Options) (*Result, error) {
	count := opts.Repeat
	if count <= 0 {
		count = 1
	}
	if count > maxRepeat {
		return nil, fmt.Errorf("--repeat %d is more than the limit of %d pages", count, maxRepeat)
	}
	step, err := ParseStep(opts.Step)
	if err != nil {
		return nil, fmt.Errorf("--step %s: %w", opts.Step, err)
	}

	pdf := pdfout.New(pdfout.Options{
		Title:      opts.Title,
		Author:     opts.Author,
		Creator:    opts.Creator,
		Created:    opts.Created,
		NoCompress: opts.NoCompress,
		Grayscale:  opts.Grayscale,
	})

	var combined *Result
	// --next asks for the periods AFTER this one, so every page shifts a further
	// step along. Printing next week's page on a Friday is the whole reason the
	// flag exists; starting from the week you are already most of the way
	// through would be useless.
	skip := 0
	if opts.StartAtNext {
		skip = 1
	}

	for index := 0; index < count; index++ {
		page := pageContext{
			number:      index + 1,
			count:       count,
			anchorShift: stepOffset{unit: step.unit, count: step.count * (index + skip)},
		}
		// Each page compiles from source with its own scope, so no state — and
		// no mistake — can carry from one page to the next.
		result, err := buildPage(src, StageLayout, opts, page)
		if err != nil {
			return result, err
		}
		if combined == nil {
			combined = result
		} else {
			combined.Diags = append(combined.Diags, result.Diags...)
			combined.Timings.Parse += result.Timings.Parse
			combined.Timings.Validate += result.Timings.Validate
			combined.Timings.Compile += result.Timings.Compile
			combined.Timings.Layout += result.Timings.Layout
		}
		if result.Diags.HasErrors() {
			combined.Diags.Sort()
			return combined, nil
		}

		started := time.Now()
		canvas := pdf.AddPage(pdfout.CustomPageSize(result.PageSize.Width, result.PageSize.Height))
		renderPage(result, canvas, opts)
		combined.Timings.Render += time.Since(started)
	}

	bytes, err := pdf.Bytes()
	if err != nil {
		return combined, fmt.Errorf("writing PDF: %w", err)
	}
	combined.FirstDate = step.applyN(baseAnchor(opts), skip)
	combined.LastDate = step.applyN(baseAnchor(opts), skip+count-1)
	combined.PDF = bytes
	combined.PageCount = pdf.PageCount()
	combined.MissingGlyphs = pdf.MissingGlyphs()
	combined.Diags.Sort()
	return combined, nil
}

// applyN moves a time by the offset repeated n times.
func (s stepOffset) applyN(t time.Time, n int) time.Time {
	return stepOffset{unit: s.unit, count: s.count * n}.apply(t)
}

// baseAnchor is the run's starting date before any per-page shift.
func baseAnchor(opts Options) time.Time {
	if opts.Anchor.IsZero() {
		return time.Now()
	}
	return opts.Anchor
}

// renderPage paints one already-laid-out page.
func renderPage(result *Result, canvas *pdfout.Canvas, opts Options) {
	Render(result.Document.Root, canvas, result.resolver, result.grid, opts)
}

// BuildDocumentFile reads a file and renders it to PDF.
func BuildDocumentFile(path string, opts Options) (*Result, error) {
	text, err := readFile(path)
	if err != nil {
		return nil, err
	}
	return BuildDocument(pulp.NewSource(path, text), opts)
}
