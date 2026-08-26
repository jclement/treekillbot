// A Canvas that writes down what it was asked to draw instead of drawing it.
//
// This is the tier of test a human can actually review. A PDF byte hash tells
// you something changed; a diff of these ops tells you that a rule moved from
// y=72.0000 to y=72.5000, in the same vocabulary the design uses. It is also
// the only place the stroke-alignment rules can be asserted directly, since in
// a PDF they are buried inside a compressed content stream.
//
// Everything here is deliberately dumb: the numbers are exactly what crossed
// the interface, and nothing is reordered or simplified. The one exception is
// re-installing a pen that is already in force, which is dropped — see
// SetStroke for why that is required rather than merely tidy.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"math"

	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/paint"
)

// Op kinds, named so a golden file reads like a description of the page.
// Painting and state-setting are distinct verbs: "set-stroke" installs a pen,
// "stroke" paints with it.
const (
	OpSave        = "save"
	OpRestore     = "restore"
	OpSetStroke   = "set-stroke"
	OpSetFill     = "set-fill"
	OpMoveTo      = "move"
	OpLineTo      = "line"
	OpCurveTo     = "curve"
	OpClosePath   = "close"
	OpRect        = "rect"
	OpStroke      = "stroke"
	OpFill        = "fill"
	OpFillStroke  = "fill-stroke"
	OpClip        = "clip"
	OpText        = "text"
	OpStrokeLines = "lines"
)

// Ops is a Canvas that records operations. It also implements Recorder, which
// is how the --emit-ops path gets at the result.
//
// An Ops is not safe for concurrent use; neither is any other Canvas, since
// painting is inherently ordered.
type Ops struct {
	ops     []Op
	pending []geom.Tick // DrawLine endpoints awaiting FlushLines, four per line
	pen     Stroke
	penSet  bool
}

// NewOps returns an empty recording canvas.
func NewOps() *Ops { return &Ops{} }

// Ops returns the operations recorded so far, including any lines still
// pending, so that a caller who forgot to call FlushLines still sees them
// rather than silently losing a ruled panel.
func (o *Ops) Ops() []Op {
	o.FlushLines()
	return o.ops
}

// WriteJSONL writes the recorded operations as JSON lines: one object per
// operation, which is the format that diffs usefully and that `jq` can filter
// without a streaming parser.
func (o *Ops) WriteJSONL(w io.Writer) error {
	encoder := json.NewEncoder(w)
	for _, op := range o.Ops() {
		if err := encoder.Encode(op); err != nil {
			return fmt.Errorf("writing ops: %w", err)
		}
	}
	return nil
}

// ---- Graphics state ----

func (o *Ops) Save() {
	o.FlushLines()
	o.record(Op{Kind: OpSave})
}

func (o *Ops) Restore() {
	o.FlushLines()
	o.record(Op{Kind: OpRestore})
}

// SetStroke records the pen, first committing any batch drawn with the old
// one — a batch recorded after its pen changed would claim the wrong weight.
//
// Re-installing the pen already in force records nothing and does not flush.
// This is the one place state is folded, and it earns it twice over: the
// natural ruled-panel loop sets its pen on every iteration, so flushing there
// would mean no batching at all, and eight identical set-stroke lines in a
// golden file are noise rather than information.
func (o *Ops) SetStroke(pen Stroke) {
	if o.penSet && sameStroke(o.pen, pen) {
		return
	}
	o.FlushLines()
	o.pen, o.penSet = pen, true
	o.record(penOp(OpSetStroke, pen))
}

// sameStroke compares two pens. Stroke holds a slice, so == will not do it.
func sameStroke(a, b Stroke) bool {
	if a.Color != b.Color || a.Width != b.Width || a.Phase != b.Phase || a.Cap != b.Cap {
		return false
	}
	if len(a.Dash) != len(b.Dash) {
		return false
	}
	for i := range a.Dash {
		if a.Dash[i] != b.Dash[i] {
			return false
		}
	}
	return true
}

func (o *Ops) SetFill(color paint.Color) {
	o.FlushLines()
	o.record(Op{Kind: OpSetFill, Color: color.String()})
}

// ---- Path construction ----

