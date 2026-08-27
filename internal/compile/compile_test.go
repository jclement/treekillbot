package compile

import (
	"testing"

	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/layout"
	"github.com/jclement/treekillbot/internal/paint"
	"github.com/jclement/treekillbot/internal/pulp"
	"github.com/jclement/treekillbot/internal/schema"
)

func compileString(t *testing.T, src string) (*Result, pulp.Diagnostics) {
	t.Helper()
	doc, parseDiags := pulp.ParseString("t.pulp", src)
	if parseDiags.HasErrors() {
		t.Fatalf("parse errors: %v", parseDiags)
	}
	return Compile(doc, Options{})
}

// find returns the first node of a kind with a matching title, or fails.
func find(t *testing.T, root *layout.Node, kind layout.Kind, title string) *layout.Node {
	t.Helper()
	var found *layout.Node
	root.Walk(func(n *layout.Node) bool {
		if found == nil && n.Kind == kind && (title == "" || n.Title == title) {
			found = n
		}
		return true
	})
	if found == nil {
		t.Fatalf("no %s titled %q in the tree", kind, title)
	}
	return found
}

func TestCompilesTheSketchShape(t *testing.T) {
	src := `page
  size: letter
  orientation: landscape
  margin: 0.5in

defaults
  font: IBM Plex Mono
  font-size: 8pt

section
  text: Day of Monday
    align: right

section
  height: fill
  column
    width: 70%
    panel "Notes"
      height: fill
      line-style: ruled
    panel "Todo"
      height: 200pt
      line-style: checkbox
  column
    width: 30%
    panel "Things"
`
	result, diags := compileString(t, src)
	for _, d := range diags {
		t.Errorf("unexpected diagnostic: %s", d.Plain())
	}

	// Landscape must have swapped the trim.
	if result.Page.Width != geom.In(11) || result.Page.Height != geom.In(8.5) {
		t.Fatalf("page = %vx%v, want 11x8.5in", result.Page.Width.Inches(), result.Page.Height.Inches())
	}
	if result.Margin.Top != geom.In(0.5) {
		t.Fatalf("margin = %v, want 0.5in", result.Margin.Top.Inches())
	}

	sections := 0
	for _, c := range result.Root.Children {
		if c.Kind == layout.KindSection {
			sections++
		}
	}
	if sections != 2 {
		t.Fatalf("got %d sections, want 2", sections)
	}

	notes := find(t, result.Root, layout.KindPanel, "NOTES")
	if got := notes.Props.Enum(schema.PLineStyle, ""); got != "ruled" {
		t.Fatalf("Notes line-style = %q", got)
	}
	// title-transform defaults to upper, so the title is upper-cased at compile
	// time rather than at paint time — one place, one answer.
	if notes.Title != "NOTES" {
		t.Fatalf("title = %q, want NOTES", notes.Title)
	}
}

// The decision in DESIGN.md section 3: a property written on an ancestor beats
// a document-level `defaults` block, which is where we part company with CSS.
func TestExplicitAncestorBeatsGlobalDefaults(t *testing.T) {
	src := `defaults
  font-size: 8pt

section
  font-size: 24pt
  panel "Big"
`
	result, diags := compileString(t, src)
	if diags.HasErrors() {
		t.Fatalf("errors: %v", diags)
	}
	panel := find(t, result.Root, layout.KindPanel, "BIG")
	if got := panel.Props.Tick(schema.PFontSize, 0); got != geom.Pt(24) {
		t.Fatalf("inherited font-size = %.2fpt, want 24pt — a defaults block must not defeat an ancestor's explicit value",
			got.Points())
	}
}

// The other half of that decision: a value an ancestor merely picked up from a
// defaults block must not defeat a more nested defaults block.
func TestCascadedAncestorValueDoesNotBeatNestedDefaults(t *testing.T) {
	src := `defaults
  font-size: 8pt

section
  defaults
    font-size: 14pt
  panel "Nested"
`
	result, diags := compileString(t, src)
	if diags.HasErrors() {
		t.Fatalf("errors: %v", diags)
	}
	panel := find(t, result.Root, layout.KindPanel, "NESTED")
	if got := panel.Props.Tick(schema.PFontSize, 0); got != geom.Pt(14) {
		t.Fatalf("font-size = %.2fpt, want 14pt from the nested defaults block", got.Points())
	}
}

