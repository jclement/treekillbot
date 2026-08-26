package layout

import (
	"math/rand"
	"testing"

	"github.com/jclement/treekillbot/internal/geom"
)

func fixed(t geom.Tick) AxisItem { return AxisItem{Dim: geom.Fixed(t)} }
func fill() AxisItem             { return AxisItem{Dim: geom.Fill} }
func fillW(w float64) AxisItem   { return AxisItem{Dim: geom.FillWeight(w)} }
func pct(p float64) AxisItem     { return AxisItem{Dim: geom.Percent(p)} }
func auto(n geom.Tick) AxisItem  { return AxisItem{Dim: geom.Auto, Natural: n} }

func sum(ts []geom.Tick) geom.Tick {
	var s geom.Tick
	for _, t := range ts {
		s += t
	}
	return s
}

// The headline guarantee: when anything on the axis can absorb slack, the
// children plus the gaps consume the available extent exactly.
func TestFillConsumesEverythingExactly(t *testing.T) {
	tests := []struct {
		name  string
		avail geom.Tick
		gap   geom.Tick
		items []AxisItem
	}{
		{"three fills", geom.Pt(100), 0, []AxisItem{fill(), fill(), fill()}},
		{"three fills with gaps", geom.Pt(100), geom.Pt(3), []AxisItem{fill(), fill(), fill()}},
		{"seven day columns", geom.In(10), geom.Pt(5), []AxisItem{fill(), fill(), fill(), fill(), fill(), fill(), fill()}},
		{"fixed then fill", geom.Pt(100), geom.Pt(7), []AxisItem{fixed(geom.Pt(30)), fill()}},
		{"auto then fill", geom.Pt(100), 0, []AxisItem{auto(geom.Pt(17)), fill()}},
		{"weighted fills", geom.Pt(100), 0, []AxisItem{fillW(1), fillW(2), fillW(3)}},
		{"awkward extent", 1601, geom.Pt(1), []AxisItem{fill(), fill(), fill()}},
		{"percent and fill", geom.Mm(210), geom.Pt(4), []AxisItem{pct(30), fill()}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveAxis(tt.avail, tt.gap, tt.items)
			if got.Used != tt.avail {
				t.Fatalf("used %d ticks of %d (sizes %v, gap %d)", got.Used, tt.avail, got.Sizes, tt.gap)
			}
			if got.Overflow != 0 {
				t.Fatalf("unexpected overflow %d", got.Overflow)
			}
		})
	}
}

// 30% + 70% must land on the content width to the tick. This is the case that
// motivated the two-stage percentage apportionment.
func TestPercentagesSummingToOneHundredAreExact(t *testing.T) {
	for _, avail := range []geom.Tick{geom.In(8.5), geom.Mm(210), 9999, 10001, 1, 7} {
		for _, gap := range []geom.Tick{0, geom.Pt(5), 1} {
			got := ResolveAxis(avail, gap, []AxisItem{pct(30), pct(70)})
			want := avail - gap
			if want < 0 {
				want = 0
			}
			if s := sum(got.Sizes); s != want {
				t.Fatalf("avail=%d gap=%d: 30%%+70%% = %v summing to %d, want %d",
					avail, gap, got.Sizes, s, want)
			}
		}
	}
}

func TestPercentagesResolveAgainstContentNotLeftover(t *testing.T) {
	// A percentage must mean the same thing regardless of what sits beside it.
	content := geom.Pt(1000)
	alone := ResolveAxis(content, 0, []AxisItem{pct(25), fill()})
	beside := ResolveAxis(content, 0, []AxisItem{fixed(geom.Pt(400)), pct(25), fill()})
	if alone.Sizes[0] != beside.Sizes[1] {
		t.Fatalf("25%% resolved to %d alone but %d beside a fixed child",
			alone.Sizes[0], beside.Sizes[1])
	}
	if alone.Sizes[0] != geom.Pt(250) {
		t.Fatalf("25%% of 1000pt = %d ticks, want %d", alone.Sizes[0], geom.Pt(250))
	}
}

