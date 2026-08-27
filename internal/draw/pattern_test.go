package draw

import (
	"testing"

	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/paint"
	"github.com/jclement/treekillbot/internal/render"
)

// Each shipped dither must actually carry the density its name claims, or the
// names are lies and nobody can reason about ink coverage from them.
func TestDitherMasksHaveTheirStatedDensity(t *testing.T) {
	tests := map[string]int{
		"dither-12": 2,
		"dither-25": 4,
		"dither-50": 8,
		"dither-75": 12,
	}
	for name, want := range tests {
		mask, ok := ditherMasks[name]
		if !ok {
			t.Fatalf("no mask for %s", name)
		}
		inked := 0
		for _, row := range mask {
			for _, cell := range row {
				if cell {
					inked++
				}
			}
		}
		if inked != want {
			t.Errorf("%s inks %d of 16 cells, want %d", name, inked, want)
		}
	}
}

// The failure that motivated hand-authoring these: at 25%, thresholding a
// standard Bayer matrix inks the same columns on rows 0 and 2, so the dots line
// up vertically and the fill reads as stripes. No two inked rows may share a
// column pattern.
func TestDitherRowsDoNotAlignVertically(t *testing.T) {
	for name, mask := range ditherMasks {
		if name == "dither-50" {
			continue // a checkerboard alternates by definition
		}
		// Only partially inked rows can stripe. A blank row draws nothing and a
		// solid row is solid, so neither can align with anything.
		var inkedRows [][4]bool
		for _, row := range mask {
			inked := 0
			for _, cell := range row {
				if cell {
					inked++
				}
			}
			if inked > 0 && inked < len(row) {
				inkedRows = append(inkedRows, row)
			}
		}
		for i := 0; i < len(inkedRows); i++ {
			for j := i + 1; j < len(inkedRows); j++ {
				if inkedRows[i] == inkedRows[j] {
					t.Errorf("%s has two inked rows with the same column pattern %v; "+
						"the dots will line up and read as stripes", name, inkedRows[i])
				}
			}
		}
	}
}

// A dash array with an odd number of entries flips its meaning every period,
// turning a checkerboard into a mess.
func TestRowDashIsAlwaysEvenLength(t *testing.T) {
	for name, mask := range ditherMasks {
		for index, row := range mask {
			_, dash, ok := rowDash(row, geom.Pt(1))
			if !ok || dash == nil {
				continue // empty or solid rows need no dash
			}
			if len(dash)%2 != 0 {
				t.Errorf("%s row %d produced a %d-entry dash %v; an odd array inverts every period",
					name, index, len(dash), dash)
			}
		}
	}
}

func TestRowDashEdgeCases(t *testing.T) {
	pitch := geom.Pt(1)
	t.Run("empty row draws nothing", func(t *testing.T) {
		if _, _, ok := rowDash([4]bool{}, pitch); ok {
			t.Fatal("a row with no ink should not be drawn")
		}
	})
	t.Run("full row is solid", func(t *testing.T) {
		_, dash, ok := rowDash([4]bool{true, true, true, true}, pitch)
		if !ok || dash != nil {
			t.Fatalf("a fully inked row should stroke solid, got dash %v", dash)
		}
	})
	t.Run("starts at a run boundary", func(t *testing.T) {
		// Ink at cells 1 and 3: the scan must begin at 1, not 0.
		start, dash, ok := rowDash([4]bool{false, true, false, true}, pitch)
		if !ok {
			t.Fatal("expected a dash")
		}
		if start != 1 {
			t.Fatalf("start = %d, want 1 (the first inked cell after a blank)", start)
		}
		if len(dash) != 4 {
			t.Fatalf("dash = %v, want four entries", dash)
		}
	})
}

// The whole reason for the dashed-row technique: a heading band must cost a
// handful of operations, not one per cell.
func TestDitherCostsOneStrokePerRow(t *testing.T) {
	ops := render.NewOps()
	region := geom.Rect{X: 0, Y: 0, W: geom.Pt(500), H: geom.Pt(12)}
	patternFill("dither-50", region, 0, paint.Black, geom.Pt(1), geom.Rect{}, ops)

	strokes := 0
	for _, op := range ops.Ops() {
		if op.Kind == "stroke" || op.Kind == "lines" {
			strokes++
		}
	}
	// Twelve rows of cells, so at most a dozen or so operations. Drawing the
	// same band cell by cell would be six thousand.
	if strokes == 0 || strokes > 20 {
		t.Fatalf("a 500x12pt band at 1pt took %d stroke operations; want roughly one per row", strokes)
	}
}

func TestPatternIsClippedToItsRegion(t *testing.T) {
	ops := render.NewOps()
	patternFill("dither-50", geom.Rect{W: geom.Pt(100), H: geom.Pt(10)}, 0,
		paint.Black, geom.Pt(1), geom.Rect{}, ops)

	var sawClip, sawSave, sawRestore bool
	for _, op := range ops.Ops() {
		switch op.Kind {
		case "clip":
			sawClip = true
		case "save":
			sawSave = true
		case "restore":
			sawRestore = true
		}
	}
	if !sawClip {
		t.Error("the pattern is not clipped; rows are drawn from the lattice origin and would " +
			"otherwise escape the region to the left")
	}
	if !sawSave || !sawRestore {
		t.Error("the clip is not balanced by a save/restore pair")
	}
}

