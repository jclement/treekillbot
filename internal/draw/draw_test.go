package draw

import (
	"testing"

	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/layout"
	"github.com/jclement/treekillbot/internal/paint"
	"github.com/jclement/treekillbot/internal/pulp"
	"github.com/jclement/treekillbot/internal/render"
	"github.com/jclement/treekillbot/internal/schema"
)

// node builds a laid-out node with properties written as Pulp source, so tests
// declare them the way a document does.
func node(t *testing.T, kind layout.Kind, border geom.Rect, properties map[string]string) *layout.Node {
	t.Helper()
	n := layout.NewNode(kind)
	for name, text := range properties {
		id, ok := schema.Lookup(name)
		if !ok {
			t.Fatalf("unknown property %q", name)
		}
		src := pulp.NewSource("test", text)
		var diags pulp.Diagnostics
		values := pulp.ParseValues(src, pulp.Span{Start: 0, End: len(text)}, text, &diags)
		if diags.HasErrors() {
			t.Fatalf("%s = %s: %v", name, text, diags)
		}
		n.Props.Set(id, values)
	}
	env := &layout.Env{Diags: &pulp.Diagnostics{}}
	layout.Arrange(n, border, env)
	return n
}

func paintToOps(t *testing.T, n *layout.Node) []render.Op {
	t.Helper()
	ops := render.NewOps()
	Paint(n, ops, &Env{})
	return ops.Ops()
}

// findOp returns the first recorded op of a kind.
func findOp(t *testing.T, ops []render.Op, kind string) render.Op {
	t.Helper()
	for _, op := range ops {
		if op.Kind == kind {
			return op
		}
	}
	t.Fatalf("no %q op among %v", kind, kinds(ops))
	return render.Op{}
}

func kinds(ops []render.Op) []string {
	out := make([]string, 0, len(ops))
	for _, op := range ops {
		out = append(out, op.Kind)
	}
	return out
}

// Rule A (DESIGN.md D4): a border's stroke path is inset by half its width, so
// the stroke's OUTER edge lands on the declared rectangle and the box still
// measures exactly what it says it does.
//
// This is the single most consequential geometric decision in the renderer. Get
// it wrong and every panel is a half-point too big, adjacent panels overlap,
// and nothing on the page is where the layout engine said it was.
func TestBorderStrokeSitsInsideTheDeclaredRect(t *testing.T) {
	const boxSize = 100
	n := node(t, layout.KindPanel, geom.Rect{X: 0, Y: 0, W: geom.Pt(boxSize), H: geom.Pt(boxSize)},
		map[string]string{"border-width": "1pt", "border-color": "gray(0)", "border-style": "solid"})

	ops := paintToOps(t, n)
	rect := findOp(t, ops, "rect")
	if len(rect.Args) < 4 {
		t.Fatalf("rect op has %d args", len(rect.Args))
	}
	x, y, w, h := rect.Args[0], rect.Args[1], rect.Args[2], rect.Args[3]

	// A 1pt border on a 100pt box strokes a 99pt path starting at 0.5, so the
	// ink covers exactly [0,1] and [99,100] and the box is still 100pt.
	if x != 0.5 || y != 0.5 {
		t.Errorf("stroke path origin = (%g,%g), want (0.5,0.5)", x, y)
	}
	if w != 99 || h != 99 {
		t.Errorf("stroke path size = %gx%g, want 99x99", w, h)
	}
	if outer := x - 0.5; outer != 0 {
		t.Errorf("the stroke's outer edge is at %g, want 0 — the border must not escape its rect", outer)
	}
	if outer := x + w + 0.5; outer != boxSize {
		t.Errorf("the stroke's far outer edge is at %g, want %d", outer, boxSize)
	}
}

