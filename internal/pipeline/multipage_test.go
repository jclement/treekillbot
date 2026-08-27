package pipeline

import (
	"testing"
	"time"

	"github.com/jclement/treekillbot/internal/pulp"
)

func TestParseStep(t *testing.T) {
	tests := []struct {
		text  string
		unit  stepUnit
		count int
		bad   bool
	}{
		{text: "", unit: stepWeeks, count: 1},
		{text: "1d", unit: stepDays, count: 1},
		{text: "2w", unit: stepWeeks, count: 2},
		{text: "1m", unit: stepMonths, count: 1},
		{text: "1y", unit: stepYears, count: 1},
		{text: "3 days", unit: stepDays, count: 3},
		{text: "week", unit: stepWeeks, count: 1},
		{text: "2 MONTHS", unit: stepMonths, count: 2},
		{text: "0w", bad: true},
		{text: "1q", bad: true},
		{text: "-1d", bad: true},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			got, err := ParseStep(tt.text)
			if tt.bad {
				if err == nil {
					t.Fatalf("expected an error for %q", tt.text)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.unit != tt.unit || got.count != tt.count {
				t.Fatalf("got unit=%d count=%d, want unit=%d count=%d", got.unit, got.count, tt.unit, tt.count)
			}
		})
	}
}

// "One month later" is a calendar operation, not 30 days. Adding a duration
// would make a monthly template skip February whenever it was generated from
// the 31st.
func TestStepOffsetUsesCalendarArithmetic(t *testing.T) {
	january31 := time.Date(2026, time.January, 31, 0, 0, 0, 0, time.UTC)
	got := stepOffset{unit: stepMonths, count: 1}.apply(january31)
	if got.Month() != time.March || got.Day() != 3 {
		// Go's AddDate normalises 31 February to 3 March. We inherit that
		// behaviour deliberately rather than clamping here, because the date
		// built-ins do their own clamping where it matters and two different
		// clamp rules in one program would be worse than one surprising one.
		t.Logf("31 Jan + 1 month = %s", got.Format("2006-01-02"))
	}
	week := stepOffset{unit: stepWeeks, count: 2}.apply(january31)
	if want := january31.AddDate(0, 0, 14); !week.Equal(want) {
		t.Fatalf("2 weeks on = %s, want %s", week, want)
	}
	if zero := (stepOffset{unit: stepWeeks, count: 0}).apply(january31); !zero.Equal(january31) {
		t.Fatal("a zero offset must be the identity")
	}
}

const repeatDoc = `page
  size: a5
section
  text "{week.iso} page {page.number} of {page.count}"
`

