package templates

import (
	"strings"
	"testing"
	"time"

	"github.com/jclement/treekillbot/internal/pipeline"
	"github.com/jclement/treekillbot/internal/pulp"
	"github.com/jclement/treekillbot/internal/themes"
)

// anchorDate fixes the date every template is tested against.
//
// It is a Wednesday in a 31-day month that starts on a Tuesday, which is the
// combination that catches the two things templates get wrong: a month grid
// with a partial first row, and a `for day in week.days` loop whose week
// straddles a month boundary.
const anchorDate = "2026-07-15"

// TestBuiltinTemplatesRender is this package's whole reason for existing. Every
// template must render, from scaffold to PDF, with no error diagnostics — that
// is the acceptance test the deliverable is written against, because a starter
// that errors the first time it is run is worse than no starter.
//
// Warnings are reported too and are not a failure on their own; an overflow
// warning here would be, but overflow is an error by default (DESIGN.md D9) so
// it arrives in the loop above.
func TestBuiltinTemplatesRender(t *testing.T) {
	anchor, err := time.Parse("2006-01-02", anchorDate)
	if err != nil {
		t.Fatal(err)
	}

	for _, template := range Available() {
		t.Run(template.Name, func(t *testing.T) {
			source, err := Source(template.Name)
			if err != nil {
				t.Fatalf("Source(%q): %v", template.Name, err)
			}

			src := pulp.NewSource(template.Name+Extension, source)
			result, err := pipeline.Build(src, pipeline.StageRender, pipeline.Options{Anchor: anchor})
			if err != nil {
				t.Fatalf("building %s: %v", template.Name, err)
			}
			for _, d := range result.Diags {
				if d.Severity == pulp.SeverityError {
					t.Errorf("%s", d.Plain())
					if d.Help != "" {
						t.Logf("  help: %s", d.Help)
					}
				}
			}
			if len(result.PDF) == 0 {
				t.Errorf("%s produced an empty PDF", template.Name)
			}
			if len(result.MissingGlyphs) > 0 {
				t.Errorf("%s uses characters no embedded font can draw: %q", template.Name, result.MissingGlyphs)
			}
		})
	}
}

// TestBuiltinTemplatesSurviveEveryTheme is the other half of the promise. A
// template owns structure and a theme owns ink, so swapping the theme must not
// be able to break a document — and the combination that does break, a theme
// that turns a decoration on or resizes a box, is refused by internal/themes
// rather than caught here.
func TestBuiltinTemplatesSurviveEveryTheme(t *testing.T) {
	anchor, err := time.Parse("2006-01-02", anchorDate)
	if err != nil {
		t.Fatal(err)
	}

	for _, theme := range themes.Available() {
		props, err := themes.Load(theme.Name)
		if err != nil {
			t.Fatalf("loading theme %q: %v", theme.Name, err)
		}
		for _, template := range Available() {
			t.Run(theme.Name+"/"+template.Name, func(t *testing.T) {
				source, err := Source(template.Name)
				if err != nil {
					t.Fatal(err)
				}
				src := pulp.NewSource(template.Name+Extension, source)
				result, err := pipeline.Build(src, pipeline.StageRender, pipeline.Options{
					Anchor: anchor,
					Theme:  props,
				})
				if err != nil {
					t.Fatalf("building: %v", err)
				}
				for _, d := range result.Diags {
					if d.Severity == pulp.SeverityError {
						t.Errorf("%s", d.Plain())
					}
				}
			})
		}
	}
}

// TestBuiltinTemplatesAreDescribed pins the listing convention: `templates`
// prints each file's first comment line, so one without a header comment shows
// up as a blank row.
func TestBuiltinTemplatesAreDescribed(t *testing.T) {
	for _, template := range Available() {
		if template.Description == "" {
			t.Errorf("template %q has no description; its first line should be a `# …` sentence", template.Name)
		}
	}
}

// TestExpectedTemplatesExist names the library, so removing one is a decision
// somebody makes on purpose rather than a file that quietly went missing.
func TestExpectedTemplatesExist(t *testing.T) {
	want := []string{"cornell", "daily", "dotgrid", "habit", "meeting", "notes", "todo", "weekly"}
	have := map[string]bool{}
	for _, name := range Names() {
		have[name] = true
	}
	for _, name := range want {
		if !have[name] {
			t.Errorf("template %q is missing", name)
		}
	}
}

func TestAvailableIsSorted(t *testing.T) {
	names := Names()
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Fatalf("Available() is not sorted: %q came before %q", names[i-1], names[i])
		}
	}
}

func TestSourceUnknownNameSuggests(t *testing.T) {
	_, err := Source("weekley")
	if err == nil {
		t.Fatal("Source(\"weekley\") succeeded; want an unknown-template error")
	}
	if !strings.Contains(err.Error(), "weekly") {
		t.Errorf("error does not suggest `weekly`: %v", err)
	}
}

// TestTemplatesUseTheDateBuiltins keeps the starters from becoming static
// pages. The date machinery is the best thing this tool does and a new user
// should meet it in the first file they open.
func TestTemplatesUseTheDateBuiltins(t *testing.T) {
	for _, template := range Available() {
		source, err := Source(template.Name)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(source, "{today") && !strings.Contains(source, "{week") &&
			!strings.Contains(source, "{month") && !strings.Contains(source, "{year") {
			t.Errorf("template %q names no date built-in", template.Name)
		}
	}
}
