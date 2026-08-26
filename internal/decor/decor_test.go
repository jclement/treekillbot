package decor

import (
	"strconv"
	"strings"
	"testing"

	"github.com/jclement/treekillbot/internal/fonts"
	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/pulp"
	"github.com/jclement/treekillbot/internal/render"
	"github.com/jclement/treekillbot/internal/schema"
)

// ---- Helpers ----

// build resolves a decoration from properties written the way a document
// writes them, so the tests exercise the real value parser and the real
// property table rather than a hand-built bag that could drift from either.
func build(t *testing.T, declarations ...string) Decoration {
	t.Helper()
	props := schemaProps(t, declarations...)
	d, err := New(props, nil)
	if err != nil {
		t.Fatalf("New(%v): %v", declarations, err)
	}
	return d
}

// buildErr is build for the cases that are supposed to be rejected.
func buildErr(t *testing.T, declarations ...string) error {
	t.Helper()
	_, err := New(schemaProps(t, declarations...), nil)
	return err
}

// segment is one drawn line, in ticks, recovered from a recording canvas.
type segment struct{ X1, Y1, X2, Y2 geom.Tick }

// draw paints a decoration onto a recording canvas and returns the ops.
func draw(d Decoration, content geom.Rect, grid Grid) []render.Op {
	canvas := render.NewOps()
	d.Draw(content, grid, canvas)
	return canvas.Ops()
}

// segments flattens every batched "lines" op into individual segments, in the
// order they were drawn.
func segments(ops []render.Op) []segment {
	var out []segment
	for _, op := range ops {
		if op.Kind != render.OpStrokeLines {
			continue
		}
		for i := 0; i+3 < len(op.Args); i += 4 {
			out = append(out, segment{
				ticks(op.Args[i]), ticks(op.Args[i+1]), ticks(op.Args[i+2]), ticks(op.Args[i+3]),
			})
		}
	}
	return out
}

// horizontalYs returns the y of every horizontal segment drawn.
func horizontalYs(ops []render.Op) []geom.Tick {
	var out []geom.Tick
	for _, s := range segments(ops) {
		if s.Y1 == s.Y2 {
			out = append(out, s.Y1)
		}
	}
	return out
}

// countOps returns how many ops of a kind were recorded.
func countOps(ops []render.Op, kind string) int {
	n := 0
	for _, op := range ops {
		if op.Kind == kind {
			n++
		}
	}
	return n
}

// ticks converts a recorded coordinate back to ticks. The conversion is exact:
// the canvas emits tick-quantised values at four decimal places, which is
// lossless for a quantum of 0.0625pt.
func ticks(points float64) geom.Tick { return geom.Pt(points) }

func equalTicks(a, b []geom.Tick) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// panel is a convenient content rect: an inch in from the top-left corner.
var panel = geom.Rect{X: geom.In(1), Y: geom.In(1), W: geom.In(6), H: geom.Pt(200)}

// ---- Row geometry ----

// The floor-and-centre rule, checked against counts and positions worked out by
// hand in the table, so a regression shows up as an arithmetic difference
// rather than as "the golden file changed".
func TestRuledRowTable(t *testing.T) {
	cases := []struct {
		name      string
		height    geom.Tick
		pitch     geom.Tick
		wantCount int
		wantFirst geom.Tick // relative to the content rect's top
		wantLast  geom.Tick
	}{
		{"exact fit", geom.Pt(200), geom.Pt(20), 10, geom.Pt(20), geom.Pt(200)},
		{"leftover centred", geom.Pt(210), geom.Pt(20), 10, geom.Pt(25), geom.Pt(205)},
		{"6mm ruling", 1000, geom.Mm(6), 3, 364, 908},
		{"one rule exactly", 320, 320, 1, 320, 320},
		{"odd tick goes to the bottom", 321, 320, 1, 320, 320},
		{"shorter than one row", 100, 320, 0, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := build(t, "line-style: ruled", "line-pitch: "+formatTicks(tc.pitch))
			content := geom.Rect{X: 0, Y: geom.Pt(72), W: geom.Pt(300), H: tc.height}
			got := d.Baselines(content)
			if len(got) != tc.wantCount {
				t.Fatalf("%d rules, want %d (%v)", len(got), tc.wantCount, got)
			}
			if tc.wantCount == 0 {
				return
			}
			if got[0] != content.Y+tc.wantFirst {
				t.Errorf("first rule at %v, want %v", got[0]-content.Y, tc.wantFirst)
			}
			if got[len(got)-1] != content.Y+tc.wantLast {
				t.Errorf("last rule at %v, want %v", got[len(got)-1]-content.Y, tc.wantLast)
			}
		})
	}
}

