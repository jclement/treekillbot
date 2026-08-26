package decor

import (
	"strings"
	"testing"

	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/render"
)

// dayPanel is a day-planner column whose height is deliberately not a multiple
// of fourteen ticks, so the exact partition has a remainder to place.
var dayPanel = geom.Rect{X: geom.In(1), Y: geom.In(1), W: geom.In(3), H: geom.Pt(200)}

// The one place the floor rule is wrong. Time is continuous and bounded: a grid
// that says 7am to 9pm must END at 9pm, on the bottom edge, not 7pt above it.
func TestTimeGridFitsExactly(t *testing.T) {
	d := build(t, "line-style: time-grid", `time-start: "7:00"`, `time-end: "21:00"`)
	boundaries := slotRules(t, d, dayPanel)

	if len(boundaries) != 15 {
		t.Fatalf("%d slot rules, want 15 for fourteen hours", len(boundaries))
	}
	if boundaries[0] != dayPanel.Y {
		t.Errorf("first rule at %d, want the top edge %d", boundaries[0], dayPanel.Y)
	}
	if last := boundaries[len(boundaries)-1]; last != dayPanel.Bottom() {
		t.Errorf("last rule at %d, want the bottom edge %d", last, dayPanel.Bottom())
	}
}

// The rows are an exact partition, so their heights differ by at most one tick
// and sum to the height with nothing left over.
func TestTimeGridRowsPartitionTheHeight(t *testing.T) {
	d := build(t, "line-style: time-grid")
	boundaries := slotRules(t, d, dayPanel)

	var total geom.Tick
	minHeight, maxHeight := geom.MaxTick, geom.Tick(0)
	for i := 1; i < len(boundaries); i++ {
		height := boundaries[i] - boundaries[i-1]
		total += height
		minHeight, maxHeight = geom.MinTick(minHeight, height), geom.MaxOf(maxHeight, height)
	}
	if total != dayPanel.H {
		t.Errorf("rows sum to %d, want the height %d", total, dayPanel.H)
	}
	if maxHeight-minHeight > 1 {
		t.Errorf("row heights range %d..%d; an exact partition differs by at most a tick", minHeight, maxHeight)
	}
}

// Each slot's marks are partitioned from THAT slot's tick count, so a half-hour
// mark cannot drift away from the middle of its hour even when neighbouring
// slots differ by a tick.
func TestHalfHourMarksDoNotDrift(t *testing.T) {
	d := build(t, "line-style: time-grid", "time-subdivide: 2")
	boundaries := slotRules(t, d, dayPanel)
	marks := subMarks(t, d, dayPanel)

	if len(marks) != len(boundaries)-1 {
		t.Fatalf("%d half-hour marks for %d slots", len(marks), len(boundaries)-1)
	}
	for i, mark := range marks {
		top, bottom := boundaries[i], boundaries[i+1]
		height := bottom - top
		// The exact halving of THIS slot: largest-remainder gives the odd tick
		// to the first part. A mark taken from a neighbouring slot's height, or
		// from a global half-pitch, lands a tick out on the slots that differ.
		want := (height + 1) / 2
		if offset := mark - top; offset != want {
			t.Errorf("slot %d: mark %d ticks into a %d-tick slot, want %d", i, offset, height, want)
		}
	}
}

// Any subdivision partitions its own slot: three marks inside each hour for
// quarter-hours, all strictly inside and strictly increasing.
func TestSubdivisionsStayInsideTheirSlot(t *testing.T) {
	d := build(t, "line-style: time-grid", "time-subdivide: 4")
	boundaries := slotRules(t, d, dayPanel)
	marks := subMarks(t, d, dayPanel)

	if want := (len(boundaries) - 1) * 3; len(marks) != want {
		t.Fatalf("%d marks, want %d", len(marks), want)
	}
	for slot := 0; slot < len(boundaries)-1; slot++ {
		previous := boundaries[slot]
		for j := 0; j < 3; j++ {
			mark := marks[slot*3+j]
			if mark <= previous || mark >= boundaries[slot+1] {
				t.Fatalf("slot %d mark %d at %d is outside (%d, %d)",
					slot, j, mark, previous, boundaries[slot+1])
			}
			previous = mark
		}
	}
}

// A step that does not divide the span is an authoring error we can name
// exactly, which beats silently rounding to something nobody asked for.
func TestTimeGridRejectsBadClocks(t *testing.T) {
	cases := []struct {
		name         string
		declarations []string
		want         string
	}{
		{"unparsable start", []string{"line-style: time-grid", `time-start: "half seven"`}, "time-start"},
		{"backwards", []string{"line-style: time-grid", `time-start: "21:00"`, `time-end: "7:00"`}, "not after"},
		{"ragged span", []string{"line-style: time-grid", `time-start: "7:00"`, `time-end: "21:20"`}, "whole number"},
		{"zero step", []string{"line-style: time-grid", "time-step: 0"}, "positive"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := buildErr(t, tc.declarations...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want one mentioning %q", err, tc.want)
			}
		})
	}
}