func TestNestedDefaultsDoNotEscapeTheirSubtree(t *testing.T) {
	src := `defaults
  font-size: 8pt

section
  defaults
    font-size: 14pt
  panel "Inside"

section
  panel "Outside"
`
	result, _ := compileString(t, src)
	inside := find(t, result.Root, layout.KindPanel, "INSIDE")
	outside := find(t, result.Root, layout.KindPanel, "OUTSIDE")
	if got := inside.Props.Tick(schema.PFontSize, 0); got != geom.Pt(14) {
		t.Fatalf("inside = %.2fpt, want 14pt", got.Points())
	}
	if got := outside.Props.Tick(schema.PFontSize, 0); got != geom.Pt(8) {
		t.Fatalf("outside = %.2fpt, want 8pt: the nested defaults leaked", got.Points())
	}
}

// A `defaults <element>` block styles the element AND what it contains. It once
// styled only the element: inheritance carried explicit values only, a defaults
// block set nothing explicitly, so the text inside a panel fell back to the
// document-wide default and the block silently did half its job.
func TestDefaultsForOneElementTypeReachTheContents(t *testing.T) {
	src := `defaults
  font-size: 8pt

defaults panel
  font-size: 14pt

section
  panel "P"
    text "inside"
`
	result, diags := compileString(t, src)
	if diags.HasErrors() {
		t.Fatalf("errors: %v", diags)
	}
	text := find(t, result.Root, layout.KindText, "")
	if got := text.Props.Tick(schema.PFontSize, 0); got != geom.Pt(14) {
		t.Fatalf("text inside the panel = %.2fpt, want 14pt from `defaults panel` — the block must reach the contents, not just the panel",
			got.Points())
	}
}

// The case that found the bug: a sheet whose rules were set once, on
// `defaults panel`, drew the rules inside a panel's columns at the theme's
// weight instead — two line weights on one page.
func TestLineWeightSetOnPanelDefaultsReachesInnerColumns(t *testing.T) {
	src := `defaults
  line-width: 0.5pt

defaults panel
  line-width: 0.35pt

section
  panel "Notes"
    column
      line-style: ruled
`
	result, diags := compileString(t, src)
	if diags.HasErrors() {
		t.Fatalf("errors: %v", diags)
	}
	col := find(t, result.Root, layout.KindColumn, "")
	if got := col.Props.Tick(schema.PLineWidth, 0); got != geom.Pt(0.35) {
		t.Fatalf("column rules at %.2fpt, want 0.35pt — a column inside the panel must rule at the panel's weight",
			got.Points())
	}
}

func TestDefaultsForOneElementType(t *testing.T) {
	src := `defaults panel
  padding: 5pt

section
  panel "P"
  box
`
	result, _ := compileString(t, src)
	panel := find(t, result.Root, layout.KindPanel, "P")
	if got := panel.Props.Edges(schema.PPadding, geom.Edges{}).Top; got != geom.Pt(5) {
		t.Fatalf("panel padding = %.2fpt, want 5pt", got.Points())
	}
	box := find(t, result.Root, layout.KindBox, "")
	if got := box.Props.Edges(schema.PPadding, geom.Edges{}).Top; got != 0 {
		t.Fatalf("box padding = %.2fpt, want 0: `defaults panel` must not reach a box", got.Points())
	}
}

func TestStyleBundles(t *testing.T) {
	src := `style ruled
  line-style: ruled
  line-pitch: 15pt

style compact
  line-pitch: 9pt

section
  panel "A"
    style: ruled
  panel "B"
    style: ruled compact
  panel "C"
    style: ruled
    line-pitch: 20pt
`
	result, diags := compileString(t, src)
	if diags.HasErrors() {
		t.Fatalf("errors: %v", diags)
	}
	cases := map[string]geom.Tick{"A": geom.Pt(15), "B": geom.Pt(9), "C": geom.Pt(20)}
	for title, want := range cases {
		panel := find(t, result.Root, layout.KindPanel, title)
		if got := panel.Props.Tick(schema.PLinePitch, 0); got != want {
			t.Errorf("panel %s line-pitch = %.2fpt, want %.2fpt", title, got.Points(), want.Points())
		}
		if got := panel.Props.Enum(schema.PLineStyle, ""); got != "ruled" {
			t.Errorf("panel %s line-style = %q", title, got)
		}
	}
}

func TestUnknownStyleSuggests(t *testing.T) {
	src := `style ruled
  line-style: ruled

section
  panel "A"
    style: ruleed
`
	_, diags := compileString(t, src)
	var found *pulp.Diagnostic
	for _, d := range diags {
		if d.Code == "E130" {
			found = d
		}
	}
	if found == nil {
		t.Fatalf("expected E130, got %v", diags)
	}
	if want := "ruled"; !contains(found.Help, want) {
		t.Fatalf("help = %q, want a suggestion of %q", found.Help, want)
	}
}

