// Package paint holds the colour model shared by the DSL front-end and the
// PDF renderer.
//
// This is a tool for making paper, so colour is modelled with printing in mind:
// a colour remembers the space it was authored in and is emitted to the PDF in
// that same space. A grey written as gray(0.85) becomes PDF DeviceGray 0.85 —
// not an RGB triple that a RIP then has to convert back — so the tint the
// author wrote is the tint the printer receives. That round-trip fidelity is
// why Color is not simply a color.RGBA.
package paint

import "fmt"

// Space is the colour space a Color was authored in and will be emitted in.
type Space uint8

const (
	// SpaceGray is PDF DeviceGray: one channel, 0 = black, 1 = white. The
	// shipped themes use nothing else, because it is the only space where what
	// you write is exactly what the printer is asked for.
	SpaceGray Space = iota
	// SpaceRGB is PDF DeviceRGB. What hex literals and named colours produce.
	SpaceRGB
	// SpaceCMYK is PDF DeviceCMYK, for authors doing real print separation.
	SpaceCMYK
)

// String returns the space's name, used in diagnostics.
func (s Space) String() string {
	switch s {
	case SpaceRGB:
		return "rgb"
	case SpaceCMYK:
		return "cmyk"
	default:
		return "gray"
	}
}

// Color is a colour plus an alpha channel. Channel values are normalised to
// [0,1]. Alpha below 1 becomes a PDF extended graphics state; it is honoured
// but discouraged, since transparency and print do not always agree.
type Color struct {
	Space      Space
	R, G, B    float64 // valid when Space == SpaceRGB
	C, M, Y, K float64 // valid when Space == SpaceCMYK
	Gray       float64 // valid when Space == SpaceGray
	Alpha      float64
}

// Transparent is the zero-ink colour. A node whose background is Transparent
// paints nothing at all, which is different from painting white — white ink on
// white paper still costs a fill operation and still knocks out anything below.
var Transparent = Color{Space: SpaceGray, Gray: 1, Alpha: 0}

// Black and White are the two colours the defaults lean on.
var (
	Black = Color{Space: SpaceGray, Gray: 0, Alpha: 1}
	White = Color{Space: SpaceGray, Gray: 1, Alpha: 1}
)

// RGB builds an opaque DeviceRGB colour from components in [0,1].
func RGB(r, g, b float64) Color {
	return Color{Space: SpaceRGB, R: clamp01(r), G: clamp01(g), B: clamp01(b), Alpha: 1}
}

// RGB8 builds an opaque DeviceRGB colour from 8-bit components, which is what
// a hex literal parses into.
func RGB8(r, g, b uint8) Color {
	return RGB(float64(r)/255, float64(g)/255, float64(b)/255)
}

// Gray builds an opaque DeviceGray colour. 0 is black, 1 is white.
func GrayN(v float64) Color {
	return Color{Space: SpaceGray, Gray: clamp01(v), Alpha: 1}
}

// CMYK builds an opaque DeviceCMYK colour.
func CMYK(c, m, y, k float64) Color {
	return Color{Space: SpaceCMYK, C: clamp01(c), M: clamp01(m), Y: clamp01(y), K: clamp01(k), Alpha: 1}
}

// WithAlpha returns the colour at a different opacity.
func (c Color) WithAlpha(a float64) Color { c.Alpha = clamp01(a); return c }

// IsInvisible reports whether painting with this colour would deposit no ink.
func (c Color) IsInvisible() bool { return c.Alpha <= 0 }

// ToRGB converts to DeviceRGB components in [0,1], for previews, debug output
// and the naive conversions a viewer would do anyway. Grey maps to equal
// channels; CMYK uses the standard naive conversion, which is good enough for
// on-screen approximation and is never used on the print path.
func (c Color) ToRGB() (r, g, b float64) {
	switch c.Space {
	case SpaceGray:
		return c.Gray, c.Gray, c.Gray
	case SpaceCMYK:
		return (1 - c.C) * (1 - c.K), (1 - c.M) * (1 - c.K), (1 - c.Y) * (1 - c.K)
	default:
		return c.R, c.G, c.B
	}
}

