// Package geom is the exact-arithmetic foundation of the layout engine.
//
// Every length in treekillbot is a Tick: a signed integer count of 1/16 of a
// PDF point. Nothing in the layout engine is a float64. That is the single
// decision that makes "pixel perfect" mean something — when a container splits
// its height among children, the guarantee that the pieces sum exactly to the
// whole is an integer identity, not a floating-point coincidence that happens
// to hold for the inputs we tested.
//
// 1/16pt was chosen because it is exactly representable in binary (2^-4) *and*
// exactly representable in 4 decimal places (0.0625), so emitting coordinates
// as "%.4f" into the PDF is lossless and round-trips. At 600dpi one tick is
// ~0.52 device pixels — far finer than any printer resolves, yet coarse enough
// that forty stacked table rows cannot accumulate drift.
//
// Floats appear in exactly two places: at the parser boundary (where "0.5in"
// becomes ticks) and at the PDF writer boundary (where ticks become points).
package geom

import "math"

// Tick is a length in 1/16 of a PDF point. See the package comment for why.
type Tick int64

// TicksPerPt is the quantisation grid: 16 ticks to the point.
const TicksPerPt Tick = 16

// MaxTick is a sentinel for "unbounded" in loose constraints. It is far larger
// than any real page (about 8 million points, or 1.8 miles of paper) while
// leaving room to add lengths together without overflowing int64.
const MaxTick Tick = 1 << 40

// Pt converts points to ticks, rounding half away from zero.
func Pt(v float64) Tick { return Tick(math.Round(v * float64(TicksPerPt))) }

// In converts inches to ticks. One inch is 72 points by definition.
func In(v float64) Tick { return Pt(v * 72) }

// Mm converts millimetres to ticks.
//
// The conversion is computed as v*72/25.4 in one expression rather than against
// a precomputed constant like 2.8346. A4 lands on 9524 ticks (595.25pt); the
// 0.009mm deviation from the ISO nominal is invisible on paper, whereas a
// column grid that fails to divide cleanly is not.
func Mm(v float64) Tick { return Pt(v * 72 / 25.4) }

// Cm converts centimetres to ticks.
func Cm(v float64) Tick { return Mm(v * 10) }

// Pc converts picas to ticks. One pica is 12 points.
func Pc(v float64) Tick { return Pt(v * 12) }

// Px converts CSS reference pixels (96 per inch) to ticks.
func Px(v float64) Tick { return Pt(v * 72 / 96) }

// Points converts ticks back to points. This is called at the PDF writer
// boundary and in error messages — never inside the layout engine.
func (t Tick) Points() float64 { return float64(t) / float64(TicksPerPt) }

// Inches converts ticks to inches, for human-facing diagnostics.
func (t Tick) Inches() float64 { return t.Points() / 72 }

// Mm converts ticks to millimetres, for human-facing diagnostics.
func (t Tick) Mm() float64 { return t.Points() * 25.4 / 72 }

// Scale multiplies a length by a rational factor num/den, rounding half away
// from zero. Used for the many "0.62 * line-height" style metrics in the line
// decorations, where going through float64 would reintroduce the drift that
// ticks exist to prevent.
func (t Tick) Scale(num, den int64) Tick {
	if den == 0 {
		panic("geom: Scale by zero denominator")
	}
	n := int64(t) * num
	// Integer division truncates toward zero, so bias by half a denominator in
	// the direction of the sign to get half-away-from-zero.
	if (n < 0) != (den < 0) {
		return Tick((n - den/2) / den)
	}
	return Tick((n + den/2) / den)
}

// Abs returns the absolute value of a length.
func (t Tick) Abs() Tick {
	if t < 0 {
		return -t
	}
	return t
}

// MinTick returns the smaller of two lengths.
func MinTick(a, b Tick) Tick {
	if a < b {
		return a
	}
	return b
}

// MaxOf returns the larger of two lengths.
func MaxOf(a, b Tick) Tick {
	if a > b {
		return a
	}
	return b
}

// Clamp constrains t to [lo, hi]. A hi below lo is treated as absent, since a
// max-height smaller than a min-height is an authoring mistake we would rather
// resolve in favour of the minimum than propagate as a negative size.
func Clamp(t, lo, hi Tick) Tick {
	if hi > 0 && hi >= lo && t > hi {
		t = hi
	}
	if t < lo {
		t = lo
	}
	return t
}
