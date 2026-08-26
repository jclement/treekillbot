// Sizes as authored: fixed, proportional, flexible, or intrinsic.
//
// A Dimension is what a `width` or `height` property holds before layout
// resolves it against an actual amount of space. Keeping the four cases in one
// named type rather than passing around a float and a mode flag is what lets
// the axis resolver be a single readable pipeline instead of a nest of
// conditionals.
package geom

import "fmt"

// SizeMode discriminates a Dimension.
type SizeMode uint8

const (
	// SizeAuto sizes to the content's intrinsic height. The default for height.
	SizeAuto SizeMode = iota
	// SizeFixed is an absolute length.
	SizeFixed
	// SizePercent is a fraction of the parent's content extent on this axis.
	SizePercent
	// SizeFill takes a weighted share of whatever is left over. The default for
	// width, because things fill width unless told otherwise.
	SizeFill
)

// Dimension is an authored size.
type Dimension struct {
	Mode   SizeMode
	Length Tick  // SizeFixed
	Pct    int32 // SizePercent, in basis points: 3000 is 30%
	Weight int32 // SizeFill, in sixteenths: 16 is `fill`, 32 is `fill(2)`
}

// Auto, Fill and the constructors below cover every way a size is written.
var (
	Auto = Dimension{Mode: SizeAuto}
	Fill = Dimension{Mode: SizeFill, Weight: 16}
)

// Fixed returns an absolute dimension.
func Fixed(t Tick) Dimension { return Dimension{Mode: SizeFixed, Length: t} }

// Percent returns a proportional dimension from a percentage value.
func Percent(pct float64) Dimension {
	return Dimension{Mode: SizePercent, Pct: int32(pct*100 + 0.5)}
}

// FillWeight returns a flexible dimension with a relative weight, where 1
// corresponds to a bare `fill`.
func FillWeight(w float64) Dimension {
	return Dimension{Mode: SizeFill, Weight: int32(w*16 + 0.5)}
}

// IsAuto, IsFixed, IsPercent and IsFill test the mode.
func (d Dimension) IsAuto() bool    { return d.Mode == SizeAuto }
func (d Dimension) IsFixed() bool   { return d.Mode == SizeFixed }
func (d Dimension) IsPercent() bool { return d.Mode == SizePercent }
func (d Dimension) IsFill() bool    { return d.Mode == SizeFill }

// Resolve returns the dimension's length against an available extent, for the
// modes that can be resolved in isolation. Fill cannot: it depends on its
// siblings, so it is handled by the axis resolver and reported here as zero
// with ok false.
func (d Dimension) Resolve(available Tick) (Tick, bool) {
	switch d.Mode {
	case SizeFixed:
		return d.Length, true
	case SizePercent:
		// Percentages route through the same exact apportionment as everything
		// else, so that 30% + 70% of a content width sums to that width rather
		// than to within a rounding error of it.
		return DistributeTicks(available, []int32{d.Pct, 10000 - d.Pct})[0], true
	}
	return 0, false
}

// String renders the dimension the way it would be written in a .pulp file.
func (d Dimension) String() string {
	switch d.Mode {
	case SizeFixed:
		return fmt.Sprintf("%gpt", d.Length.Points())
	case SizePercent:
		return fmt.Sprintf("%g%%", float64(d.Pct)/100)
	case SizeFill:
		if d.Weight == 16 {
			return "fill"
		}
		return fmt.Sprintf("fill(%g)", float64(d.Weight)/16)
	}
	return "auto"
}
