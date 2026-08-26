package pulp

import "testing"

var properties = []string{
	"line-style", "line-pitch", "line-color", "line-width", "line-inset", "line-height",
	"font", "font-size", "font-weight", "color", "background",
	"width", "height", "padding", "margin", "gap", "border", "border-radius",
	"align", "valign", "title", "title-position",
}

func TestSuggestCatchesTypos(t *testing.T) {
	tests := []struct {
		typo string
		want string
	}{
		{"line-stile", "line-style"},
		{"line-heigth", "line-height"},
		{"witdh", "width"},
		{"colour", "color"},
		{"fnot", "font"},
		{"padding-", "padding"},
		{"bordr", "border"},
		{"aling", "align"},
	}
	for _, tt := range tests {
		t.Run(tt.typo, func(t *testing.T) {
			got := Suggest(tt.typo, properties)
			if len(got) == 0 {
				t.Fatalf("no suggestion for %q", tt.typo)
			}
			if got[0].Name != tt.want {
				t.Fatalf("suggested %q, want %q", got[0].Name, tt.want)
			}
		})
	}
}

// Transposition must cost one edit, not two, or short names fall outside the
// threshold exactly when a suggestion would help most.
func TestTranspositionCostsOneEdit(t *testing.T) {
	if d := damerauLevenshtein("stlye", "style"); d != 1 {
		t.Fatalf("stlye→style = %d edits, want 1", d)
	}
	if d := damerauLevenshtein("witdh", "width"); d != 1 {
		t.Fatalf("witdh→width = %d edits, want 1", d)
	}
}

// A name that matches once case and separators are folded is a convention
// mistake, not a typo, and should be reported with certainty.
func TestConventionMistakesAreExact(t *testing.T) {
	for _, wrong := range []string{"lineStyle", "line_style", "LINE-STYLE", "LineStyle"} {
		got := Suggest(wrong, properties)
		if len(got) != 1 {
			t.Fatalf("%q gave %d suggestions, want exactly 1", wrong, len(got))
		}
		if !got[0].Exact || got[0].Name != "line-style" {
			t.Fatalf("%q gave %+v, want an exact match on line-style", wrong, got[0])
		}
		if help := FormatSuggestions("property", got); help == "" {
			t.Fatal("expected a help line")
		}
	}
}

// A wrong suggestion costs more than none, because the reader tries it.
func TestNoSuggestionWhenNothingIsClose(t *testing.T) {
	for _, nonsense := range []string{"zzzzzzzz", "elephant", "qqq"} {
		if got := Suggest(nonsense, properties); len(got) != 0 {
			t.Fatalf("%q suggested %v, want nothing", nonsense, got)
		}
	}
}

func TestSuggestionsAreCappedAndDeterministic(t *testing.T) {
	first := Suggest("line-styl", properties)
	if len(first) > maxSuggestions {
		t.Fatalf("got %d suggestions, want at most %d", len(first), maxSuggestions)
	}
	for i := 0; i < 100; i++ {
		again := Suggest("line-styl", properties)
		if len(again) != len(first) {
			t.Fatal("suggestion count is not deterministic")
		}
		for j := range first {
			if again[j] != first[j] {
				t.Fatalf("suggestions are not deterministic: %v vs %v", again, first)
			}
		}
	}
}