// The gutter rule sits between the labels and the writing area, and the rules
// span only the writing area.
func TestTimeGridGutter(t *testing.T) {
	gutter := geom.Pt(34)
	d := build(t, "line-style: time-grid", "time-gutter: 34pt")
	writingLeft := dayPanel.X + gutter + timeLabelGap

	var gutterRules int
	for _, s := range segments(draw(d, dayPanel, Grid{})) {
		if s.X1 == s.X2 {
			gutterRules++
			if want := dayPanel.X + gutter + timeLabelGap/2; s.X1 != want {
				t.Errorf("gutter rule at x=%d, want %d", s.X1, want)
			}
			continue
		}
		if s.X1 != writingLeft || s.X2 != dayPanel.Right() {
			t.Errorf("rule spans %d..%d, want the writing area %d..%d", s.X1, s.X2, writingLeft, dayPanel.Right())
		}
	}
	if gutterRules != 1 {
		t.Errorf("%d gutter rules, want 1", gutterRules)
	}
}

// Labels need a face to measure against; without a font resolver the grid still
// draws, silently, rather than placing text it cannot position.
func TestTimeGridWithoutFontsDrawsNoLabels(t *testing.T) {
	if got := countOps(draw(build(t, "line-style: time-grid"), dayPanel, Grid{}), render.OpText); got != 0 {
		t.Errorf("%d labels drawn with no font resolver", got)
	}
}

// `height: auto` on a day column means the whole day at the author's writing
// pitch, which is more useful than a grid of zero height.
func TestTimeGridNaturalHeight(t *testing.T) {
	d := build(t, "line-style: time-grid", "line-pitch: 20pt", `time-start: "7:00"`, `time-end: "21:00"`)
	if want := geom.Pt(20) * 14; d.NaturalHeight() != want {
		t.Errorf("NaturalHeight %d, want fourteen slots at 20pt = %d", d.NaturalHeight(), want)
	}
}

// Baselines are the slot tops text hangs from — the closing rule at the bottom
// edge is not one of them, because nothing is written after the end of the day.
func TestTimeGridBaselinesExcludeTheClosingRule(t *testing.T) {
	d := build(t, "line-style: time-grid")
	baselines := d.Baselines(dayPanel)
	if len(baselines) != 14 {
		t.Fatalf("%d baselines, want one per slot", len(baselines))
	}
	if last := baselines[len(baselines)-1]; last >= dayPanel.Bottom() {
		t.Errorf("last baseline at %d is on or past the bottom edge %d", last, dayPanel.Bottom())
	}
}

func TestParseClock(t *testing.T) {
	cases := []struct {
		text string
		want int
	}{
		{"7:00", 7 * 60},
		{"7", 7 * 60},
		{"07:30", 7*60 + 30},
		{"7am", 7 * 60},
		{"7 pm", 19 * 60},
		{"12am", 0},
		{"12pm", 12 * 60},
		{"19:00", 19 * 60},
		{"24:00", 24 * 60},
	}
	for _, tc := range cases {
		got, err := parseClock(tc.text)
		if err != nil || got != tc.want {
			t.Errorf("parseClock(%q) = %d, %v; want %d", tc.text, got, err, tc.want)
		}
	}
	for _, bad := range []string{"", "noon", "25:00", "7:99", "13pm", "0am"} {
		if _, err := parseClock(bad); err == nil {
			t.Errorf("parseClock(%q) accepted an invalid time", bad)
		}
	}
}

// The compact form a planner wants: bare numbers, with a meridiem only where a
// bare number would be ambiguous.
func TestClockLabel(t *testing.T) {
	cases := []struct {
		minutes int
		want    string
	}{
		{7 * 60, "7"},
		{11 * 60, "11"},
		{12 * 60, "12p"},
		{13 * 60, "1"},
		{21 * 60, "9"},
		{0, "12a"},
		{7*60 + 30, "7:30"},
	}
	for _, tc := range cases {
		if got := clockLabel(tc.minutes); got != tc.want {
			t.Errorf("clockLabel(%d) = %q, want %q", tc.minutes, got, tc.want)
		}
	}
}

// ---- Recovering the two line batches ----

// slotRules returns the horizontal rules drawn with the slot pen: the ones
// between the sub-slot batch and the end.
func slotRules(t *testing.T, d Decoration, content geom.Rect) []geom.Tick {
	t.Helper()
	batches := horizontalBatches(draw(d, content, Grid{}))
	if len(batches) != 2 {
		t.Fatalf("%d line batches, want one for the sub marks and one for the slot rules", len(batches))
	}
	return batches[1]
}

// subMarks returns the horizontal rules drawn with the lighter sub-slot pen.
func subMarks(t *testing.T, d Decoration, content geom.Rect) []geom.Tick {
	t.Helper()
	batches := horizontalBatches(draw(d, content, Grid{}))
	if len(batches) != 2 {
		t.Fatalf("%d line batches, want 2", len(batches))
	}
	return batches[0]
}

// horizontalBatches groups the horizontal segments by the batch they were
// flushed in, which is how the two pens are told apart.
func horizontalBatches(ops []render.Op) [][]geom.Tick {
	var out [][]geom.Tick
	for _, op := range ops {
		if op.Kind != render.OpStrokeLines {
			continue
		}
		out = append(out, horizontalYs([]render.Op{op}))
	}
	return out
}
