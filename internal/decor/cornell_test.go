package decor

import (
	"testing"

	"github.com/jclement/treekillbot/internal/geom"
)

// cornellPanel is a full-page Cornell area: big enough that the summary band
// lands on its 20% default rather than on either clamp.
var cornellPanel = geom.Rect{X: geom.In(1), Y: geom.In(1), W: geom.In(6.5), H: geom.In(9)}

// The critical detail of the whole decoration: the notes rules and the cue
// column's rules come from ONE computation, so a line continues across the
// divider instead of restarting a fraction of a millimetre off.
func TestCornellRulesCrossTheDivider(t *testing.T) {
	d := build(t, "line-style: cornell", "line-pitch: 6mm", "cornell-cue: 63pt")
	drawn := segments(draw(d, cornellPanel, Grid{}))

	horizontals := 0
	for _, s := range drawn {
		if s.Y1 != s.Y2 {
			continue
		}
		horizontals++
		if s.X1 == cornellPanel.X && s.X2 == cornellPanel.Right() {
			continue
		}
		// The summary divider is the one horizontal that is not a writing rule,
		// and it spans the full width too.
		t.Errorf("rule at y=%d spans %d..%d, want the full width %d..%d",
			s.Y1, s.X1, s.X2, cornellPanel.X, cornellPanel.Right())
	}
	if horizontals < 20 {
		t.Fatalf("only %d horizontal lines on a 9in Cornell page", horizontals)
	}
}

// The notes band and the summary band are separately centred: the summary is
// its own optical unit and does not continue the notes rhythm.
func TestCornellSummaryIsItsOwnBand(t *testing.T) {
	d := build(t, "line-style: cornell", "line-pitch: 6mm")
	rules := d.Baselines(cornellPanel)
	if len(rules) < 2 {
		t.Fatal("no rules")
	}

	summaryHeight := cornellPanel.H.Scale(20, 100)
	summaryTop := cornellPanel.Bottom() - summaryHeight

	var notes, summary []geom.Tick
	for _, y := range rules {
		if y <= summaryTop {
			notes = append(notes, y)
		} else {
			summary = append(summary, y)
		}
	}
	if len(notes) == 0 || len(summary) == 0 {
		t.Fatalf("expected rules in both bands, got %d notes and %d summary", len(notes), len(summary))
	}

	// Each band's own leftover is centred within it, so the gap above the first
	// summary rule is a full pitch plus half that band's remainder — not a
	// continuation of the notes pitch across the divider.
	pitch := geom.Mm(6)
	leftover := summaryHeight - geom.Tick(len(summary))*pitch
	if want := summaryTop + leftover/2 + pitch; summary[0] != want {
		t.Errorf("first summary rule at %d, want %d", summary[0], want)
	}
	if gap := summary[0] - notes[len(notes)-1]; gap == pitch {
		t.Error("the summary band continued the notes rhythm; it should be centred in its own band")
	}
}

// Baselines is ordered top-down across both bands: the caller flows text
// through it and must not have to sort.
func TestCornellBaselinesAreOrdered(t *testing.T) {
	rules := build(t, "line-style: cornell", "line-pitch: 6mm").Baselines(cornellPanel)
	for i := 1; i < len(rules); i++ {
		if rules[i] <= rules[i-1] {
			t.Fatalf("baseline %d (%d) is not below its predecessor (%d)", i, rules[i], rules[i-1])
		}
	}
}

// The two dividers frame the three regions, and both are centre-aligned rules
// rather than borders.
func TestCornellDividers(t *testing.T) {
	cue := geom.Pt(63)
	d := build(t, "line-style: cornell", "line-pitch: 6mm", "cornell-cue: 63pt", "line-width: 0.5pt")
	summaryTop := cornellPanel.Bottom() - cornellPanel.H.Scale(20, 100)

	var vertical, horizontal bool
	for _, s := range segments(draw(d, cornellPanel, Grid{})) {
		if s.X1 == s.X2 && s.X1 == cornellPanel.X+cue {
			vertical = true
			if s.Y1 != cornellPanel.Y || s.Y2 != summaryTop {
				t.Errorf("cue divider spans %d..%d, want %d..%d", s.Y1, s.Y2, cornellPanel.Y, summaryTop)
			}
		}
		if s.Y1 == s.Y2 && s.Y1 == summaryTop {
			horizontal = true
		}
	}
	if !vertical {
		t.Errorf("no cue divider at x=%d", cornellPanel.X+cue)
	}
	if !horizontal {
		t.Errorf("no summary divider at y=%d", summaryTop)
	}
}

// The summary band is clamped at both ends: a proportion of a short panel would
// be too small to write a sentence in, and a proportion of a tall one would eat
// the notes area it is supposed to summarise.
func TestCornellSummaryIsClamped(t *testing.T) {
	cases := []struct {
		name    string
		request string
		content geom.Rect
		want    geom.Tick
	}{
		{"floored", "5%", geom.Rect{X: 0, Y: 0, W: geom.In(4), H: geom.Pt(400)}, summaryMinHeight},
		{"capped", "60%", geom.Rect{X: 0, Y: 0, W: geom.In(4), H: geom.In(9)},
			geom.In(9).Scale(summaryMaxNum, summaryMaxDen)},
		{"honoured", "25%", geom.Rect{X: 0, Y: 0, W: geom.In(4), H: geom.In(9)},
			geom.In(9).Scale(25, 100)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := build(t, "line-style: cornell", "line-pitch: 6mm", "cornell-summary: "+tc.request)
			wantY := tc.content.Bottom() - tc.want

			for _, s := range segments(draw(d, tc.content, Grid{})) {
				if s.X1 == s.X2 && s.Y2 == wantY {
					return // the cue divider stops exactly at the summary band
				}
			}
			t.Fatalf("no cue divider ending at y=%d, so the summary band is not %d tall", wantY, tc.want)
		})
	}
}
