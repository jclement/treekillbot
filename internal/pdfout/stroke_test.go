package pdfout

import (
	"testing"

	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/paint"
	"github.com/jclement/treekillbot/internal/render"
)

// Rule A, stated as arithmetic: a 1pt border declared on a 100pt box must lay
// ink over [0,1] and [99,100] and nowhere else. If the inset is wrong in
// either direction the box stops measuring 100pt, which is the bug border-box
// sizing exists to prevent.
//
// The assertion is on recorded operations rather than on PDF bytes, because in
// a PDF this number is inside a compressed content stream and a failure would
// read "the hash changed".
func TestStrokeBorderRuleA(t *testing.T) {
	tests := []struct {
		name      string
		box       geom.Rect
		width     geom.Tick
		wantPath  geom.Rect
		wantOuter [2]geom.Tick // the [start, end] the stroke's outer edges cover on x
	}{
		{
			name:      "one point border on a hundred point box",
			box:       geom.Rect{W: geom.Pt(100), H: geom.Pt(100)},
			width:     geom.Pt(1),
			wantPath:  geom.Rect{X: geom.Pt(0.5), Y: geom.Pt(0.5), W: geom.Pt(99), H: geom.Pt(99)},
			wantOuter: [2]geom.Tick{0, geom.Pt(100)},
		},
		{
			name:      "hairline",
			box:       geom.Rect{X: geom.Pt(72), Y: geom.Pt(72), W: geom.Pt(200), H: geom.Pt(50)},
			width:     geom.Pt(0.25),
			wantPath:  geom.Rect{X: geom.Pt(72.125), Y: geom.Pt(72.125), W: geom.Pt(199.75), H: geom.Pt(49.75)},
			wantOuter: [2]geom.Tick{geom.Pt(72), geom.Pt(272)},
		},
		{
			name:      "thick border still fits the declared box",
			box:       geom.Rect{W: geom.Pt(40), H: geom.Pt(40)},
			width:     geom.Pt(4),
			wantPath:  geom.Rect{X: geom.Pt(2), Y: geom.Pt(2), W: geom.Pt(36), H: geom.Pt(36)},
			wantOuter: [2]geom.Tick{0, geom.Pt(40)},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			canvas := render.NewOps()
			StrokeBorder(canvas, tc.box, 0, render.Stroke{Color: paint.Black, Width: tc.width})

			ops := canvas.Ops()
			rect := findOp(t, ops, render.OpRect)
			want := []float64{
				tc.wantPath.X.Points(), tc.wantPath.Y.Points(),
				tc.wantPath.W.Points(), tc.wantPath.H.Points(), 0,
			}
			assertFloats(t, rect.Args, want)

			// Restate the same claim the way a printer sees it: where does the
			// ink start and stop?
			half := tc.width.Points() / 2
			gotLeft := rect.Args[0] - half
			gotRight := rect.Args[0] + rect.Args[2] + half
			if gotLeft != tc.wantOuter[0].Points() || gotRight != tc.wantOuter[1].Points() {
				t.Errorf("stroke covers x in [%v,%v], want [%v,%v]",
					gotLeft, gotRight, tc.wantOuter[0].Points(), tc.wantOuter[1].Points())
			}
		})
	}
}

// Rule B: changing a writing rule's weight must not move it. A rule at y=100
// stays at y=100 at any weight, because the rule *is* the line.
func TestStrokeRuleIsCentred(t *testing.T) {
	for _, width := range []geom.Tick{geom.Pt(0.25), geom.Pt(1), geom.Pt(3)} {
		canvas := render.NewOps()
		StrokeRule(canvas, geom.Pt(72), geom.Pt(100), geom.Pt(540), geom.Pt(100),
			render.Stroke{Color: paint.Black, Width: width})
		canvas.FlushLines()

		lines := findOp(t, canvas.Ops(), render.OpStrokeLines)
		assertFloats(t, lines.Args, []float64{72, 100, 540, 100})
	}
}

