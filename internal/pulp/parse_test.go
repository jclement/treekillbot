package pulp

import (
	"strings"
	"testing"
)

// The original sketch this language was designed around, with only the "..."
// elision placeholders removed. If this stops parsing, the central claim of the
// design — that the shape people naturally write is the shape the language
// accepts — has been broken.
const sketch = `defualt:
   font: robo-mono
   font-size: 16pt
   panel-background: #ddd

section:
   text: Day of {date}
       align: right
       size: 16pt
section:
   height: fill
   column:
     width: 70%
     panel: "Notes"
        height: fill
        line-style: thin
     panel: "Todo":
        height: 200pt
        line-style: thin
        line-height: 2 #2x
        line-decoration: checkbox

   column:
      width: 30%
      panel: "things"
        panel-title-position: "left"

section:
   text: Version 1.0
      align: left
`

func TestParsesTheOriginalSketch(t *testing.T) {
	doc, diags := ParseString("sketch.pulp", sketch)
	for _, d := range diags {
		t.Errorf("unexpected diagnostic: %s", d.Plain())
	}

	top := doc.TopLevel()
	wantNames := []string{"defualt", "section", "section", "section"}
	if len(top) != len(wantNames) {
		var got []string
		for _, n := range top {
			got = append(got, n.Name)
		}
		t.Fatalf("top level = %v, want %v", got, wantNames)
	}
	for i, want := range wantNames {
		if top[i].Name != want {
			t.Fatalf("top level %d = %q, want %q", i, top[i].Name, want)
		}
	}

	// Three sibling `section` nodes is the shape YAML could not have held.
	// The middle one carries the columns.
	body := top[2]
	columns := body.ChildrenNamed("column")
	if len(columns) != 2 {
		t.Fatalf("body section has %d columns, want 2", len(columns))
	}

	// `panel: "Notes"` carries an argument AND a child block — the other thing
	// YAML could not have held.
	notes := columns[0].ChildrenNamed("panel")
	if len(notes) != 2 {
		t.Fatalf("first column has %d panels, want 2", len(notes))
	}
	if notes[0].Arg != `"Notes"` {
		t.Fatalf("panel arg = %q, want %q", notes[0].Arg, `"Notes"`)
	}
	if len(notes[0].Children) != 2 {
		t.Fatalf("Notes panel has %d children, want 2", len(notes[0].Children))
	}

	// `line-height: 2 #2x` — the trailing #2x is a comment, not a colour,
	// because "2x" is not a run of 3/4/6/8 hex digits.
	todo := notes[1]
	lh := todo.Child("line-height")
	if lh == nil {
		t.Fatal("Todo panel has no line-height")
	}
	if lh.Arg != "2" {
		t.Fatalf("line-height arg = %q, want %q (inline comment should be stripped)", lh.Arg, "2")
	}

	// A ragged 3/5/7-space indent scheme is legal as long as each block is
	// internally consistent, which the sketch is.
	dayText := top[1].Child("text")
	if dayText == nil {
		t.Fatal("first section has no text node")
	}
	if dayText.Arg != "Day of {date}" {
		t.Fatalf("text arg = %q", dayText.Arg)
	}
	if a := dayText.Child("align"); a == nil || a.Arg != "right" {
		t.Fatalf("text/align = %v", a)
	}
}

func TestHexColorIsNotAComment(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{"three digit hex", "background: #ddd", "#ddd"},
		{"six digit hex", "background: #ddeeff", "#ddeeff"},
		{"eight digit hex with alpha", "background: #ddeeff80", "#ddeeff80"},
		{"hex then comment", "background: #ddd  # muted", "#ddd"},
		{"comment only after value", "background: white # the paper", "white"},
		{"hash inside a word is kept", "text: item#5", "item#5"},
		{"hash inside quotes is kept", `text: "a # b"`, `"a # b"`},
		{"five digits is not a colour", "text: #abcde", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, diags := ParseString("t.pulp", tt.line+"\n")
			if diags.HasErrors() {
				t.Fatalf("errors: %v", diags)
			}
			n := doc.TopLevel()[0]
			if n.Arg != tt.want {
				t.Fatalf("arg = %q, want %q", n.Arg, tt.want)
			}
		})
	}
}

func TestTabInIndentationIsAnError(t *testing.T) {
	_, diags := ParseString("t.pulp", "panel\n\tfont: mono\n")
	if !diags.HasErrors() {
		t.Fatal("expected an error for a tab")
	}
	if diags[0].Code != "E002" {
		t.Fatalf("code = %s, want E002", diags[0].Code)
	}
	if !strings.Contains(diags[0].Help, "fmt") {
		t.Fatalf("help should point at `treekillbot fmt`, got %q", diags[0].Help)
	}
}

