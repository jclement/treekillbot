package layout

import (
	"testing"

	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/pulp"
	"github.com/jclement/treekillbot/internal/schema"
)

// testEnv returns an environment with no fonts. Structural tests do not need
// them: text measures to zero, and every assertion here is about boxes.
func testEnv() *Env {
	var diags pulp.Diagnostics
	return &Env{Diags: &diags}
}

// node builds a layout node with properties given as Pulp source, so tests
// declare sizes the same way a document does and exercise the real parser.
func node(t *testing.T, kind Kind, properties map[string]string) *Node {
	t.Helper()
	n := NewNode(kind)
	for name, text := range properties {
		id, ok := schema.Lookup(name)
		if !ok {
			t.Fatalf("unknown property %q in test", name)
		}
		src := pulp.NewSource("test", text)
		var diags pulp.Diagnostics
		values := pulp.ParseValues(src, pulp.Span{Start: 0, End: len(text)}, text, &diags)
		if diags.HasErrors() {
			t.Fatalf("test property %s: %s = %v", name, text, diags)
		}
		n.Props.Set(id, values)
	}
	return n
}

var letter = geom.Rect{X: 0, Y: 0, W: geom.In(8.5), H: geom.In(11)}

// anyFlexible reports whether some group on the axis can absorb leftover space.
func anyFlexible(groups []group) bool {
	for _, g := range groups {
		switch groupHeight(g).Mode {
		case geom.SizeFill, geom.SizePercent:
			return true
		}
	}
	return false
}

// The invariant that makes "pixel perfect" mean anything: children tile their
// parent's content box exactly, with no gap and no overlap, at every level.
func assertChildrenTileParent(t *testing.T, n *Node) {
	t.Helper()
	if len(n.Children) == 0 {
		return
	}
	groups := groupChildren(n.Children)
	gap := n.Props.Tick(schema.PGap, 0)
	content := n.Frame.Content

	var covered geom.Tick
	for i, g := range groups {
		var h geom.Tick
		if g.single != nil {
			h = g.single.Frame.Margin.H
		} else {
			// Every column in a row must be exactly the row's height.
			h = g.columns[0].Frame.Margin.H
			for _, col := range g.columns[1:] {
				if col.Frame.Margin.H != h {
					t.Errorf("%s: columns in a row differ in height: %d vs %d",
						n.Label(), col.Frame.Margin.H, h)
				}
			}
			// And the columns must tile the row's width exactly.
			var wide geom.Tick
			for j, col := range g.columns {
				wide += col.Frame.Margin.W
				if j < len(g.columns)-1 {
					wide += gap
				}
			}
			if wide != content.W {
				t.Errorf("%s: row %d columns span %d ticks, parent content is %d",
					n.Label(), i, wide, content.W)
			}
		}
		covered += h
		if i < len(groups)-1 {
			covered += gap
		}
	}
	// Children must never exceed their parent's content box. They need only
	// FILL it when something on the axis is flexible — a column holding one
	// fixed-height panel legitimately leaves space below it.
	if covered > content.H {
		t.Errorf("%s: children span %d ticks, overflowing parent content of %d by %d",
			n.Label(), covered, content.H, covered-content.H)
	}
	if anyFlexible(groups) && covered != content.H {
		t.Errorf("%s: children span %d ticks but parent content is %d; a flexible child should have absorbed the difference of %d",
			n.Label(), covered, content.H, content.H-covered)
	}

	for _, c := range n.Children {
		assertChildrenTileParent(t, c)
	}
}

func TestNestedFillsTileExactly(t *testing.T) {
	page := node(t, KindPage, map[string]string{"padding": "0.5in"})
	header := node(t, KindSection, map[string]string{"height": "48pt"})
	body := node(t, KindSection, map[string]string{"height": "fill", "gap": "5pt"})
	footer := node(t, KindSection, map[string]string{"height": "20pt"})
	page.Append(header)
	page.Append(body)
	page.Append(footer)

	left := node(t, KindColumn, map[string]string{"width": "70%"})
	right := node(t, KindColumn, map[string]string{"width": "30%"})
	body.Append(left)
	body.Append(right)

	notes := node(t, KindPanel, map[string]string{"height": "fill", "border-width": "0.5pt", "padding": "5pt"})
	todo := node(t, KindPanel, map[string]string{"height": "200pt", "border-width": "0.5pt", "padding": "5pt"})
	left.Append(notes)
	right.Append(todo)

	Layout(page, letter, testEnv())
	assertChildrenTileParent(t, page)

	// Hand-checkable: the body section gets everything the header and footer
	// leave behind.
	contentH := geom.In(11) - geom.In(1) // page padding, top and bottom
	wantBody := contentH - geom.Pt(48) - geom.Pt(20)
	if body.Frame.Border.H != wantBody {
		t.Fatalf("body height = %d ticks (%.2fpt), want %d (%.2fpt)",
			body.Frame.Border.H, body.Frame.Border.H.Points(), wantBody, wantBody.Points())
	}
}

