package decor

import (
	"testing"

	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/render"
)

// boxRects returns every checkbox path recorded, as (x, y, w, h) in ticks.
func boxRects(ops []render.Op) []geom.Rect {
	var out []geom.Rect
	for _, op := range ops {
		if op.Kind != render.OpRect || len(op.Args) < 4 {
			continue
		}
		out = append(out, geom.Rect{
			X: ticks(op.Args[0]), Y: ticks(op.Args[1]),
			W: ticks(op.Args[2]), H: ticks(op.Args[3]),
		})
	}
	return out
}

// The requirement that justifies sharing the row arithmetic: a checkbox panel
// and a ruled panel of the same height and pitch put their rules in exactly the
// same places, so the two can sit side by side in adjacent columns.
func TestCheckboxRowsMatchRuledRows(t *testing.T) {
	for _, height := range []geom.Tick{geom.Pt(200), geom.Pt(207), 1001, geom.In(9)} {
		content := geom.Rect{X: geom.In(1), Y: geom.In(1), W: geom.In(3), H: height}
		ruledRows := build(t, "line-style: ruled", "line-pitch: 6mm").Baselines(content)
		boxRows := build(t, "line-style: checkbox", "line-pitch: 6mm").Baselines(content)
		if !equalTicks(ruledRows, boxRows) {
			t.Fatalf("height %d:\n ruled    %v\n checkbox %v", height, ruledRows, boxRows)
		}
	}
}

// Rule A: the box is stroked edge-aligned, so its outer silhouette is exactly
// size x size and a column of boxes measures exactly one pitch between outer
// edges. The pen width is chosen even here so the halving is exact; an odd
// width is out by half a tick, which is 0.03pt.
func TestCheckboxSilhouetteIsExact(t *testing.T) {
	content := geom.Rect{X: geom.In(1), Y: geom.In(1), W: geom.In(3), H: geom.Pt(200)}
	d := build(t, "line-style: checkbox", "line-pitch: 20pt", "line-width: 1pt")
	ops := draw(d, content, Grid{})

	boxes := boxRects(ops)
	if len(boxes) != len(d.Baselines(content)) {
		t.Fatalf("%d boxes for %d rows", len(boxes), len(d.Baselines(content)))
	}

	var penWidth geom.Tick
	for _, op := range ops {
		if op.Kind == render.OpSetStroke && ticks(op.Width) != geom.Pt(1) {
			penWidth = ticks(op.Width)
		}
	}
	if penWidth == 0 || penWidth%2 != 0 {
		t.Fatalf("box pen %d ticks; the test wants an even width", penWidth)
	}

	size := geom.Pt(20).Scale(checkboxSizeNum, checkboxSizeDen)
	half := penWidth / 2
	for i, box := range boxes {
		if got := box.W + penWidth; got != size {
			t.Errorf("box %d silhouette %d wide, want %d", i, got, size)
		}
		if got := box.X - half; got != content.X {
			t.Errorf("box %d outer left edge at %d, want the content edge %d", i, got, content.X)
		}
	}

	// One path, one stroke operator, for every box on the panel.
	if got := countOps(ops, render.OpStroke); got != 1 {
		t.Errorf("%d stroke operators for the boxes, want 1", got)
	}
}

// The box sits ON the baseline with a small optical overshoot below it, the way
// a well-set bullet does. Exactly on the baseline reads as floating high.
func TestCheckboxSitsBelowTheBaseline(t *testing.T) {
	content := geom.Rect{X: 0, Y: 0, W: geom.In(3), H: geom.Pt(200)}
	d := build(t, "line-style: checkbox", "line-pitch: 20pt", "line-width: 1pt")
	ops := draw(d, content, Grid{})

	size := geom.Pt(20).Scale(checkboxSizeNum, checkboxSizeDen)
	sit := size.Scale(checkboxSitNum, checkboxSitDen)
	if sit <= 0 {
		t.Fatal("no optical overshoot at all")
	}
	half := geom.Pt(1).Scale(checkboxWeightNum, checkboxWeightDen) / 2

	baselines := d.Baselines(content)
	for i, box := range boxRects(ops) {
		if got := box.Bottom() + half; got != baselines[i]+sit {
			t.Errorf("box %d bottom at %d, want the baseline %d plus %d of overshoot",
				i, got, baselines[i], sit)
		}
	}
}

// The trailing rule starts a gutter past the box, and `checkbox-rule: false`
// removes it entirely.
func TestCheckboxTrailingRule(t *testing.T) {
	content := geom.Rect{X: geom.In(1), Y: 0, W: geom.In(3), H: geom.Pt(200)}
	pitch := geom.Pt(20)
	size := pitch.Scale(checkboxSizeNum, checkboxSizeDen)
	gutter := pitch.Scale(checkboxGutterNum, checkboxGutterDen)

	drawn := segments(draw(build(t, "line-style: checkbox", "line-pitch: 20pt"), content, Grid{}))
	if len(drawn) == 0 {
		t.Fatal("no trailing rules")
	}
	if want := content.X + size + gutter; drawn[0].X1 != want {
		t.Errorf("rule starts at %d, want past the box and gutter at %d", drawn[0].X1, want)
	}
	if drawn[0].X2 != content.Right() {
		t.Errorf("rule ends at %d, want the right edge %d", drawn[0].X2, content.Right())
	}

	bare := draw(build(t, "line-style: checkbox", "line-pitch: 20pt", "checkbox-rule: false"), content, Grid{})
	if len(segments(bare)) != 0 {
		t.Error("checkbox-rule: false still drew rules")
	}
	if len(boxRects(bare)) == 0 {
		t.Error("checkbox-rule: false removed the boxes too")
	}
}

// checkbox-size overrides the 0.62-of-pitch default without disturbing the rows.
func TestCheckboxSizeOverride(t *testing.T) {
	content := geom.Rect{X: 0, Y: 0, W: geom.In(3), H: geom.Pt(200)}
	d := build(t, "line-style: checkbox", "line-pitch: 20pt", "line-width: 1pt", "checkbox-size: 8pt")
	boxes := boxRects(draw(d, content, Grid{}))
	if len(boxes) == 0 {
		t.Fatal("no boxes")
	}
	penWidth := geom.Pt(1).Scale(checkboxWeightNum, checkboxWeightDen)
	if got := boxes[0].W + penWidth; got != geom.Pt(8) {
		t.Errorf("box silhouette %d, want 8pt = %d", got, geom.Pt(8))
	}
}
