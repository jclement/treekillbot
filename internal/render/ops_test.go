package render

import (
	"bytes"
	"strings"
	"testing"

	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/paint"
)

var _ Canvas = (*Ops)(nil)
var _ Recorder = (*Ops)(nil)

// The whole point of DrawLine existing separately from MoveTo/LineTo/Stroke:
// a ruled panel is one operation, not two hundred.
func TestDrawLineBatchesIntoOneOp(t *testing.T) {
	const rules = 40
	canvas := NewOps()
	canvas.SetStroke(Stroke{Color: paint.Black, Width: geom.Pt(0.25)})
	for i := 0; i < rules; i++ {
		y := geom.Pt(72) + geom.Tick(i)*geom.Pt(18)
		canvas.DrawLine(geom.Pt(72), y, geom.Pt(540), y)
	}
	canvas.FlushLines()

	ops := canvas.Ops()
	lineOps := 0
	for _, op := range ops {
		if op.Kind == OpStrokeLines {
			lineOps++
			if got := len(op.Args); got != rules*4 {
				t.Errorf("batched op carries %d args, want %d", got, rules*4)
			}
		}
	}
	if lineOps != 1 {
		t.Fatalf("%d rules produced %d line operations, want 1", rules, lineOps)
	}
	if ops[len(ops)-1].Args[0] != 72 || ops[len(ops)-1].Args[1] != 72 {
		t.Errorf("first segment = %v, want it to start at (72,72)", ops[len(ops)-1].Args[:4])
	}
}

// Changing the pen mid-batch has to commit what came before it, or forty rules
// drawn at 0.25pt get repainted at 1pt.
func TestSetStrokeFlushesBatchOnPenChange(t *testing.T) {
	thin := Stroke{Color: paint.Black, Width: geom.Pt(0.25)}
	thick := Stroke{Color: paint.Black, Width: geom.Pt(1)}

	canvas := NewOps()
	canvas.SetStroke(thin)
	canvas.DrawLine(0, 0, geom.Pt(100), 0)
	canvas.DrawLine(0, geom.Pt(10), geom.Pt(100), geom.Pt(10))
	canvas.SetStroke(thick)
	canvas.DrawLine(0, geom.Pt(20), geom.Pt(100), geom.Pt(20))
	canvas.FlushLines()

	want := []string{OpSetStroke, OpStrokeLines, OpSetStroke, OpStrokeLines}
	assertKinds(t, canvas.Ops(), want)

	ops := canvas.Ops()
	if got := len(ops[1].Args); got != 8 {
		t.Errorf("first batch has %d args, want 8 (two segments)", got)
	}
	if got := ops[0].Width; got != 0.25 {
		t.Errorf("first pen width = %v, want 0.25", got)
	}
	if got := ops[2].Width; got != 1 {
		t.Errorf("second pen width = %v, want 1", got)
	}
}

// Re-installing the pen already in force must not break the batch, because the
// obvious way to write a ruled panel does exactly that on every iteration.
func TestSetStrokeIdempotentKeepsBatch(t *testing.T) {
	pen := Stroke{Color: paint.GrayN(0.7), Width: geom.Pt(0.25), Dash: []geom.Tick{geom.Pt(2), geom.Pt(2)}}
	canvas := NewOps()
	for i := 0; i < 6; i++ {
		canvas.SetStroke(pen)
		canvas.DrawLine(0, geom.Tick(i)*geom.Pt(12), geom.Pt(100), geom.Tick(i)*geom.Pt(12))
	}
	canvas.FlushLines()
	assertKinds(t, canvas.Ops(), []string{OpSetStroke, OpStrokeLines})
	if got := len(canvas.Ops()[1].Args); got != 24 {
		t.Errorf("batch has %d args, want 24 (six segments)", got)
	}
}

