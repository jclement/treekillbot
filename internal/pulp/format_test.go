package pulp

import "testing"

// canonicalForTest stands in for the schema, which sits above this package.
func canonicalForTest(name string) (string, bool, bool) {
	elements := map[string]string{
		"page": "page", "section": "section", "column": "column", "panel": "panel",
		"text": "text", "rule": "rule", "defaults": "defaults", "style": "style",
		"row": "section", "col": "column",
	}
	properties := map[string]string{
		"height": "height", "width": "width", "align": "align", "font": "font",
		"font-size": "font-size", "colour": "color", "color": "color",
		"line-style": "line-style", "padding": "padding", "title": "title",
	}
	if canonical, ok := elements[name]; ok {
		return canonical, true, true
	}
	if canonical, ok := properties[name]; ok {
		return canonical, false, true
	}
	return name, false, false
}

func format(t *testing.T, input string) string {
	t.Helper()
	out, diags := Format(NewSource("t.pulp", input), FormatOptions{Canonical: canonicalForTest})
	if diags.HasErrors() {
		t.Fatalf("unexpected errors: %v", diags)
	}
	return out
}

func TestFormatNormalisesIndentation(t *testing.T) {
	// The user's own ragged 3/7/5-space scheme is legal, and `fmt` is the one
	// opinion the language enforces.
	input := "section\n" +
		"   column\n" +
		"       panel \"Notes\"\n" +
		"          height: fill\n"
	want := "section\n" +
		"  column\n" +
		"    panel \"Notes\"\n" +
		"      height: fill\n"
	if got := format(t, input); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// Elements take the bare form and properties the colon form, which is the
// distinction people write naturally even though the grammar ignores it.
func TestFormatNormalisesArgumentSpelling(t *testing.T) {
	input := "section\n  panel: \"Notes\"\n    height fill\n"
	want := "section\n  panel \"Notes\"\n    height: fill\n"
	if got := format(t, input); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatCanonicalisesNames(t *testing.T) {
	input := "row\n  col\n    text \"hi\"\n      colour: #333\n"
	want := "section\n  column\n    text \"hi\"\n      color: #333\n"
	if got := format(t, input); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// A formatter that eats comments is not a formatter.
func TestFormatPreservesComments(t *testing.T) {
	input := "# the masthead\n" +
		"section\n" +
		"      # this one sits with the panel\n" +
		"  panel \"Notes\"    #trailing\n" +
		"    height: fill\n"
	want := "# the masthead\n" +
		"section\n" +
		"  # this one sits with the panel\n" +
		"  panel \"Notes\"  # trailing\n" +
		"    height: fill\n"
	if got := format(t, input); got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

// Normalising comments to `# ` is what makes the one syntactic ambiguity in the
// language unreachable in a formatted file.
func TestFormatMakesTheHexAmbiguityUnreachable(t *testing.T) {
	input := "section\n  panel\n    color: #ddd\n"
	got := format(t, input)
	if got != "section\n  panel\n    color: #ddd\n" {
		t.Fatalf("a colour must survive formatting untouched, got %q", got)
	}
}

func TestFormatCollapsesBlankRuns(t *testing.T) {
	input := "\n\nsection\n\n\n\n  panel\n\n\n"
	want := "section\n\n  panel\n"
	if got := format(t, input); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatPreservesBlockStrings(t *testing.T) {
	input := "panel\n" +
		"      text: |\n" +
		"            1. first\n" +
		"               continued\n" +
		"            2. second\n"
	want := "panel\n" +
		"  text: |\n" +
		"    1. first\n" +
		"       continued\n" +
		"    2. second\n"
	if got := format(t, input); got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

// Formatting must be a fixed point, or a save hook would rewrite the file on
// every save forever.
func TestFormatIsIdempotent(t *testing.T) {
	inputs := []string{
		sketch,
		"section\n   column\n     panel: \"A\"\n       height fill\n",
		"# lead\n\n\nsection\n  # inner\n  panel\n",
		"panel\n  text: |\n    body line\n  height: fill\n",
	}
	for _, input := range inputs {
		once := format(t, input)
		twice := format(t, once)
		if once != twice {
			t.Fatalf("not idempotent.\nfirst:\n%q\nsecond:\n%q", once, twice)
		}
	}
}

// Formatting must never change what a document means.
func TestFormatPreservesTheParsedTree(t *testing.T) {
	formatted := format(t, sketch)
	before, _ := ParseString("a.pulp", sketch)
	after, _ := ParseString("b.pulp", formatted)

	var flatten func(n *Node, depth int, out *[]string)
	flatten = func(n *Node, depth int, out *[]string) {
		if n.Name != "" {
			*out = append(*out, string(rune('0'+depth))+n.Name+"="+n.Arg)
		}
		for _, c := range n.Children {
			flatten(c, depth+1, out)
		}
	}
	var a, b []string
	for _, n := range before.TopLevel() {
		flatten(n, 0, &a)
	}
	for _, n := range after.TopLevel() {
		flatten(n, 0, &b)
	}
	if len(a) != len(b) {
		t.Fatalf("node count changed: %d then %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("node %d changed: %q became %q", i, a[i], b[i])
		}
	}
}

// A file that does not parse comes back untouched: moving the text an error
// points at makes the error harder to find.
func TestFormatRefusesABrokenFile(t *testing.T) {
	input := "section\n\tpanel\n"
	out, diags := Format(NewSource("t.pulp", input), FormatOptions{Canonical: canonicalForTest})
	if !diags.HasErrors() {
		t.Fatal("expected the tab error")
	}
	if out != input {
		t.Fatalf("a broken file must be returned unchanged, got %q", out)
	}
}