func TestBorderShorthandInAnyOrder(t *testing.T) {
	for _, spelling := range []string{
		"0.5pt solid #333",
		"#333 0.5pt solid",
		"solid #333 0.5pt",
	} {
		src := "section\n  panel \"P\"\n    border: " + spelling + "\n"
		result, diags := compileString(t, src)
		if diags.HasErrors() {
			t.Fatalf("%s: %v", spelling, diags)
		}
		panel := find(t, result.Root, layout.KindPanel, "P")
		if got := panel.Props.Edges(schema.PBorderWidth, geom.Edges{}).Top; got != geom.Pt(0.5) {
			t.Errorf("%s: width = %.2fpt", spelling, got.Points())
		}
		if got := panel.Props.Enum(schema.PBorderStyle, ""); got != "solid" {
			t.Errorf("%s: style = %q", spelling, got)
		}
		r, g, b := panel.Props.Color(schema.PBorderColor, paint.Black).ToRGB8()
		if r != 0x33 || g != 0x33 || b != 0x33 {
			t.Errorf("%s: colour = %02x%02x%02x", spelling, r, g, b)
		}
	}
}

func TestRepeatExpandsToRealNodes(t *testing.T) {
	src := `section
  repeat 5
    panel "Row"
`
	result, diags := compileString(t, src)
	if diags.HasErrors() {
		t.Fatalf("errors: %v", diags)
	}
	count := 0
	result.Root.Walk(func(n *layout.Node) bool {
		if n.Kind == layout.KindPanel {
			count++
		}
		return true
	})
	if count != 5 {
		t.Fatalf("got %d panels, want 5", count)
	}
}

func TestLiteralListLoop(t *testing.T) {
	src := `section
  for tag in Home, Work, Errands
    column
      panel "T"
`
	result, diags := compileString(t, src)
	if diags.HasErrors() {
		t.Fatalf("errors: %v", diags)
	}
	columns := 0
	result.Root.Walk(func(n *layout.Node) bool {
		if n.Kind == layout.KindColumn {
			columns++
		}
		return true
	})
	if columns != 3 {
		t.Fatalf("got %d columns, want 3", columns)
	}
}

// A loop's children must form a row exactly as hand-written columns do — that
// is what makes `for day in week.days` produce a week spread.
func TestLoopColumnsFormOneRow(t *testing.T) {
	src := `section
  for tag in A, B, C
    column
      panel "P"
`
	result, _ := compileString(t, src)
	var section *layout.Node
	result.Root.Walk(func(n *layout.Node) bool {
		if n.Kind == layout.KindSection && section == nil {
			section = n
		}
		return true
	})
	if section == nil {
		t.Fatal("no section")
	}
	for i, c := range section.Children {
		if c.Kind != layout.KindColumn {
			t.Fatalf("child %d is a %s, want a column", i, c.Kind)
		}
	}
	if len(section.Children) != 3 {
		t.Fatalf("got %d children, want 3", len(section.Children))
	}
}

func TestWhenDropsASubtree(t *testing.T) {
	src := `section
  panel "Kept"
  panel "Dropped"
    when: false
`
	result, _ := compileString(t, src)
	result.Root.Walk(func(n *layout.Node) bool {
		if n.Title == "DROPPED" {
			t.Fatal("a node with `when: false` should not reach the layout tree")
		}
		return true
	})
	find(t, result.Root, layout.KindPanel, "KEPT")
}

func TestPageSizes(t *testing.T) {
	tests := []struct {
		spelling      string
		width, height geom.Tick
	}{
		{"letter", geom.In(8.5), geom.In(11)},
		{"a4", geom.Mm(210), geom.Mm(297)},
		{"US Letter", geom.In(8.5), geom.In(11)},
		{"a5", geom.Mm(148), geom.Mm(210)},
		{"200mm 300mm", geom.Mm(200), geom.Mm(300)},
		{"4x6", geom.In(4), geom.In(6)},
	}
	for _, tt := range tests {
		t.Run(tt.spelling, func(t *testing.T) {
			result, diags := compileString(t, "page\n  size: "+tt.spelling+"\n")
			if diags.HasErrors() {
				t.Fatalf("errors: %v", diags)
			}
			if result.Page.Width != tt.width || result.Page.Height != tt.height {
				t.Fatalf("got %dx%d ticks, want %dx%d", result.Page.Width, result.Page.Height, tt.width, tt.height)
			}
		})
	}
}

func TestUnknownPageSizeSuggests(t *testing.T) {
	_, diags := compileString(t, "page\n  size: a44\n")
	for _, d := range diags {
		if d.Code == "E140" {
			if !contains(d.Help, "a4") {
				t.Fatalf("help = %q, want a suggestion of a4", d.Help)
			}
			return
		}
	}
	t.Fatalf("expected E140, got %v", diags)
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