// Every other operation has to commit the batch too, or the recorded order
// stops matching the order the ink lands in.
func TestPendingLinesFlushBeforeOtherOps(t *testing.T) {
	tests := []struct {
		name string
		act  func(*Ops)
	}{
		{"save", func(o *Ops) { o.Save() }},
		{"restore", func(o *Ops) { o.Restore() }},
		{"set fill", func(o *Ops) { o.SetFill(paint.Black) }},
		{"move", func(o *Ops) { o.MoveTo(0, 0) }},
		{"rect", func(o *Ops) { o.AddRect(geom.Rect{W: 16, H: 16}, 0) }},
		{"fill", func(o *Ops) { o.Fill() }},
		{"clip", func(o *Ops) { o.Clip() }},
		{"text", func(o *Ops) { o.DrawText(0, 0, TextRun{Text: "x", Color: paint.Black}) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			canvas := NewOps()
			canvas.DrawLine(0, 0, geom.Pt(10), 0)
			tc.act(canvas)
			ops := canvas.Ops()
			if len(ops) < 2 || ops[0].Kind != OpStrokeLines {
				t.Fatalf("batch was not committed first; got %v", kinds(ops))
			}
		})
	}
}

// Ops() must not lose a batch the caller forgot to flush — silently dropping a
// panel's worth of rules is worse than any diff.
func TestOpsFlushesUnflushedLines(t *testing.T) {
	canvas := NewOps()
	canvas.DrawLine(0, 0, geom.Pt(10), 0)
	assertKinds(t, canvas.Ops(), []string{OpStrokeLines})
}

func TestOpsRecordsGeometryInPoints(t *testing.T) {
	canvas := NewOps()
	canvas.SetFill(paint.GrayN(0.85))
	canvas.AddRect(geom.Rect{X: geom.Pt(10), Y: geom.Pt(20), W: geom.Pt(100), H: geom.Pt(60)}, geom.Pt(6))
	canvas.FillStroke()
	canvas.MoveTo(geom.Pt(1), geom.Tick(1)) // one tick is 0.0625pt exactly
	canvas.LineTo(geom.Pt(2), 0)
	canvas.CurveTo(0, 0, geom.Pt(1), geom.Pt(1), geom.Pt(2), geom.Pt(2))
	canvas.ClosePath()
	canvas.Stroke()

	ops := canvas.Ops()
	assertKinds(t, ops, []string{
		OpSetFill, OpRect, OpFillStroke, OpMoveTo, OpLineTo, OpCurveTo, OpClosePath, OpStroke,
	})
	if got, want := ops[0].Color, "gray(0.85)"; got != want {
		t.Errorf("fill colour = %q, want %q", got, want)
	}
	assertArgs(t, ops[1].Args, []float64{10, 20, 100, 60, 6})
	assertArgs(t, ops[3].Args, []float64{1, 0.0625})
}

func TestPenOpRecordsDash(t *testing.T) {
	canvas := NewOps()
	canvas.SetStroke(Stroke{
		Color: paint.GrayN(0.5),
		Width: geom.Pt(0.5),
		Dash:  []geom.Tick{geom.Pt(2), geom.Pt(1)},
		Phase: geom.Pt(0.5),
	})
	op := canvas.Ops()[0]
	assertArgs(t, op.Dash, []float64{2, 1})
	assertArgs(t, op.Args, []float64{0.5})
	if op.Width != 0.5 {
		t.Errorf("width = %v, want 0.5", op.Width)
	}
}

func TestWriteJSONLIsOnePerLine(t *testing.T) {
	canvas := NewOps()
	canvas.Save()
	canvas.AddRect(geom.Rect{W: geom.Pt(10), H: geom.Pt(10)}, 0)
	canvas.Fill()
	canvas.Restore()

	var buf bytes.Buffer
	if err := canvas.WriteJSONL(&buf); err != nil {
		t.Fatalf("WriteJSONL: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("wrote %d lines, want 4:\n%s", len(lines), buf.String())
	}
	if !strings.HasPrefix(lines[0], `{"op":"save"`) {
		t.Errorf("first line = %q", lines[0])
	}
}

func kinds(ops []Op) []string {
	out := make([]string, len(ops))
	for i, op := range ops {
		out[i] = op.Kind
	}
	return out
}

func assertKinds(t *testing.T, ops []Op, want []string) {
	t.Helper()
	got := kinds(ops)
	if len(got) != len(want) {
		t.Fatalf("recorded %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("recorded %v, want %v", got, want)
		}
	}
}

func assertArgs(t *testing.T, got, want []float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("args %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args %v, want %v", got, want)
		}
	}
}