func TestFixedChildrenAreNeverOverridden(t *testing.T) {
	// Even when the axis is over-committed, an explicit length survives; only
	// auto children give ground.
	got := ResolveAxis(geom.Pt(100), 0, []AxisItem{
		fixed(geom.Pt(80)),
		{Dim: geom.Auto, Natural: geom.Pt(60), Min: geom.Pt(10)},
	})
	if got.Sizes[0] != geom.Pt(80) {
		t.Fatalf("fixed child shrank to %d", got.Sizes[0])
	}
	if got.Sizes[1] != geom.Pt(20) {
		t.Fatalf("auto child = %d ticks, want %d after absorbing the excess", got.Sizes[1], geom.Pt(20))
	}
	if got.Overflow != 0 {
		t.Fatalf("overflow = %d, want 0: the auto child could absorb it all", got.Overflow)
	}
}

// A form that does not fit is a document bug, and the engine must say so rather
// than quietly squashing something (DESIGN.md D9).
func TestOverflowIsReportedNotHidden(t *testing.T) {
	got := ResolveAxis(geom.Pt(100), 0, []AxisItem{
		fixed(geom.Pt(80)),
		fixed(geom.Pt(50)),
	})
	if got.Overflow != geom.Pt(30) {
		t.Fatalf("overflow = %d ticks (%.2fpt), want %d", got.Overflow, got.Overflow.Points(), geom.Pt(30))
	}
}

func TestAutoShrinkStopsAtTheMinimum(t *testing.T) {
	// 30pt of space, a child wanting 100pt but refusing to go below 40pt: it
	// shrinks to exactly its floor and the remaining 10pt is reported rather
	// than silently swallowed.
	got := ResolveAxis(geom.Pt(30), 0, []AxisItem{
		{Dim: geom.Auto, Natural: geom.Pt(100), Min: geom.Pt(40)},
	})
	if got.Sizes[0] != geom.Pt(40) {
		t.Fatalf("size = %d, want the minimum %d", got.Sizes[0], geom.Pt(40))
	}
	if got.Overflow != geom.Pt(10) {
		t.Fatalf("overflow = %d ticks (%.2fpt), want %d", got.Overflow, got.Overflow.Points(), geom.Pt(10))
	}
}

// Shrinking above the floor is not overflow: a child that can absorb the whole
// excess without breaching its minimum has simply done its job.
func TestAutoShrinksWithoutOverflowWhenItCan(t *testing.T) {
	got := ResolveAxis(geom.Pt(50), 0, []AxisItem{
		{Dim: geom.Auto, Natural: geom.Pt(100), Min: geom.Pt(40)},
	})
	if got.Sizes[0] != geom.Pt(50) {
		t.Fatalf("size = %d, want %d", got.Sizes[0], geom.Pt(50))
	}
	if got.Overflow != 0 {
		t.Fatalf("overflow = %d, want 0", got.Overflow)
	}
}

func TestMaxBoundFreezesAndRedistributes(t *testing.T) {
	// The first child would take 50pt as a fill but is capped at 20pt; the
	// other 30pt must go to its sibling, not vanish.
	got := ResolveAxis(geom.Pt(100), 0, []AxisItem{
		{Dim: geom.Fill, Max: geom.Pt(20)},
		fill(),
	})
	if got.Sizes[0] != geom.Pt(20) {
		t.Fatalf("capped child = %d, want %d", got.Sizes[0], geom.Pt(20))
	}
	if got.Sizes[1] != geom.Pt(80) {
		t.Fatalf("sibling = %d, want %d", got.Sizes[1], geom.Pt(80))
	}
	if got.Used != geom.Pt(100) {
		t.Fatalf("used = %d, want the full extent", got.Used)
	}
}