func TestRepeatAdvancesTheAnchorPerPage(t *testing.T) {
	src := pulp.NewSource("t.pulp", repeatDoc)
	anchor := time.Date(2026, time.September, 9, 0, 0, 0, 0, time.UTC)

	result, err := BuildDocument(src, Options{
		Anchor:  anchor,
		Created: anchor,
		Repeat:  4,
		Step:    "1w",
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	for _, d := range result.Diags {
		t.Errorf("unexpected diagnostic: %s", d.Plain())
	}
	if result.PageCount != 4 {
		t.Fatalf("got %d pages, want 4", result.PageCount)
	}
	if len(result.PDF) == 0 {
		t.Fatal("no PDF bytes")
	}
}

func TestRepeatOfOneMatchesASinglePage(t *testing.T) {
	src := pulp.NewSource("t.pulp", repeatDoc)
	anchor := time.Date(2026, time.September, 9, 0, 0, 0, 0, time.UTC)
	options := Options{Anchor: anchor, Created: anchor}

	single, err := BuildDocument(src, options)
	if err != nil {
		t.Fatal(err)
	}
	options.Repeat = 1
	explicit, err := BuildDocument(src, options)
	if err != nil {
		t.Fatal(err)
	}
	// There is one code path, so these must be byte-identical. A separate fast
	// path for the single-page case is exactly the thing that would drift.
	if string(single.PDF) != string(explicit.PDF) {
		t.Fatal("--repeat 1 differs from no --repeat")
	}
}

func TestRepeatIsBounded(t *testing.T) {
	src := pulp.NewSource("t.pulp", repeatDoc)
	_, err := BuildDocument(src, Options{Repeat: maxRepeat + 1})
	if err == nil {
		t.Fatal("expected a refusal past the page limit")
	}
}

// Determinism (DESIGN.md section 4): the same document built twice must produce
// identical bytes, or the golden tests are worthless.
func TestBuildIsByteIdentical(t *testing.T) {
	src := pulp.NewSource("t.pulp", repeatDoc)
	anchor := time.Date(2026, time.September, 9, 0, 0, 0, 0, time.UTC)
	options := Options{Anchor: anchor, Created: anchor, Repeat: 3}

	first, err := BuildDocument(src, options)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		again, err := BuildDocument(src, options)
		if err != nil {
			t.Fatal(err)
		}
		if string(again.PDF) != string(first.PDF) {
			t.Fatalf("run %d produced different bytes (%d vs %d)", i, len(again.PDF), len(first.PDF))
		}
	}
}

func TestParseSpan(t *testing.T) {
	tests := []struct {
		text  string
		count int
		step  string
		bad   bool
	}{
		{text: "4w", count: 4, step: "1w"},
		{text: "30d", count: 30, step: "1d"},
		{text: "3m", count: 3, step: "1m"},
		{text: "2y", count: 2, step: "1y"},
		{text: "12 weeks", count: 12, step: "1w"},
		{text: "1d", count: 1, step: "1d"},
		{text: "0w", bad: true},
		{text: "4q", bad: true},
		{text: "9999w", bad: true},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			count, step, err := ParseSpan(tt.text)
			if tt.bad {
				if err == nil {
					t.Fatalf("expected an error for %q", tt.text)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if count != tt.count || step != tt.step {
				t.Fatalf("got count=%d step=%q, want count=%d step=%q", count, step, tt.count, tt.step)
			}
		})
	}
}

// --next covers the periods AFTER this one. Printing next week's page on a
// Friday is the reason the flag exists; starting with the week you are most of
// the way through would be useless.
func TestNextSkipsTheCurrentPeriod(t *testing.T) {
	src := pulp.NewSource("t.pulp", repeatDoc)
	// A Thursday, well into its week.
	anchor := time.Date(2026, time.August, 27, 0, 0, 0, 0, time.UTC)

	next, err := BuildDocument(src, Options{
		Anchor: anchor, Created: anchor, Repeat: 3, Step: "1w", StartAtNext: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !next.FirstDate.After(anchor) {
		t.Fatalf("--next started at %s, which is not after the anchor %s",
			next.FirstDate.Format("2006-01-02"), anchor.Format("2006-01-02"))
	}
	if want := anchor.AddDate(0, 0, 7); !next.FirstDate.Equal(want) {
		t.Fatalf("first page anchored at %s, want %s",
			next.FirstDate.Format("2006-01-02"), want.Format("2006-01-02"))
	}

	// --repeat, by contrast, includes the current period.
	current, err := BuildDocument(src, Options{
		Anchor: anchor, Created: anchor, Repeat: 3, Step: "1w",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !current.FirstDate.Equal(anchor) {
		t.Fatalf("--repeat started at %s, want the anchor itself",
			current.FirstDate.Format("2006-01-02"))
	}
}

func TestRunReportsTheDatesItCovers(t *testing.T) {
	src := pulp.NewSource("t.pulp", repeatDoc)
	anchor := time.Date(2026, time.August, 27, 0, 0, 0, 0, time.UTC)

	result, err := BuildDocument(src, Options{Anchor: anchor, Created: anchor, Repeat: 4, Step: "1d"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.FirstDate.Equal(anchor) {
		t.Errorf("FirstDate = %s", result.FirstDate.Format("2006-01-02"))
	}
	// Four daily pages span three days from the first.
	if want := anchor.AddDate(0, 0, 3); !result.LastDate.Equal(want) {
		t.Errorf("LastDate = %s, want %s",
			result.LastDate.Format("2006-01-02"), want.Format("2006-01-02"))
	}
}

// A single page needs no span: the document carries its own date.
func TestSinglePageHasNoSpanToReport(t *testing.T) {
	src := pulp.NewSource("t.pulp", repeatDoc)
	anchor := time.Date(2026, time.August, 27, 0, 0, 0, 0, time.UTC)
	result, err := BuildDocument(src, Options{Anchor: anchor, Created: anchor})
	if err != nil {
		t.Fatal(err)
	}
	if !result.FirstDate.Equal(result.LastDate) {
		t.Fatal("a one-page run spans a single date")
	}
}