// Every rule is an exact multiple of the pitch from the first one: the pitch the
// author tuned is preserved exactly and the remainder becomes padding.
func TestRulesAreExactMultiplesOfPitch(t *testing.T) {
	pitch := geom.Mm(6)
	d := build(t, "line-style: ruled", "line-pitch: 6mm")
	rules := d.Baselines(panel)
	if len(rules) != int(panel.H/pitch) {
		t.Fatalf("%d rules, want floor(%d/%d) = %d", len(rules), panel.H, pitch, panel.H/pitch)
	}
	for i, y := range rules {
		if want := rules[0] + geom.Tick(i)*pitch; y != want {
			t.Errorf("rule %d at %d, want %d", i, y, want)
		}
	}
}

// Rule B (DESIGN.md D4). A writing rule IS the line, so its weight must not
// move it — a 2pt rule and a hairline share a centre.
func TestLineWidthDoesNotMoveRules(t *testing.T) {
	thin := build(t, "line-style: ruled", "line-pitch: 20pt", "line-width: 0.25pt")
	thick := build(t, "line-style: ruled", "line-pitch: 20pt", "line-width: 3pt")

	if !equalTicks(thin.Baselines(panel), thick.Baselines(panel)) {
		t.Fatalf("baselines moved with line-width:\n thin  %v\n thick %v",
			thin.Baselines(panel), thick.Baselines(panel))
	}
	thinYs := horizontalYs(draw(thin, panel, Grid{}))
	thickYs := horizontalYs(draw(thick, panel, Grid{}))
	if !equalTicks(thinYs, thickYs) {
		t.Fatalf("drawn rules moved with line-width:\n thin  %v\n thick %v", thinYs, thickYs)
	}
}

// The leftover is split floor(leftover/2) above the block and the rest below,
// which is what makes a panel look designed rather than truncated.
func TestLeftoverIsCentred(t *testing.T) {
	pitch := geom.Pt(20)
	content := geom.Rect{X: 0, Y: 0, W: geom.Pt(300), H: geom.Pt(207)} // 10 rows, 7pt over
	d := build(t, "line-style: ruled", "line-pitch: 20pt")
	rules := d.Baselines(content)

	leftover := content.H - geom.Tick(len(rules))*pitch
	wantTop := leftover / 2
	if got := rules[0] - pitch - content.Y; got != wantTop {
		t.Errorf("top offset %d, want floor(%d/2) = %d", got, leftover, wantTop)
	}
	gotBottom := content.Bottom() - rules[len(rules)-1]
	if want := leftover - wantTop; gotBottom != want {
		t.Errorf("bottom offset %d, want %d", gotBottom, want)
	}
	if gotBottom < wantTop {
		t.Errorf("the odd tick went to the top, not the bottom")
	}
}

// `line-distribute: grow` gives up the declared pitch so the last rule lands
// EXACTLY on the bottom edge — the property DistributeTicks exists to provide.
func TestGrowMeetsTheBottomEdge(t *testing.T) {
	for _, height := range []geom.Tick{geom.Pt(207), geom.Pt(200), 1001, 999} {
		content := geom.Rect{X: 0, Y: geom.In(2), W: geom.Pt(300), H: height}
		d := build(t, "line-style: ruled", "line-pitch: 20pt", "line-distribute: grow")
		rules := d.Baselines(content)
		if len(rules) == 0 {
			t.Fatalf("height %d: no rules", height)
		}
		if last := rules[len(rules)-1]; last != content.Bottom() {
			t.Errorf("height %d: last rule at %d, want the bottom edge %d", height, last, content.Bottom())
		}
	}
}

// `line-distribute: start` and `end` push the block to one end instead.
func TestDistributeStartAndEnd(t *testing.T) {
	content := geom.Rect{X: 0, Y: 0, W: geom.Pt(300), H: geom.Pt(207)}
	pitch := geom.Pt(20)

	start := build(t, "line-style: ruled", "line-pitch: 20pt", "line-distribute: start").Baselines(content)
	if start[0] != pitch {
		t.Errorf("start: first rule at %d, want %d", start[0], pitch)
	}
	end := build(t, "line-style: ruled", "line-pitch: 20pt", "line-distribute: end").Baselines(content)
	if last := end[len(end)-1]; last != content.Bottom() {
		t.Errorf("end: last rule at %d, want %d", last, content.Bottom())
	}
}

// `lines: N` fixes the count and still centres what is left over.
func TestLineCountCapsAndCentres(t *testing.T) {
	content := geom.Rect{X: 0, Y: 0, W: geom.Pt(300), H: geom.Pt(200)}
	d := build(t, "line-style: ruled", "line-pitch: 20pt", "lines: 4")
	rules := d.Baselines(content)
	if len(rules) != 4 {
		t.Fatalf("%d rules, want 4", len(rules))
	}
	leftover := content.H - 4*geom.Pt(20)
	if want := content.Y + leftover/2 + geom.Pt(20); rules[0] != want {
		t.Errorf("first rule at %d, want %d", rules[0], want)
	}
	if want := geom.Pt(80); d.NaturalHeight() != want {
		t.Errorf("NaturalHeight %d, want %d", d.NaturalHeight(), want)
	}
}