func TestNoFillLeavesLeftoverForTheCaller(t *testing.T) {
	got := ResolveAxis(geom.Pt(100), 0, []AxisItem{fixed(geom.Pt(20)), fixed(geom.Pt(30))})
	if got.Leftover != geom.Pt(50) {
		t.Fatalf("leftover = %d, want %d", got.Leftover, geom.Pt(50))
	}
	if got.Used != geom.Pt(50) {
		t.Fatalf("used = %d, want %d", got.Used, geom.Pt(50))
	}
}

func TestWeightedFillsSplitProportionally(t *testing.T) {
	got := ResolveAxis(geom.Pt(120), 0, []AxisItem{fillW(1), fillW(2)})
	if got.Sizes[0] != geom.Pt(40) || got.Sizes[1] != geom.Pt(80) {
		t.Fatalf("sizes = %v, want 40pt and 80pt", got.Sizes)
	}
}

func TestOffsetsHaveNoTrailingGap(t *testing.T) {
	offs := Offsets(geom.Pt(10), []geom.Tick{geom.Pt(20), geom.Pt(30)}, geom.Pt(5))
	if offs[0] != geom.Pt(10) || offs[1] != geom.Pt(35) {
		t.Fatalf("offsets = %v", offs)
	}
}

// Randomised: whenever the axis holds at least one fill child and is not
// over-committed, the result must consume the extent exactly. This is the
// invariant the whole engine rests on, so it is worth hammering.
func TestResolveAxisExactnessProperty(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for trial := 0; trial < 20000; trial++ {
		n := 1 + rng.Intn(8)
		items := make([]AxisItem, n)
		hasFill := false
		var committed geom.Tick
		for i := range items {
			switch rng.Intn(4) {
			case 0:
				l := geom.Tick(rng.Intn(400))
				items[i] = fixed(l)
				committed += l
			case 1:
				items[i] = pct(float64(rng.Intn(40)))
			case 2:
				items[i] = auto(geom.Tick(rng.Intn(300)))
			default:
				items[i] = fillW(float64(1 + rng.Intn(3)))
				hasFill = true
			}
		}
		gap := geom.Tick(rng.Intn(30))
		avail := geom.Tick(2000 + rng.Intn(12000))

		got := ResolveAxis(avail, gap, items)

		if len(got.Sizes) != n {
			t.Fatalf("returned %d sizes for %d items", len(got.Sizes), n)
		}
		for i, s := range got.Sizes {
			if s < 0 {
				t.Fatalf("trial %d: child %d got a negative size %d", trial, i, s)
			}
		}
		if hasFill && got.Overflow == 0 && committed < avail {
			if got.Used != avail {
				t.Fatalf("trial %d: used %d of %d (sizes %v, gap %d)", trial, got.Used, avail, got.Sizes, gap)
			}
		}
		if got.Used-gap*geom.Tick(n-1) != sum(got.Sizes) {
			t.Fatalf("trial %d: Used does not equal sizes plus gaps", trial)
		}
	}
}

func TestResolveAxisIsDeterministic(t *testing.T) {
	items := []AxisItem{pct(30), fill(), auto(geom.Pt(12)), fillW(2), fixed(geom.Pt(9))}
	first := ResolveAxis(geom.Mm(297), geom.Pt(4), items)
	for i := 0; i < 500; i++ {
		again := ResolveAxis(geom.Mm(297), geom.Pt(4), items)
		for j := range first.Sizes {
			if again.Sizes[j] != first.Sizes[j] {
				t.Fatalf("run %d differs: %v vs %v", i, again.Sizes, first.Sizes)
			}
		}
	}
}

func TestEmptyAxis(t *testing.T) {
	got := ResolveAxis(geom.Pt(100), geom.Pt(5), nil)
	if len(got.Sizes) != 0 || got.Leftover != geom.Pt(100) {
		t.Fatalf("empty axis = %+v", got)
	}
}