func TestMisalignedDedentIsAnError(t *testing.T) {
	// Columns 0, 4 and 8 are open; landing on 2 belongs to none of them, and
	// silently reparenting to column 0 would produce a valid document that is
	// not the one the author wrote.
	src := "section\n" +
		"    column\n" +
		"        panel\n" +
		"  text: stray\n"
	_, diags := ParseString("t.pulp", src)
	if !diags.HasErrors() {
		t.Fatal("expected an error for the misaligned dedent")
	}
	d := diags[0]
	if d.Code != "E003" {
		t.Fatalf("code = %s, want E003", d.Code)
	}
	if !strings.Contains(d.Help, "1, 5, 9") {
		t.Fatalf("help should list the legal columns 1, 5, 9; got %q", d.Help)
	}
}

func TestRaggedButConsistentIndentIsFine(t *testing.T) {
	src := "section\n" +
		"   column\n" +
		"       panel\n" +
		"          height: fill\n" +
		"       panel\n" +
		"   column\n"
	doc, diags := ParseString("t.pulp", src)
	if diags.HasErrors() {
		t.Fatalf("unexpected errors: %v", diags)
	}
	cols := doc.TopLevel()[0].ChildrenNamed("column")
	if len(cols) != 2 {
		t.Fatalf("got %d columns, want 2", len(cols))
	}
	if len(cols[0].ChildrenNamed("panel")) != 2 {
		t.Fatalf("first column should hold 2 panels")
	}
}

func TestBlockStrings(t *testing.T) {
	t.Run("keep newlines", func(t *testing.T) {
		src := "panel\n  text: |\n    line one\n    line two\n"
		doc, diags := ParseString("t.pulp", src)
		if diags.HasErrors() {
			t.Fatalf("errors: %v", diags)
		}
		got := doc.TopLevel()[0].Child("text").Arg
		if got != "line one\nline two" {
			t.Fatalf("arg = %q", got)
		}
	})

	t.Run("fold into spaces with paragraph breaks", func(t *testing.T) {
		src := "panel\n  text: >\n    one\n    two\n\n    three\n"
		doc, diags := ParseString("t.pulp", src)
		if diags.HasErrors() {
			t.Fatalf("errors: %v", diags)
		}
		got := doc.TopLevel()[0].Child("text").Arg
		if got != "one two\nthree" {
			t.Fatalf("arg = %q, want %q", got, "one two\nthree")
		}
	})

	t.Run("block ends at a shallower line", func(t *testing.T) {
		src := "panel\n  text: |\n    body\n  font-size: 8pt\n"
		doc, diags := ParseString("t.pulp", src)
		if diags.HasErrors() {
			t.Fatalf("errors: %v", diags)
		}
		panel := doc.TopLevel()[0]
		if got := panel.Child("text").Arg; got != "body" {
			t.Fatalf("text arg = %q, want %q", got, "body")
		}
		if fs := panel.Child("font-size"); fs == nil || fs.Arg != "8pt" {
			t.Fatalf("font-size should be a sibling property of text, got %v", fs)
		}
	})

	t.Run("common indent is stripped but relative indent kept", func(t *testing.T) {
		src := "panel\n  text: |\n    1. first\n       continued\n    2. second\n"
		doc, diags := ParseString("t.pulp", src)
		if diags.HasErrors() {
			t.Fatalf("errors: %v", diags)
		}
		got := doc.TopLevel()[0].Child("text").Arg
		want := "1. first\n   continued\n2. second"
		if got != want {
			t.Fatalf("arg = %q, want %q", got, want)
		}
	})
}

func TestLineContinuation(t *testing.T) {
	src := "text: the quick brown \\\n      fox jumps\n"
	doc, diags := ParseString("t.pulp", src)
	if diags.HasErrors() {
		t.Fatalf("errors: %v", diags)
	}
	if got := doc.TopLevel()[0].Arg; got != "the quick brown fox jumps" {
		t.Fatalf("arg = %q", got)
	}
}

func TestBothArgumentSpellingsAreEquivalent(t *testing.T) {
	bare, _ := ParseString("a.pulp", "panel \"Notes\"\n")
	colon, _ := ParseString("b.pulp", "panel: \"Notes\"\n")
	a, b := bare.TopLevel()[0], colon.TopLevel()[0]
	if a.Name != b.Name || a.Arg != b.Arg {
		t.Fatalf("spellings differ: %q/%q vs %q/%q", a.Name, a.Arg, b.Name, b.Arg)
	}
	if a.Colon || !b.Colon {
		t.Fatal("Colon flag should record which spelling was used, for fmt")
	}
}

func TestPositionsAreAccurate(t *testing.T) {
	src := "section\n  panel \"Notes\"\n    height: 200\n"
	doc, _ := ParseString("t.pulp", src)
	height := doc.TopLevel()[0].Children[0].Child("height")
	if height == nil {
		t.Fatal("no height node")
	}
	// The caret must be able to land on `200`, not merely on the line.
	pos := doc.Source.Position(height.ArgSpan.Start)
	if pos.Line != 3 || pos.Column != 13 {
		t.Fatalf("argument at %d:%d, want 3:13", pos.Line, pos.Column)
	}
	if got := doc.Source.SpanText(height.ArgSpan); got != "200" {
		t.Fatalf("argument span covers %q, want %q", got, "200")
	}
}