// `line-partial` is the legal pad that genuinely runs off the edge: the leftover
// stays at the bottom as a short row with a rule closing it.
func TestPartialKeepsTheStubRow(t *testing.T) {
	content := geom.Rect{X: 0, Y: 0, W: geom.Pt(300), H: geom.Pt(210)}
	full := build(t, "line-style: ruled", "line-pitch: 20pt").Baselines(content)
	stub := build(t, "line-style: ruled", "line-pitch: 20pt", "line-partial: true").Baselines(content)

	if len(stub) != len(full)+1 {
		t.Fatalf("%d rules with a stub, want one more than %d", len(stub), len(full))
	}
	if stub[0] != geom.Pt(20) {
		t.Errorf("first rule at %d, want the full pitch %d (no top padding)", stub[0], geom.Pt(20))
	}
	if last := stub[len(stub)-1]; last != content.Bottom() {
		t.Errorf("stub rule at %d, want the bottom edge %d", last, content.Bottom())
	}
}

// Batching is a promise, not an optimisation: forty rules must be one path and
// one stroke operator, so every rule is painted in an identical graphics state.
func TestRuledPanelIsOneStrokeOperator(t *testing.T) {
	content := geom.Rect{X: 0, Y: 0, W: geom.Pt(300), H: geom.Pt(800)}
	ops := draw(build(t, "line-style: ruled", "line-pitch: 20pt"), content, Grid{})

	if got := countOps(ops, render.OpStrokeLines); got != 1 {
		t.Fatalf("%d line operators, want 1 (ops: %v)", got, ops)
	}
	if got := len(horizontalYs(ops)); got != 40 {
		t.Fatalf("%d rules in the batch, want 40", got)
	}
	if got := countOps(ops, render.OpSetStroke); got != 1 {
		t.Errorf("%d pen changes, want 1", got)
	}
}

// Draw and Baselines must agree to the tick. Text asks the decoration where the
// lines are precisely so that these two numbers cannot diverge.
func TestDrawnRulesMatchBaselines(t *testing.T) {
	d := build(t, "line-style: ruled", "line-pitch: 6mm", "line-inset: 4pt")
	if !equalTicks(horizontalYs(draw(d, panel, Grid{})), d.Baselines(panel)) {
		t.Fatal("drawn rules and reported baselines disagree")
	}
}

// line-inset moves the rules in from the content edges on all four sides.
func TestLineInsetShrinksTheRuledArea(t *testing.T) {
	inset := geom.Pt(9)
	d := build(t, "line-style: ruled", "line-pitch: 20pt", "line-inset: 9pt")
	drawn := segments(draw(d, panel, Grid{}))
	if len(drawn) == 0 {
		t.Fatal("nothing drawn")
	}
	if drawn[0].X1 != panel.X+inset || drawn[0].X2 != panel.Right()-inset {
		t.Errorf("rule spans %d..%d, want %d..%d", drawn[0].X1, drawn[0].X2, panel.X+inset, panel.Right()-inset)
	}
	if drawn[0].Y1 <= panel.Y+inset {
		t.Errorf("first rule at %d is not below the inset top edge %d", drawn[0].Y1, panel.Y+inset)
	}
}

// `line-style: none` is a real decoration, not a nil: it answers every question
// so the painter never has to special-case it.
func TestNoneDrawsNothing(t *testing.T) {
	d := build(t)
	if ops := draw(d, panel, Grid{}); len(ops) != 0 {
		t.Errorf("none drew %v", ops)
	}
	if len(d.Baselines(panel)) != 0 || d.NaturalHeight() != 0 {
		t.Error("none reported geometry it does not have")
	}
}

func TestUnknownStyleIsRejected(t *testing.T) {
	err := buildErr(t, "line-style: hatched")
	if err == nil || !strings.Contains(err.Error(), "hatched") {
		t.Fatalf("err = %v, want a complaint naming the style", err)
	}
}

// ---- Test-only property construction ----

// schemaProps parses "name: value" declarations into a resolved property set.
// Declarations are a slice rather than a map so the order is the source order;
// nothing in this package may depend on map iteration (DESIGN.md section 4),
// and that includes the tests.
func schemaProps(t *testing.T, declarations ...string) *schema.Props {
	t.Helper()
	props := schema.NewProps()
	for _, declaration := range declarations {
		name, text, ok := strings.Cut(declaration, ":")
		if !ok {
			t.Fatalf("test declaration %q is not `name: value`", declaration)
		}
		name, text = strings.TrimSpace(name), strings.TrimSpace(text)
		id, known := schema.Lookup(name)
		if !known {
			t.Fatalf("unknown property %q in test", name)
		}
		src := pulp.NewSource("test", text)
		var diags pulp.Diagnostics
		values := pulp.ParseValues(src, pulp.Span{Start: 0, End: len(text)}, text, &diags)
		if diags.HasErrors() {
			t.Fatalf("test declaration %q: %v", declaration, diags)
		}
		props.Set(id, values)
	}
	return props
}