// A panel's fill and its border have to come off the same path, or a rounded
// corner shows a sliver of background outside the stroke.
func TestFillPanelSharesOneSilhouette(t *testing.T) {
	box := geom.Rect{X: geom.Pt(10), Y: geom.Pt(10), W: geom.Pt(200), H: geom.Pt(100)}
	pen := render.Stroke{Color: paint.Black, Width: geom.Pt(1)}

	canvas := render.NewOps()
	FillPanel(canvas, box, geom.Pt(6), paint.GrayN(0.95), pen)

	ops := canvas.Ops()
	assertKinds(t, ops, []string{render.OpSetFill, render.OpSetStroke, render.OpRect, render.OpFillStroke})
	// One rect, painted once: the fill cannot be a different shape from the
	// border if there is only one path.
	assertFloats(t, ops[2].Args, []float64{10.5, 10.5, 199, 99, 5.5})
}

// With no border there is nothing to inset against, so the fill takes the
// declared rect exactly.
func TestFillPanelWithoutBorderUsesTheDeclaredRect(t *testing.T) {
	box := geom.Rect{X: geom.Pt(10), Y: geom.Pt(10), W: geom.Pt(200), H: geom.Pt(100)}
	canvas := render.NewOps()
	FillPanel(canvas, box, geom.Pt(6), paint.GrayN(0.95), render.Stroke{})
	ops := canvas.Ops()
	assertKinds(t, ops, []string{render.OpSetFill, render.OpRect, render.OpFill})
	assertFloats(t, ops[1].Args, []float64{10, 10, 200, 100, 6})
}

// A border wider than its corner radius has square corners; leaving a negative
// radius in the path would put a nonsense number in the content stream.
func TestBorderRadiusCollapsesToSquare(t *testing.T) {
	tests := []struct {
		radius, width, want geom.Tick
	}{
		{geom.Pt(6), geom.Pt(1), geom.Pt(5.5)},
		{geom.Pt(1), geom.Pt(4), 0},
		{0, geom.Pt(1), 0},
	}
	for _, tc := range tests {
		if got := borderRadius(tc.radius, tc.width); got != tc.want {
			t.Errorf("borderRadius(%v, %v) = %v, want %v", tc.radius, tc.width, got, tc.want)
		}
	}
}

// An invisible pen or an empty rect must paint nothing at all — a white-on-
// white fill still knocks out whatever is under it.
func TestHelpersPaintNothingWhenThereIsNoInk(t *testing.T) {
	tests := []struct {
		name string
		draw func(render.Canvas)
	}{
		{"zero-width border", func(c render.Canvas) {
			StrokeBorder(c, geom.Rect{W: geom.Pt(10), H: geom.Pt(10)}, 0, render.Stroke{Color: paint.Black})
		}},
		{"transparent border", func(c render.Canvas) {
			StrokeBorder(c, geom.Rect{W: geom.Pt(10), H: geom.Pt(10)}, 0,
				render.Stroke{Color: paint.Transparent, Width: geom.Pt(1)})
		}},
		{"empty rect", func(c render.Canvas) {
			StrokeBorder(c, geom.Rect{W: 0, H: geom.Pt(10)}, 0,
				render.Stroke{Color: paint.Black, Width: geom.Pt(1)})
		}},
		{"panel with neither fill nor border", func(c render.Canvas) {
			FillPanel(c, geom.Rect{W: geom.Pt(10), H: geom.Pt(10)}, 0, paint.Transparent, render.Stroke{})
		}},
		{"invisible rule", func(c render.Canvas) {
			StrokeRule(c, 0, 0, geom.Pt(10), 0, render.Stroke{Color: paint.Black})
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			canvas := render.NewOps()
			tc.draw(canvas)
			if ops := canvas.Ops(); len(ops) != 0 {
				t.Errorf("recorded %d operations, want none: %+v", len(ops), ops)
			}
		})
	}
}

// ---- helpers ----

func findOp(t *testing.T, ops []render.Op, kind string) render.Op {
	t.Helper()
	for _, op := range ops {
		if op.Kind == kind {
			return op
		}
	}
	t.Fatalf("no %q operation in %v", kind, opKinds(ops))
	return render.Op{}
}

func opKinds(ops []render.Op) []string {
	out := make([]string, len(ops))
	for i, op := range ops {
		out[i] = op.Kind
	}
	return out
}

func assertKinds(t *testing.T, ops []render.Op, want []string) {
	t.Helper()
	got := opKinds(ops)
	if len(got) != len(want) {
		t.Fatalf("recorded %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("recorded %v, want %v", got, want)
		}
	}
}

func assertFloats(t *testing.T, got, want []float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
