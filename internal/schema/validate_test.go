package schema

import (
	"strings"
	"testing"

	"github.com/jclement/treekillbot/internal/pulp"
)

// check parses and validates a fragment, returning every diagnostic.
func check(t *testing.T, src string) pulp.Diagnostics {
	t.Helper()
	doc, parseDiags := pulp.ParseString("t.pulp", src)
	if parseDiags.HasErrors() {
		t.Fatalf("unexpected parse errors: %v", parseDiags)
	}
	return Validate(doc)
}

// first returns the first diagnostic with the given code, or fails.
func first(t *testing.T, diags pulp.Diagnostics, code string) *pulp.Diagnostic {
	t.Helper()
	for _, d := range diags {
		if d.Code == code {
			return d
		}
	}
	t.Fatalf("no %s diagnostic; got %v", code, diags)
	return nil
}

func TestUnknownPropertySuggests(t *testing.T) {
	diags := check(t, "section\n  panel \"Notes\"\n    line-stile: ruled\n")
	d := first(t, diags, "E101")
	if !strings.Contains(d.Help, "line-style") {
		t.Fatalf("help = %q, want a suggestion of line-style", d.Help)
	}
	if !strings.Contains(d.Message, "line-stile") {
		t.Fatalf("message = %q", d.Message)
	}
}

// The user's own sketch misspells `defaults`. The tool should catch it and say
// what was meant, which is the whole reason the schema resolves names.
func TestSketchTypoIsCaught(t *testing.T) {
	diags := check(t, "defualt:\n   font: robo-mono\n")
	d := first(t, diags, "E101")
	if !strings.Contains(d.Help, "defaults") {
		t.Fatalf("help = %q, want a suggestion of defaults", d.Help)
	}
}

// A bare number where a length belongs is the most likely mistake in the
// language, so it gets a dedicated message with the conversion spelled out.
func TestBareNumberGetsTheBespokeMessage(t *testing.T) {
	diags := check(t, "section\n  panel\n    height: 200\n")
	d := first(t, diags, "E021")
	if !strings.Contains(d.Message, "no unit") {
		t.Fatalf("message = %q", d.Message)
	}
	for _, want := range []string{"200pt", "fill", "auto"} {
		if !strings.Contains(d.Help, want) {
			t.Fatalf("help = %q, want it to mention %q", d.Help, want)
		}
	}
	// The caret must land on `200`, not on the line or the property name.
	if got := d.Source.SpanText(d.Span); got != "200" {
		t.Fatalf("underlined %q, want %q", got, "200")
	}
}

func TestBadEnumSuggestsAValidValue(t *testing.T) {
	diags := check(t, "section\n  panel\n    line-style: rules\n")
	d := first(t, diags, "E111")
	if !strings.Contains(d.Help, "ruled") {
		t.Fatalf("help = %q, want a suggestion of ruled", d.Help)
	}
}

func TestPropertyOnTheWrongElement(t *testing.T) {
	diags := check(t, "section\n  text \"hi\"\n    line-style: ruled\n")
	d := first(t, diags, "E102")
	if !strings.Contains(d.Message, "not a property of `text`") {
		t.Fatalf("message = %q", d.Message)
	}
	if !strings.Contains(d.Help, "panel") {
		t.Fatalf("help = %q, want it to list where line-style does apply", d.Help)
	}
}

func TestCamelCaseIsAConventionErrorNotAGuess(t *testing.T) {
	diags := check(t, "section\n  panel\n    lineStyle: ruled\n")
	d := first(t, diags, "E101")
	if !strings.Contains(d.Help, "kebab-case") {
		t.Fatalf("help = %q, want it to name the convention rather than guess", d.Help)
	}
}

func TestBadColorSuggestsANamedColor(t *testing.T) {
	diags := check(t, "section\n  panel\n    background: gray\n")
	// `gray` is a real CSS colour, so this must NOT error.
	for _, d := range diags {
		if d.Code == "E110" {
			t.Fatalf("gray is a valid colour name, got %s", d.Plain())
		}
	}
	diags = check(t, "section\n  panel\n    background: slategrey2\n")
	d := first(t, diags, "E110")
	if !strings.Contains(d.Help, "slategrey") {
		t.Fatalf("help = %q, want a colour suggestion", d.Help)
	}
}