// formatTicks renders a length as a Pulp literal, for the table-driven tests
// that want to state a height in ticks and a pitch in the same breath.
func formatTicks(t geom.Tick) string {
	text := strconv.FormatFloat(t.Points(), 'f', -1, 64)
	return text + "pt"
}

// ---- Font-dependent behaviour ----

// embeddedFonts resolves against the built-in IBM Plex faces. Most tests here
// need no fonts at all; these two do, because the numbers under test come out
// of a real face's metrics.
type embeddedFonts struct{ registry *fonts.Registry }

func (e embeddedFonts) Resolve(family string, style fonts.Style) *fonts.Face {
	face, _, err := e.registry.Resolve(family, style)
	if err != nil {
		return nil
	}
	return face
}

func buildWithFonts(t *testing.T, declarations ...string) Decoration {
	t.Helper()
	d, err := New(schemaProps(t, declarations...), embeddedFonts{fonts.NewRegistry()})
	if err != nil {
		t.Fatalf("New(%v): %v", declarations, err)
	}
	return d
}

// `baseline-on-rule: false` moves the TEXT, not the rule: writing sits a descent
// above the line so a descender just touches it. The ink stays exactly where
// Rule B put it.
func TestBaselineOnRuleMovesTextNotInk(t *testing.T) {
	content := geom.Rect{X: 0, Y: 0, W: geom.Pt(300), H: geom.Pt(200)}
	on := buildWithFonts(t, "line-style: ruled", "line-pitch: 20pt", "font-size: 10pt")
	off := buildWithFonts(t, "line-style: ruled", "line-pitch: 20pt", "font-size: 10pt", "baseline-on-rule: false")

	if !equalTicks(horizontalYs(draw(on, content, Grid{})), horizontalYs(draw(off, content, Grid{}))) {
		t.Fatal("baseline-on-rule moved the drawn rules; it should only move the text")
	}

	face := embeddedFonts{fonts.NewRegistry()}.Resolve("IBM Plex Mono", fonts.Regular)
	if face == nil {
		t.Fatal("no embedded face to measure against")
	}
	descent := face.Descent(quarterPoints(geom.Pt(10)))
	if descent <= 0 {
		t.Fatal("the face reports no descent")
	}

	rules, baselines := on.Baselines(content), off.Baselines(content)
	if len(rules) != len(baselines) {
		t.Fatalf("%d baselines against %d rules", len(baselines), len(rules))
	}
	for i := range rules {
		if want := rules[i] - descent; baselines[i] != want {
			t.Fatalf("baseline %d at %d, want a descent above the rule at %d", i, baselines[i], want)
		}
	}
}

// The hour labels hug the top of their slot, right-aligned in the gutter, which
// is what every calendar app does: the label names the hour that begins there.
func TestTimeGridLabels(t *testing.T) {
	content := geom.Rect{X: geom.In(1), Y: geom.In(1), W: geom.In(3), H: geom.Pt(280)}
	d := buildWithFonts(t, "line-style: time-grid", `time-start: "7:00"`, `time-end: "21:00"`,
		"time-gutter: 34pt", "font-size: 10pt")
	ops := draw(d, content, Grid{})

	var labels []render.Op
	for _, op := range ops {
		if op.Kind == render.OpText {
			labels = append(labels, op)
		}
	}
	if len(labels) != 14 {
		t.Fatalf("%d labels, want one per slot", len(labels))
	}
	if labels[0].Text != "7" || labels[5].Text != "12p" || labels[6].Text != "1" {
		t.Errorf("labels read %q, %q, %q; want 7, 12p, 1", labels[0].Text, labels[5].Text, labels[6].Text)
	}

	face := embeddedFonts{fonts.NewRegistry()}.Resolve("IBM Plex Mono", fonts.Regular)
	size := labelSizeQpt(quarterPoints(geom.Pt(10)))
	slotTops := d.Baselines(content)
	for i, label := range labels {
		if want := slotTops[i] + face.Ascent(size) + timeLabelPad; ticks(label.Args[1]) != want {
			t.Errorf("label %d baseline at %v, want %d below its slot top", i, ticks(label.Args[1]), want)
		}
		width := face.Width(label.Text, size, 0)
		if want := content.X + geom.Pt(34) - width; ticks(label.Args[0]) != want {
			t.Errorf("label %d starts at %v, want right-aligned at %d", i, ticks(label.Args[0]), want)
		}
	}
}