// The corollary: changing the border width must not move the box.
func TestBorderWidthDoesNotChangeTheBox(t *testing.T) {
	for _, width := range []string{"0.25pt", "0.5pt", "1pt", "3pt"} {
		n := node(t, layout.KindPanel, geom.Rect{W: geom.Pt(100), H: geom.Pt(100)},
			map[string]string{"border-width": width, "border-color": "gray(0)"})
		if got := n.Frame.Border.W; got != geom.Pt(100) {
			t.Errorf("border-width %s changed the box to %.2fpt", width, got.Points())
		}
		ops := paintToOps(t, n)
		rect := findOp(t, ops, "rect")
		half := rect.Args[0]
		if outerLeft := rect.Args[0] - half; outerLeft != 0 {
			t.Errorf("border-width %s: outer edge at %g, want 0", width, outerLeft)
		}
	}
}

// A zero-width stroke means "thinnest the device can draw" in PDF, which makes
// the line's weight a property of the printer rather than of the document.
func TestStrokesAreNeverThinnerThanAQuarterPoint(t *testing.T) {
	for _, width := range []string{"0.05pt", "0.1pt", "0.24pt"} {
		n := node(t, layout.KindPanel, geom.Rect{W: geom.Pt(100), H: geom.Pt(100)},
			map[string]string{"border-width": width, "border-color": "gray(0)"})
		ops := paintToOps(t, n)
		for _, op := range ops {
			if op.Kind == "stroke-pen" || op.Width > 0 {
				if op.Width > 0 && op.Width < 0.25 {
					t.Errorf("border-width %s emitted a %gpt stroke, below the 0.25pt floor", width, op.Width)
				}
			}
		}
	}
}

// A transparent background must paint nothing at all. Painting white instead
// would still cost a fill and would knock out anything beneath it.
func TestTransparentBackgroundPaintsNothing(t *testing.T) {
	n := node(t, layout.KindPanel, geom.Rect{W: geom.Pt(100), H: geom.Pt(100)}, nil)
	for _, op := range paintToOps(t, n) {
		if op.Kind == "fill" {
			t.Fatalf("a panel with no background should not fill: %v", kinds(paintToOps(t, n)))
		}
	}
}

func TestBackgroundIsFilled(t *testing.T) {
	n := node(t, layout.KindPanel, geom.Rect{W: geom.Pt(100), H: geom.Pt(100)},
		map[string]string{"background": "gray(0.9)"})
	ops := paintToOps(t, n)
	found := false
	for _, op := range ops {
		if op.Kind == "fill" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a fill, got %v", kinds(ops))
	}
}

// Grayscale is a property of a print run, not of the document, so it is applied
// at paint time rather than by rewriting the document's colours.
func TestGrayscaleConvertsAtPaintTime(t *testing.T) {
	n := node(t, layout.KindPanel, geom.Rect{W: geom.Pt(100), H: geom.Pt(100)},
		map[string]string{"background": "#1f6feb"})

	colour := n.Props.Color(schema.PBackground, paint.Transparent)
	if colour.Space != paint.SpaceRGB {
		t.Fatal("the document's own colour must stay RGB")
	}

	ops := render.NewOps()
	Paint(n, ops, &Env{Grayscale: true})
	// The document is untouched; only the emitted ink changed.
	if after := n.Props.Color(schema.PBackground, paint.Transparent); after != colour {
		t.Fatal("--grayscale must not rewrite the document's colours")
	}
}

func TestZeroSizedNodesPaintNothing(t *testing.T) {
	for _, r := range []geom.Rect{
		{W: 0, H: geom.Pt(10)},
		{W: geom.Pt(10), H: 0},
		{},
	} {
		n := node(t, layout.KindPanel, r, map[string]string{"background": "gray(0.5)"})
		if ops := paintToOps(t, n); len(ops) != 0 {
			t.Fatalf("rect %s painted %v", r, kinds(ops))
		}
	}
}