// D6's cost: line-height and line-pitch are both real, so setting the wrong one
// is silent unless we warn about it.
func TestLineHeightOnARuledPanelWarns(t *testing.T) {
	diags := check(t, "section\n  panel \"Notes\"\n    line-style: ruled\n    line-height: 2\n")
	d := first(t, diags, "W030")
	if d.Severity != pulp.SeverityWarning {
		t.Fatal("should be a warning, not an error")
	}
	if !strings.Contains(d.Label, "line-pitch") {
		t.Fatalf("label = %q, want it to name line-pitch", d.Label)
	}
	// Setting line-pitch explicitly means the author knows the difference.
	quiet := check(t, "section\n  panel\n    line-style: ruled\n    line-pitch: 6mm\n    line-height: 2\n")
	for _, x := range quiet {
		if x.Code == "W030" {
			t.Fatal("no warning expected once line-pitch is set")
		}
	}
}

func TestMissingAndUnexpectedArguments(t *testing.T) {
	diags := check(t, "style\n  font: mono\n")
	if d := first(t, diags, "E120"); !strings.Contains(d.Help, "style <name>") {
		t.Fatalf("help = %q", d.Help)
	}
	diags = check(t, "section \"why\"\n")
	if d := first(t, diags, "E121"); !strings.Contains(d.Message, "does not take an argument") {
		t.Fatalf("message = %q", d.Message)
	}
}

// A whole valid document must be silent. A validator that cries wolf on good
// input is worse than none.
func TestValidDocumentIsClean(t *testing.T) {
	src := `page
  size: letter
  orientation: landscape
  margin: 0.5in

defaults
  font: IBM Plex Mono
  font-size: 8pt
  color: gray(0.1)

defaults panel
  border-width: 0.5pt
  border-color: gray(0.78)
  border-radius: 2pt
  padding: 5pt

style ruled
  line-style: ruled
  line-pitch: 15pt

section
  height: auto
  text "Week 37"
    font-size: 24pt
    align: left

section
  height: fill
  gap: 5pt
  column
    width: 70%
    panel "Notes"
      height: fill
      style: ruled
  column
    width: 30%
    panel "Todo"
      height: 200pt
      line-style: checkbox
      line-pitch: 20pt
`
	diags := check(t, src)
	for _, d := range diags {
		t.Errorf("unexpected diagnostic: %s (help: %s)", d.Plain(), d.Help)
	}
}

func TestSchemaTableIsWellFormed(t *testing.T) {
	seen := make(map[string]PropID)
	for id := PropID(1); id < numProps; id++ {
		def := Def(id)
		if def.Name == "" {
			t.Fatalf("property %d has no name; a gap in the const block will silently shift every id after it", id)
		}
		if prev, dup := seen[def.Name]; dup {
			t.Fatalf("property %q defined twice (ids %d and %d)", def.Name, prev, id)
		}
		seen[def.Name] = id
		if def.Kind == KindEnum && len(def.Enum) == 0 {
			t.Fatalf("property %q is an enum with no values", def.Name)
		}
		if def.Kind == KindEnum && def.Default != "" {
			found := false
			for _, e := range def.Enum {
				if e == def.Default {
					found = true
				}
			}
			if !found {
				t.Fatalf("property %q defaults to %q, which is not one of its values %v", def.Name, def.Default, def.Enum)
			}
		}
	}
}

// Every default must parse as the type it claims to be, or the very first
// render will be wrong in a way nobody thinks to test.
func TestEveryDefaultParsesAsItsOwnType(t *testing.T) {
	for id := PropID(1); id < numProps; id++ {
		def := Def(id)
		if def.Default == "" {
			continue
		}
		// String properties take the rest of the line verbatim, so there is no
		// value lexing to check — mirroring what validator.property does.
		if def.Kind == KindString {
			continue
		}
		src := pulp.NewSource("defaults", def.Default)
		var diags pulp.Diagnostics
		values := pulp.ParseValues(src, pulp.Span{Start: 0, End: len(def.Default)}, def.Default, &diags)
		if diags.HasErrors() {
			t.Errorf("%s: default %q does not parse: %v", def.Name, def.Default, diags)
			continue
		}
		if len(values) == 0 {
			t.Errorf("%s: default %q parsed to nothing", def.Name, def.Default)
			continue
		}
		v := &validator{src: src, diags: &pulp.Diagnostics{}}
		var check pulp.Diagnostics
		v.diags = &check
		v.checkKind(&pulp.Node{Name: def.Name, HasArg: true, Arg: def.Default}, id, def, values)
		if check.HasErrors() {
			t.Errorf("%s: default %q fails its own validation: %v", def.Name, def.Default, check)
		}
	}
}