// Border-box sizing (D4): a declared height is the height of the border box.
// Adding a border must not make the box taller.
func TestBorderBoxSizing(t *testing.T) {
	page := node(t, KindPage, nil)
	panel := node(t, KindPanel, map[string]string{
		"height": "100pt", "border-width": "1pt", "padding": "6pt",
	})
	page.Append(panel)
	Layout(page, letter, testEnv())

	if panel.Frame.Border.H != geom.Pt(100) {
		t.Fatalf("border box = %.2fpt, want exactly 100pt", panel.Frame.Border.H.Points())
	}
	// A 1pt border and 6pt padding take 7pt off each side.
	if want := geom.Pt(100 - 14); panel.Frame.Content.H != want {
		t.Fatalf("content box = %.2fpt, want %.2fpt", panel.Frame.Content.H.Points(), want.Points())
	}
	if panel.Frame.Content.Y != panel.Frame.Border.Y+geom.Pt(7) {
		t.Fatal("content should start 7pt below the border box top")
	}
}

// Seven day boxes across a landscape page: the case the whole tool exists for.
func TestSevenDayColumnsTileExactly(t *testing.T) {
	landscape := geom.Rect{X: 0, Y: 0, W: geom.In(11), H: geom.In(8.5)}
	page := node(t, KindPage, map[string]string{"padding": "0.4in"})
	row := node(t, KindSection, map[string]string{"height": "fill", "gap": "5pt"})
	page.Append(row)

	for i := 0; i < 7; i++ {
		col := node(t, KindColumn, nil)
		col.Append(node(t, KindPanel, map[string]string{"height": "fill", "border-width": "0.5pt"}))
		row.Append(col)
	}

	Layout(page, landscape, testEnv())
	assertChildrenTileParent(t, page)

	contentW := geom.In(11) - geom.In(0.8)
	var spanned geom.Tick
	for i, col := range row.Children {
		spanned += col.Frame.Border.W
		if i < 6 {
			spanned += geom.Pt(5)
		}
	}
	if spanned != contentW {
		t.Fatalf("seven columns plus six gaps span %d ticks, content width is %d", spanned, contentW)
	}

	// Widths may differ by at most one tick (1/16pt) — that is the remainder
	// being distributed, and it is invisible at any print resolution.
	widest, narrowest := row.Children[0].Frame.Border.W, row.Children[0].Frame.Border.W
	for _, col := range row.Children {
		if col.Frame.Border.W > widest {
			widest = col.Frame.Border.W
		}
		if col.Frame.Border.W < narrowest {
			narrowest = col.Frame.Border.W
		}
	}
	if widest-narrowest > 1 {
		t.Fatalf("column widths vary by %d ticks, want at most 1", widest-narrowest)
	}
}

// The same document on a different page size must still tile exactly. This is
// the page-size independence the design promises.
func TestPageSizeIndependence(t *testing.T) {
	sizes := map[string]geom.Rect{
		"letter":           {W: geom.In(8.5), H: geom.In(11)},
		"a4":               {W: geom.Mm(210), H: geom.Mm(297)},
		"a5":               {W: geom.Mm(148), H: geom.Mm(210)},
		"legal":            {W: geom.In(8.5), H: geom.In(14)},
		"letter landscape": {W: geom.In(11), H: geom.In(8.5)},
		"awkward":          {W: 9999, H: 13337},
	}
	for name, page := range sizes {
		t.Run(name, func(t *testing.T) {
			root := node(t, KindPage, map[string]string{"padding": "0.5in", "gap": "6pt"})
			top := node(t, KindSection, map[string]string{"height": "fill(2)", "gap": "4pt"})
			bottom := node(t, KindSection, map[string]string{"height": "fill(1)"})
			root.Append(top)
			root.Append(bottom)
			for _, w := range []string{"30%", "45%", "25%"} {
				col := node(t, KindColumn, map[string]string{"width": w})
				col.Append(node(t, KindPanel, map[string]string{"height": "fill"}))
				top.Append(col)
			}
			Layout(root, page, testEnv())
			assertChildrenTileParent(t, root)
		})
	}
}

func TestOverflowIsReportedWithNumbers(t *testing.T) {
	env := testEnv()
	page := node(t, KindPage, nil)
	page.Source = pulp.NewSource("t.pulp", "page")
	section := node(t, KindSection, map[string]string{"height": "2in"})
	section.Source = page.Source
	page.Append(section)
	// Two fixed panels that together exceed the section.
	section.Append(node(t, KindPanel, map[string]string{"height": "100pt"}))
	section.Append(node(t, KindPanel, map[string]string{"height": "100pt"}))

	Layout(page, geom.Rect{W: geom.In(8.5), H: geom.In(11)}, env)

	found := false
	for _, d := range *env.Diags {
		if d.Code == "E010" {
			found = true
			if d.Help == "" {
				t.Error("overflow error should say how much it is short by")
			}
		}
	}
	if !found {
		t.Fatalf("expected an E010 overflow error, got %v", *env.Diags)
	}
}