// Border comes after children so an overflowing child cannot paint over the
// frame that contains it.
func TestBorderIsPaintedAfterChildren(t *testing.T) {
	parent := layout.NewNode(layout.KindPanel)
	setProp(t, parent, "border-width", "1pt")
	setProp(t, parent, "border-color", "gray(0)")
	child := layout.NewNode(layout.KindPanel)
	setProp(t, child, "background", "gray(0.5)")
	// An auto-height panel with no content is legitimately zero-high and paints
	// nothing, so the child has to ask for space before it can overlap anything.
	setProp(t, child, "height", "fill")
	parent.Append(child)

	env := &layout.Env{Diags: &pulp.Diagnostics{}}
	layout.Arrange(parent, geom.Rect{W: geom.Pt(100), H: geom.Pt(100)}, env)

	ops := render.NewOps()
	Paint(parent, ops, &Env{})
	recorded := ops.Ops()

	fillAt, strokeAt := -1, -1
	for i, op := range recorded {
		if op.Kind == "fill" && fillAt < 0 {
			fillAt = i
		}
		if op.Kind == "stroke" {
			strokeAt = i
		}
	}
	if fillAt < 0 || strokeAt < 0 {
		t.Fatalf("expected both a child fill and a parent stroke, got %v", kinds(recorded))
	}
	if strokeAt < fillAt {
		t.Fatal("the parent's border was painted before its child's background")
	}
}

func setProp(t *testing.T, n *layout.Node, name, text string) {
	t.Helper()
	id, ok := schema.Lookup(name)
	if !ok {
		t.Fatalf("unknown property %q", name)
	}
	src := pulp.NewSource("test", text)
	var diags pulp.Diagnostics
	n.Props.Set(id, pulp.ParseValues(src, pulp.Span{Start: 0, End: len(text)}, text, &diags))
}

// A notch title straddles the border rather than sitting inside the padding.
// Positioning it like the other styles put the text inside the padding while
// the knockout stayed on the border, so on any panel with padding-top the gap
// appeared where the text was not and the frame looked broken.
func TestNotchTitleSitsOnTheBorder(t *testing.T) {
	n := node(t, layout.KindPanel, geom.Rect{W: geom.Pt(200), H: geom.Pt(100)},
		map[string]string{
			"border-width": "1pt",
			"border-color": "gray(0)",
			"padding":      "14pt",
			"title-style":  "notch",
		})
	n.Title = "NOTCHED"

	// Without a font the title cannot be measured, so the band collapses and
	// nothing is drawn — which is itself the correct behaviour, and is what the
	// zero-height assertion below pins.
	title := titleMetrics(n, &Env{})
	if title.present() {
		t.Fatal("a title with no resolvable face must not claim a band")
	}

	// With a band, it must be centred on the border rather than on the content.
	title = titleInfo{text: "NOTCHED", style: "notch", position: "top", height: geom.Pt(8)}
	band := titleBand(n, title)
	borderTop := n.Frame.Border.Y
	if want := borderTop - geom.Pt(4); band.Y != want {
		t.Fatalf("notch band at y=%.2fpt, want %.2fpt (centred on the border)",
			band.Y.Points(), want.Points())
	}
	if band.Y >= n.Frame.Content.Y {
		t.Fatal("the notch band must sit above the content box, not inside the padding")
	}
}

// Two boxes that touch exactly would each stroke the edge they share, making it
// twice as heavy as every other line on the page. Exactly one stroke must
// survive (DESIGN.md D4).
func TestSharedBordersCollapse(t *testing.T) {
	page := layout.NewNode(layout.KindPage)
	left := layout.NewNode(layout.KindBox)
	right := layout.NewNode(layout.KindBox)
	for _, box := range []*layout.Node{left, right} {
		setProp(t, box, "border-width", "1pt")
		setProp(t, box, "border-color", "gray(0)")
		setProp(t, box, "width", "50%")
	}
	// Placed by hand so the two share an edge exactly, which is the condition
	// the collapse is defined on.
	env := &layout.Env{Diags: &pulp.Diagnostics{}}
	layout.Arrange(left, geom.Rect{X: 0, Y: 0, W: geom.Pt(100), H: geom.Pt(50)}, env)
	layout.Arrange(right, geom.Rect{X: geom.Pt(100), Y: 0, W: geom.Pt(100), H: geom.Pt(50)}, env)
	page.Append(left)
	page.Append(right)
	page.Frame = left.Frame

	collapsed := collapseBorders(page, &Env{})
	if skip := collapsed[right]; !skip.left {
		t.Fatal("the right-hand box should not stroke the edge its neighbour already strokes")
	}
	if skip := collapsed[left]; skip.left || skip.top {
		t.Fatal("the left-hand box keeps every edge; nothing precedes it")
	}
}