func (o *Ops) MoveTo(x, y geom.Tick) {
	o.FlushLines()
	o.record(Op{Kind: OpMoveTo, Args: points(x, y)})
}

func (o *Ops) LineTo(x, y geom.Tick) {
	o.FlushLines()
	o.record(Op{Kind: OpLineTo, Args: points(x, y)})
}

func (o *Ops) CurveTo(c1x, c1y, c2x, c2y, x, y geom.Tick) {
	o.FlushLines()
	o.record(Op{Kind: OpCurveTo, Args: points(c1x, c1y, c2x, c2y, x, y)})
}

func (o *Ops) ClosePath() {
	o.FlushLines()
	o.record(Op{Kind: OpClosePath})
}

// AddRect records the rectangle and its corner radius verbatim. The corner
// geometry is not expanded here: a golden file that says "a 100x60 box with
// 6pt corners" is the thing a reviewer wants to read, and expanding it into
// eight Béziers would bury the one number that ever changes.
func (o *Ops) AddRect(r geom.Rect, radius geom.Tick) {
	o.FlushLines()
	o.record(Op{Kind: OpRect, Args: points(r.X, r.Y, r.W, r.H, radius)})
}

// ---- Painting ----

func (o *Ops) Stroke()     { o.FlushLines(); o.record(Op{Kind: OpStroke}) }
func (o *Ops) Fill()       { o.FlushLines(); o.record(Op{Kind: OpFill}) }
func (o *Ops) FillStroke() { o.FlushLines(); o.record(Op{Kind: OpFillStroke}) }
func (o *Ops) Clip()       { o.FlushLines(); o.record(Op{Kind: OpClip}) }

// DrawText records the run's position, content, face and size. Tracking rides
// in the third argument rather than in a field of its own, because Op is the
// golden-file schema and widening it for one property costs every existing
// golden file a rewrite.
func (o *Ops) DrawText(x, y geom.Tick, run TextRun) {
	o.FlushLines()
	op := Op{
		Kind:  OpText,
		Args:  points(x, y, run.Tracking),
		Text:  run.Text,
		Size:  round4(float64(run.SizeQpt) / 4),
		Color: run.Color.String(),
	}
	if run.Face != nil {
		op.Font = run.Face.Name + " " + run.Face.Style.String()
	}
	o.record(op)
}

// ---- Batched lines ----

// DrawLine accumulates one line segment. Nothing is recorded until FlushLines,
// which is what lets a ruled panel of two hundred rules be one operation.
func (o *Ops) DrawLine(x1, y1, x2, y2 geom.Tick) {
	o.pending = append(o.pending, x1, y1, x2, y2)
}

// FlushLines emits every accumulated segment as a single "lines" operation
// whose arguments are x1,y1,x2,y2 per segment. One operation for N lines is
// the assertion that the batching is real; a golden file showing N separate
// operations means it silently regressed.
func (o *Ops) FlushLines() {
	if len(o.pending) == 0 {
		return
	}
	o.ops = append(o.ops, Op{Kind: OpStrokeLines, Args: points(o.pending...)})
	o.pending = o.pending[:0]
}

func (o *Ops) record(op Op) { o.ops = append(o.ops, op) }

// penOp flattens a Stroke into the golden-file shape. A pen that deposits no
// ink still gets recorded — "the border was set to invisible" is exactly the
// kind of thing a reviewer needs to see.
func penOp(kind string, pen Stroke) Op {
	op := Op{Kind: kind, Color: pen.Color.String(), Width: round4(pen.Width.Points())}
	if len(pen.Dash) > 0 {
		op.Dash = points(pen.Dash...)
		op.Args = points(pen.Phase)
	}
	return op
}

// points converts ticks to points, rounded to four decimals. That is lossless
// for tick-quantised values — a tick is 0.0625pt exactly — and it keeps a
// float64's last-bit noise out of the golden files.
func points(ticks ...geom.Tick) []float64 {
	out := make([]float64, len(ticks))
	for i, t := range ticks {
		out[i] = round4(t.Points())
	}
	return out
}

func round4(v float64) float64 { return math.Round(v*1e4) / 1e4 }