// ToRGB8 converts to 8-bit RGB, rounding half away from zero.
func (c Color) ToRGB8() (r, g, b uint8) {
	rf, gf, bf := c.ToRGB()
	return to8(rf), to8(gf), to8(bf)
}

// Luminance returns perceived brightness in [0,1] using Rec. 709 weights. Used
// to decide whether a fill is dark enough that writing on it will not be
// legible, which is the check behind the "you cannot write on this panel"
// warning.
func (c Color) Luminance() float64 {
	r, g, b := c.ToRGB()
	return 0.2126*r + 0.7152*g + 0.0722*b
}

// InkCoverage estimates the fraction of full ink this colour deposits, used for
// the coverage figure in the build summary and the heavy-coverage warning.
// For CMYK it is total area coverage normalised by the four-channel maximum.
func (c Color) InkCoverage() float64 {
	if c.Alpha <= 0 {
		return 0
	}
	switch c.Space {
	case SpaceGray:
		return (1 - c.Gray) * c.Alpha
	case SpaceCMYK:
		return (c.C + c.M + c.Y + c.K) / 4 * c.Alpha
	default:
		return (1 - c.Luminance()) * c.Alpha
	}
}

// Desaturate converts a colour to its DeviceGray equivalent, preserving alpha.
// This backs the --grayscale flag, which is a property of a particular print
// run rather than of the document, and so is applied at render time rather than
// by swapping the theme.
func (c Color) Desaturate() Color {
	if c.Space == SpaceGray {
		return c
	}
	return Color{Space: SpaceGray, Gray: c.Luminance(), Alpha: c.Alpha}
}

// Mix blends two colours by weight t in [0,1], where 0 is the receiver. Mixing
// happens in RGB and the result is RGB unless both inputs are grey, in which
// case staying in DeviceGray preserves the exact-tint guarantee.
func (c Color) Mix(o Color, t float64) Color {
	t = clamp01(t)
	if c.Space == SpaceGray && o.Space == SpaceGray {
		return Color{Space: SpaceGray, Gray: lerp(c.Gray, o.Gray, t), Alpha: lerp(c.Alpha, o.Alpha, t)}
	}
	r1, g1, b1 := c.ToRGB()
	r2, g2, b2 := o.ToRGB()
	return Color{Space: SpaceRGB, R: lerp(r1, r2, t), G: lerp(g1, g2, t), B: lerp(b1, b2, t), Alpha: lerp(c.Alpha, o.Alpha, t)}
}

// Lighten moves a colour toward white by amount, Darken toward black.
func (c Color) Lighten(amount float64) Color { return c.Mix(White, amount) }
func (c Color) Darken(amount float64) Color  { return c.Mix(Black, amount) }

// String renders the colour the way it would be written in a .pulp file, which
// makes it usable directly in diagnostics and in `themes --show` output.
func (c Color) String() string {
	switch c.Space {
	case SpaceGray:
		if c.Alpha < 1 {
			return fmt.Sprintf("gray(%g, %g)", c.Gray, c.Alpha)
		}
		return fmt.Sprintf("gray(%g)", c.Gray)
	case SpaceCMYK:
		return fmt.Sprintf("cmyk(%g %g %g %g)", c.C, c.M, c.Y, c.K)
	default:
		r, g, b := c.ToRGB8()
		if c.Alpha < 1 {
			return fmt.Sprintf("#%02x%02x%02x%02x", r, g, b, to8(c.Alpha))
		}
		return fmt.Sprintf("#%02x%02x%02x", r, g, b)
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func lerp(a, b, t float64) float64 { return a + (b-a)*t }

func to8(v float64) uint8 { return uint8(clamp01(v)*255 + 0.5) }