func TestPatternDoesNothingWhenItShouldNot(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		color   paint.Color
		region  geom.Rect
	}{
		{"none", "none", paint.Black, geom.Rect{W: geom.Pt(50), H: geom.Pt(10)}},
		{"empty name", "", paint.Black, geom.Rect{W: geom.Pt(50), H: geom.Pt(10)}},
		{"unknown name", "dither-33", paint.Black, geom.Rect{W: geom.Pt(50), H: geom.Pt(10)}},
		{"invisible ink", "dither-50", paint.Transparent, geom.Rect{W: geom.Pt(50), H: geom.Pt(10)}},
		{"empty region", "dither-50", paint.Black, geom.Rect{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ops := render.NewOps()
			patternFill(tt.pattern, tt.region, 0, tt.color, geom.Pt(1), geom.Rect{}, ops)
			for _, op := range ops.Ops() {
				if op.Kind == "stroke" || op.Kind == "lines" || op.Kind == "fill" {
					t.Fatalf("painted %q when it should have painted nothing", op.Kind)
				}
			}
		})
	}
}

// Patterns anchor to the page, not to each region, so a dither behind one
// heading lines up with the dither behind the next.
func TestPatternAnchorsToThePage(t *testing.T) {
	origin := geom.Rect{X: geom.Pt(36), Y: geom.Pt(36)}
	pitch := geom.Pt(1)

	rowsFor := func(region geom.Rect) []float64 {
		ops := render.NewOps()
		patternFill("dither-50", region, 0, paint.Black, pitch, origin, ops)
		var ys []float64
		for _, op := range ops.Ops() {
			if op.Kind == "move" && len(op.Args) >= 2 {
				ys = append(ys, op.Args[1])
			}
		}
		return ys
	}

	// Two bands whose tops differ by a whole number of cells must produce rows
	// on the same lattice.
	high := rowsFor(geom.Rect{X: geom.Pt(36), Y: geom.Pt(100), W: geom.Pt(80), H: geom.Pt(8)})
	low := rowsFor(geom.Rect{X: geom.Pt(36), Y: geom.Pt(140), W: geom.Pt(80), H: geom.Pt(8)})
	if len(high) == 0 || len(low) == 0 {
		t.Fatal("no rows drawn")
	}
	for _, y := range append(append([]float64{}, high...), low...) {
		// Every row centre sits half a cell off the lattice.
		offset := y - origin.Y.Points()
		if remainder := offset - float64(int(offset)); remainder < 0.49 || remainder > 0.51 {
			t.Fatalf("row at %g is not on the page lattice (offset %g from origin)", y, offset)
		}
	}
}

// The title floats in the pattern rather than sitting after it: that is the
// look, and an earlier version lost it by running the knockout out to the band
// edge whenever the title was edge-aligned.
func TestTitleKnockoutLeavesPatternOnBothSides(t *testing.T) {
	band := geom.Rect{X: 0, Y: 0, W: geom.Pt(400), H: geom.Pt(12)}
	// Title inset 14pt from the left, 100pt wide.
	gap := titleKnockout(band, geom.Pt(14), geom.Pt(100))

	if gap.X <= band.X {
		t.Fatalf("knockout starts at %.2fpt, want a margin of pattern before it", gap.X.Points())
	}
	if gap.Right() >= band.Right() {
		t.Fatalf("knockout ends at %.2fpt, want pattern after it too", gap.Right().Points())
	}
	// It must actually clear the text, with air on each side.
	if gap.X > geom.Pt(14) || gap.Right() < geom.Pt(114) {
		t.Fatalf("knockout %s does not cover the text plus a halo", gap)
	}
}

// A margin too narrow to read as deliberate is absorbed rather than left as a
// sliver of pattern against the edge.
func TestTitleKnockoutAbsorbsASliver(t *testing.T) {
	band := geom.Rect{X: 0, Y: 0, W: geom.Pt(400), H: geom.Pt(12)}

	// One point of title padding: the halo alone would leave nothing sensible.
	gap := titleKnockout(band, geom.Pt(1), geom.Pt(100))
	if gap.X != band.X {
		t.Fatalf("knockout starts at %.2fpt; a sub-3pt margin should be absorbed to the edge", gap.X.Points())
	}

	// The same at the right-hand end.
	right := titleKnockout(band, geom.Pt(299), geom.Pt(100))
	if right.Right() != band.Right() {
		t.Fatalf("knockout ends at %.2fpt, want the band edge %.2fpt",
			right.Right().Points(), band.Right().Points())
	}
}

func TestTitleKnockoutStaysInsideTheBand(t *testing.T) {
	band := geom.Rect{X: geom.Pt(10), Y: geom.Pt(5), W: geom.Pt(200), H: geom.Pt(12)}
	for _, x := range []geom.Tick{geom.Pt(-50), geom.Pt(10), geom.Pt(150), geom.Pt(400)} {
		gap := titleKnockout(band, x, geom.Pt(80))
		// A title placed entirely outside its band has nothing to clear, and an
		// empty rectangle paints nothing. Anything else must be inside.
		if gap.IsEmpty() {
			continue
		}
		if !band.Contains(gap) {
			t.Errorf("knockout %s for x=%.0fpt escapes the band %s", gap, x.Points(), band)
		}
	}
}