func TestMarginsSitOutsideTheDeclaredSize(t *testing.T) {
	page := node(t, KindPage, nil)
	panel := node(t, KindPanel, map[string]string{"height": "100pt", "margin": "10pt"})
	page.Append(panel)
	Layout(page, letter, testEnv())

	if panel.Frame.Border.H != geom.Pt(100) {
		t.Fatalf("border box = %.2fpt, want 100pt: a margin must not change the box", panel.Frame.Border.H.Points())
	}
	if panel.Frame.Margin.H != geom.Pt(120) {
		t.Fatalf("margin box = %.2fpt, want 120pt", panel.Frame.Margin.H.Points())
	}
	if panel.Frame.Border.X != geom.Pt(10) {
		t.Fatalf("border box x = %.2fpt, want 10pt", panel.Frame.Border.X.Points())
	}
}

func TestLayoutIsDeterministic(t *testing.T) {
	build := func() *Node {
		root := node(t, KindPage, map[string]string{"padding": "0.5in", "gap": "6pt"})
		for i := 0; i < 4; i++ {
			s := node(t, KindSection, map[string]string{"height": "fill", "gap": "3pt"})
			for _, w := range []string{"33%", "34%", "33%"} {
				c := node(t, KindColumn, map[string]string{"width": w})
				c.Append(node(t, KindPanel, map[string]string{"height": "fill", "padding": "4pt"}))
				s.Append(c)
			}
			root.Append(s)
		}
		return root
	}

	first := build()
	Layout(first, letter, testEnv())
	var reference []geom.Rect
	first.Walk(func(n *Node) bool {
		reference = append(reference, n.Frame.Border)
		return true
	})

	for run := 0; run < 50; run++ {
		again := build()
		Layout(again, letter, testEnv())
		i := 0
		again.Walk(func(n *Node) bool {
			if n.Frame.Border != reference[i] {
				t.Fatalf("run %d, node %d: %s vs %s", run, i, n.Frame.Border, reference[i])
			}
			i++
			return true
		})
	}
}

// A container sizing itself to its contents must honour a child's DECLARED
// height, not its intrinsic one. Measuring the intrinsic size here undersizes
// the parent, and the symptom is an overflow error naming two numbers that both
// look correct in isolation.
func TestAutoContainerHonoursFixedChildHeight(t *testing.T) {
	page := node(t, KindPage, nil)
	section := node(t, KindSection, map[string]string{"height": "auto"})
	panel := node(t, KindPanel, map[string]string{"height": "40pt", "padding": "10pt"})
	page.Append(section)
	section.Append(panel)

	env := testEnv()
	Layout(page, letter, env)

	if got := section.Frame.Border.H; got != geom.Pt(40) {
		t.Fatalf("auto section = %.2fpt, want 40pt — it must size to its child's declared height",
			got.Points())
	}
	if got := panel.Frame.Border.H; got != geom.Pt(40) {
		t.Fatalf("panel = %.2fpt, want 40pt", got.Points())
	}
	for _, d := range *env.Diags {
		t.Errorf("unexpected diagnostic: %s", d.Plain())
	}
}

// The same, across a row: the row is as tall as its tallest column demands.
func TestAutoContainerHonoursTallestColumn(t *testing.T) {
	page := node(t, KindPage, nil)
	section := node(t, KindSection, map[string]string{"height": "auto"})
	page.Append(section)
	for _, height := range []string{"30pt", "80pt", "50pt"} {
		column := node(t, KindColumn, map[string]string{"height": height})
		section.Append(column)
	}

	env := testEnv()
	Layout(page, letter, env)
	if got := section.Frame.Border.H; got != geom.Pt(80) {
		t.Fatalf("auto section = %.2fpt, want 80pt (the tallest column)", got.Points())
	}
	for _, d := range *env.Diags {
		t.Errorf("unexpected diagnostic: %s", d.Plain())
	}
}

// A min-height floor applies to the contribution too, so a short fixed child
// still reserves the room it asked for.
func TestMinHeightRaisesTheContribution(t *testing.T) {
	page := node(t, KindPage, nil)
	section := node(t, KindSection, map[string]string{"height": "auto"})
	panel := node(t, KindPanel, map[string]string{"height": "10pt", "min-height": "60pt"})
	page.Append(section)
	section.Append(panel)

	Layout(page, letter, testEnv())
	if got := section.Frame.Border.H; got != geom.Pt(60) {
		t.Fatalf("auto section = %.2fpt, want 60pt", got.Points())
	}
}
