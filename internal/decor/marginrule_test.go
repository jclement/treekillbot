package decor

import (
	"testing"

	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/render"
)

// The margin rule is a modifier, not a style: it must compose with whatever the
// node asked for rather than replacing it.
func TestMarginRuleComposes(t *testing.T) {
	content := geom.Rect{X: geom.In(1), Y: geom.In(1), W: geom.In(6), H: geom.Pt(200)}
	plain := build(t, "line-style: ruled", "line-pitch: 20pt")
	withRule := build(t, "line-style: ruled", "line-pitch: 20pt", "margin-rule: true")

	if !equalTicks(plain.Baselines(content), withRule.Baselines(content)) {
		t.Fatal("the margin rule moved the writing rules")
	}

	drawn := segments(draw(withRule, content, Grid{}))
	verticals := 0
	for _, s := range drawn {
		if s.X1 != s.X2 {
			continue
		}
		verticals++
		if want := content.X + geom.Pt(28); s.X1 != want {
			t.Errorf("margin rule at x=%d, want the default 28pt offset at %d", s.X1, want)
		}
		if s.Y1 != content.Y || s.Y2 != content.Bottom() {
			t.Errorf("margin rule spans %d..%d, want the full content height %d..%d",
				s.Y1, s.Y2, content.Y, content.Bottom())
		}
	}
	if verticals != 1 {
		t.Fatalf("%d vertical rules, want exactly 1", verticals)
	}
	if got := len(horizontalYs(draw(plain, content, Grid{}))); got != len(drawn)-1 {
		t.Errorf("%d writing rules beside the margin rule, want %d", len(drawn)-1, got)
	}
}

// It composes with a dot grid too, and keeps its own colour: the red line is
// red, not the grey of the rules it sits over.
func TestMarginRuleOnADotGrid(t *testing.T) {
	content := geom.Rect{X: 0, Y: 0, W: geom.In(6), H: geom.Pt(200)}
	ops := draw(build(t, "line-style: dotted", "dot-pitch: 5mm", "margin-rule: true",
		"margin-rule-offset: 40pt", "line-color: gray(0.8)"), content, pageGrid)

	if countOps(ops, render.OpFill) != 1 {
		t.Error("the dot grid stopped being drawn")
	}
	drawn := segments(ops)
	if len(drawn) != 1 || drawn[0].X1 != geom.Pt(40) {
		t.Fatalf("margin rule segments %v, want one at x=%d", drawn, geom.Pt(40))
	}
	for _, op := range ops {
		if op.Kind == render.OpSetStroke && op.Color == "gray(0.8)" {
			t.Error("the margin rule took the line colour instead of its own")
		}
	}
}

// An offset that falls outside the content rect draws nothing rather than a
// stray line on the panel edge.
func TestMarginRuleOutsideTheContentRect(t *testing.T) {
	content := geom.Rect{X: 0, Y: 0, W: geom.Pt(20), H: geom.Pt(200)}
	d := build(t, "line-style: ruled", "line-pitch: 20pt", "margin-rule: true")
	for _, s := range segments(draw(d, content, Grid{})) {
		if s.X1 == s.X2 {
			t.Fatalf("drew a margin rule at x=%d in a %d-wide panel", s.X1, content.W)
		}
	}
}