func TestCollapseRequiresAnIdenticalPen(t *testing.T) {
	tests := []struct {
		name       string
		rightProps map[string]string
		wantSkip   bool
	}{
		{"identical pens collapse", map[string]string{"border-width": "1pt", "border-color": "gray(0)"}, true},
		{"a different width does not", map[string]string{"border-width": "2pt", "border-color": "gray(0)"}, false},
		{"a different colour does not", map[string]string{"border-width": "1pt", "border-color": "gray(0.5)"}, false},
		{"a different style does not", map[string]string{"border-width": "1pt", "border-color": "gray(0)", "border-style": "dashed"}, false},
		{"opting out does not", map[string]string{"border-width": "1pt", "border-color": "gray(0)", "border-collapse": "false"}, false},
		// A rounded corner has no straight edge to share, and collapsing would
		// leave a gap where the curve pulls away from the join.
		{"a radius does not", map[string]string{"border-width": "1pt", "border-color": "gray(0)", "border-radius": "3pt"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page := layout.NewNode(layout.KindPage)
			left := layout.NewNode(layout.KindBox)
			setProp(t, left, "border-width", "1pt")
			setProp(t, left, "border-color", "gray(0)")
			right := layout.NewNode(layout.KindBox)
			for name, value := range tt.rightProps {
				setProp(t, right, name, value)
			}
			env := &layout.Env{Diags: &pulp.Diagnostics{}}
			layout.Arrange(left, geom.Rect{W: geom.Pt(100), H: geom.Pt(50)}, env)
			layout.Arrange(right, geom.Rect{X: geom.Pt(100), W: geom.Pt(100), H: geom.Pt(50)}, env)
			page.Append(left)
			page.Append(right)

			if got := collapseBorders(page, &Env{})[right].left; got != tt.wantSkip {
				t.Fatalf("collapsed = %v, want %v", got, tt.wantSkip)
			}
		})
	}
}

// Boxes that merely line up on one axis but do not touch must both keep their
// borders, or a gap would lose a line.
func TestSeparatedBoxesDoNotCollapse(t *testing.T) {
	page := layout.NewNode(layout.KindPage)
	left := layout.NewNode(layout.KindBox)
	right := layout.NewNode(layout.KindBox)
	for _, box := range []*layout.Node{left, right} {
		setProp(t, box, "border-width", "1pt")
		setProp(t, box, "border-color", "gray(0)")
	}
	env := &layout.Env{Diags: &pulp.Diagnostics{}}
	layout.Arrange(left, geom.Rect{W: geom.Pt(100), H: geom.Pt(50)}, env)
	// One tick of gap is still a gap.
	layout.Arrange(right, geom.Rect{X: geom.Pt(100) + 1, W: geom.Pt(100), H: geom.Pt(50)}, env)
	page.Append(left)
	page.Append(right)

	if collapseBorders(page, &Env{})[right].left {
		t.Fatal("boxes one tick apart do not share an edge and must both keep their borders")
	}
}

// The collapse is defined on edges, not on the tree, because the boxes that
// touch are often not siblings: a row of columns each holding one bordered
// panel has the panels as grandchildren of the row.
func TestCollapseWorksAcrossNesting(t *testing.T) {
	page := layout.NewNode(layout.KindPage)
	var panels []*layout.Node
	for i := 0; i < 2; i++ {
		column := layout.NewNode(layout.KindColumn)
		panel := layout.NewNode(layout.KindPanel)
		setProp(t, panel, "border-width", "0.5pt")
		setProp(t, panel, "border-color", "gray(0.2)")
		setProp(t, panel, "height", "fill")
		column.Append(panel)
		page.Append(column)
		panels = append(panels, panel)
	}
	env := &layout.Env{Diags: &pulp.Diagnostics{}}
	layout.Layout(page, geom.Rect{W: geom.Pt(200), H: geom.Pt(50)}, env)

	if !collapseBorders(page, &Env{})[panels[1]].left {
		t.Fatal("panels in adjacent columns share an edge even though they are not siblings")
	}
}
