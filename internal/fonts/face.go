// Package fonts loads TrueType faces and answers metric questions about them.
//
// The layout engine needs to know how wide a string will be *before* anything
// is drawn, and it needs that answer to be identical on every machine and every
// run. So all measurement here happens in integer font design units — the
// font's own coordinate space, typically 1000 or 2048 units to the em — and
// converts to ticks exactly once, at the end, in a single expression. Summing
// forty glyph advances as float64 and converting at the end is how a table's
// last column ends up a thousandth of a point off on one machine and not
// another; summing them as int64 cannot drift.
package fonts

import (
	"fmt"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"

	"github.com/jclement/treekillbot/internal/geom"
)

// Style is a face's position within its family.
type Style uint8

const (
	Regular Style = iota
	Bold
	Italic
	BoldItalic
)

// String returns the style's DSL spelling.
func (s Style) String() string {
	switch s {
	case Bold:
		return "bold"
	case Italic:
		return "italic"
	case BoldItalic:
		return "bold-italic"
	default:
		return "regular"
	}
}

// StyleFor maps a weight and an italic flag onto one of the four concrete
// styles we ship. Weights of 600 and above count as bold; there are no
// intermediate weights because we embed static instances, not variable fonts.
func StyleFor(weight int, italic bool) Style {
	bold := weight >= 600
	switch {
	case bold && italic:
		return BoldItalic
	case bold:
		return Bold
	case italic:
		return Italic
	default:
		return Regular
	}
}

// Face is one loaded font file: its metrics, its glyph advances, and the raw
// bytes the PDF writer will embed.
//
// A Face is safe for concurrent use. The underlying sfnt.Buffer is not, so
// every path that touches it holds the mutex; the advance cache lives behind
// the same lock. The cache is only ever read by key and never iterated, so it
// introduces no map-order nondeterminism.
type Face struct {
	Name   string // the family name as written in the DSL, e.g. "IBM Plex Mono"
	Style  Style
	Data   []byte // the original TTF bytes, handed to the PDF writer for embedding
	Source string // where the bytes came from, for `treekillbot fonts` and diagnostics

	font       *sfnt.Font
	unitsPerEm int32

	// Vertical metrics, all in font design units. Descent is stored POSITIVE
	// (a distance below the baseline) because every formula that uses it wants
	// to add it, and a sign convention that flips between fonts and formulas is
	// a reliable source of one-line bugs.
	ascent    int32
	descent   int32
	lineGap   int32
	capHeight int32
	xHeight   int32

	mu       sync.Mutex
	buf      sfnt.Buffer
	advances map[rune]int32
	glyphs   map[rune]sfnt.GlyphIndex
	monoAdv  int32 // non-zero when every glyph shares an advance
}

// Load parses a TrueType or OpenType file and extracts its metrics.
//
// The metrics come from x/image/font/sfnt, which reads the hhea table. We
// deliberately do not consult OS/2's typographic metrics or its
// USE_TYPO_METRICS flag: the choice matters less than the consistency, and
// pinning one source means golden files stay valid across font updates that
// touch only the other table.
func Load(name string, style Style, data []byte, source string) (*Face, error) {
	parsed, err := sfnt.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parsing font %q: %w", name, err)
	}
	f := &Face{
		Name:       name,
		Style:      style,
		Data:       data,
		Source:     source,
		font:       parsed,
		unitsPerEm: int32(parsed.UnitsPerEm()),
		advances:   make(map[rune]int32, 128),
		glyphs:     make(map[rune]sfnt.GlyphIndex, 128),
	}
	if f.unitsPerEm <= 0 {
		return nil, fmt.Errorf("font %q reports %d units per em", name, f.unitsPerEm)
	}

	// Asking for metrics at a ppem equal to unitsPerEm yields design units
	// directly, with no scaling and so no rounding.
	ppem := fixed.Int26_6(f.unitsPerEm << 6)
	metrics, err := parsed.Metrics(&f.buf, ppem, font.HintingNone)
	if err != nil {
		return nil, fmt.Errorf("reading metrics for font %q: %w", name, err)
	}
	f.ascent = int32(metrics.Ascent >> 6)
	f.descent = int32(metrics.Descent >> 6)
	f.capHeight = int32(metrics.CapHeight >> 6)
	f.xHeight = int32(metrics.XHeight >> 6)
	// font.Metrics.Height is ascent+descent+lineGap, so the gap falls out.
	f.lineGap = int32(metrics.Height>>6) - f.ascent - f.descent
	if f.lineGap < 0 {
		f.lineGap = 0
	}

	f.detectMonospace()
	return f, nil
}

// detectMonospace records the shared advance when every glyph has one. A mono
// face is the common case for this tool, and knowing it lets width measurement
// skip the per-rune cache entirely for ASCII runs.
func (f *Face) detectMonospace() {
	probe := []rune{'i', 'M', 'W', '.', '0', 'm'}
	var shared int32
	for i, r := range probe {
		adv, ok := f.advanceLocked(r)
		if !ok {
			return
		}
		if i == 0 {
			shared = adv
			continue
		}
		if adv != shared {
			return
		}
	}
	f.monoAdv = shared
}

// UnitsPerEm returns the font's design grid resolution.
func (f *Face) UnitsPerEm() int32 { return f.unitsPerEm }

// AscentFU, DescentFU, LineGapFU, CapHeightFU and XHeightFU return vertical
// metrics in font design units. Descent is positive.
func (f *Face) AscentFU() int32    { return f.ascent }
func (f *Face) DescentFU() int32   { return f.descent }
func (f *Face) LineGapFU() int32   { return f.lineGap }
func (f *Face) CapHeightFU() int32 { return f.capHeight }
func (f *Face) XHeightFU() int32   { return f.xHeight }

// IsMonospace reports whether every probed glyph shares one advance width.
func (f *Face) IsMonospace() bool { return f.monoAdv > 0 }

// scaleFU converts a length in font design units to ticks at a given size,
// expressed in quarter-points. This is the single conversion point between the
// font's integer world and the layout engine's, and it is one expression so
// that no intermediate rounding can creep in.
//
//	ticks = fontUnits * (sizeQpt/4 points) * (16 ticks/point) / unitsPerEm
//	      = fontUnits * sizeQpt * 4 / unitsPerEm
func (f *Face) scaleFU(fu int64, sizeQpt int32) geom.Tick {
	num := fu * int64(sizeQpt) * 4
	den := int64(f.unitsPerEm)
	if (num < 0) != (den < 0) {
		return geom.Tick((num - den/2) / den)
	}
	return geom.Tick((num + den/2) / den)
}

// Ascent returns the distance from the baseline to the top of the face at the
// given size in quarter-points.
func (f *Face) Ascent(sizeQpt int32) geom.Tick { return f.scaleFU(int64(f.ascent), sizeQpt) }

// Descent returns the positive distance from the baseline to the bottom of the
// face at the given size.
func (f *Face) Descent(sizeQpt int32) geom.Tick { return f.scaleFU(int64(f.descent), sizeQpt) }

// NaturalLineHeight returns ascent + descent + line gap: the leading the font
// designer intended. The DSL's line-height property overrides it, but this is
// what "line-height: normal" resolves to.
func (f *Face) NaturalLineHeight(sizeQpt int32) geom.Tick {
	return f.scaleFU(int64(f.ascent)+int64(f.descent)+int64(f.lineGap), sizeQpt)
}

// CapHeight and XHeight return those metrics as ticks at the given size.
// Cap height is what optical centring of a title within a bar should use —
// centring on the full ascent leaves short titles looking low.
func (f *Face) CapHeight(sizeQpt int32) geom.Tick { return f.scaleFU(int64(f.capHeight), sizeQpt) }
func (f *Face) XHeight(sizeQpt int32) geom.Tick   { return f.scaleFU(int64(f.xHeight), sizeQpt) }

// advanceLocked returns a rune's advance in font units. The caller must hold
// the mutex, or be in a context where no other goroutine can reach the Face.
func (f *Face) advanceLocked(r rune) (int32, bool) {
	if adv, ok := f.advances[r]; ok {
		return adv, adv >= 0
	}
	gid, err := f.font.GlyphIndex(&f.buf, r)
	if err != nil || gid == 0 {
		// Cache the miss as a negative sentinel so a document full of an
		// unavailable glyph does not re-probe the font on every occurrence.
		f.advances[r] = -1
		return 0, false
	}
	ppem := fixed.Int26_6(f.unitsPerEm << 6)
	adv, err := f.font.GlyphAdvance(&f.buf, gid, ppem, font.HintingNone)
	if err != nil {
		f.advances[r] = -1
		return 0, false
	}
	scaled := int32(adv >> 6)
	f.advances[r] = scaled
	f.glyphs[r] = gid
	return scaled, true
}

// HasGlyph reports whether the face can render a rune.
//
// This matters more than it looks: the PDF library silently substitutes a space
// for any glyph it cannot find, so a document containing a checkbox character
// the font lacks renders as a blank with no error anywhere. Callers use this to
// turn that silence into a warning.
func (f *Face) HasGlyph(r rune) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.advanceLocked(r)
	return ok
}

// MissingGlyphs returns the distinct runes in s that the face cannot render,
// in order of first appearance so the diagnostic is stable.
func (f *Face) MissingGlyphs(s string) []rune {
	f.mu.Lock()
	defer f.mu.Unlock()
	var missing []rune
	seen := make(map[rune]bool)
	for _, r := range s {
		if seen[r] {
			continue
		}
		seen[r] = true
		if _, ok := f.advanceLocked(r); !ok {
			missing = append(missing, r)
		}
	}
	return missing
}

// WidthFU returns the advance width of a string in font design units, including
// tracking of trackFU units applied BETWEEN glyphs — n-1 times, not n.
//
// CSS adds letter-spacing after every character including the last, which
// leaves a centred piece of tracked text visibly off-centre by half a space.
// Since tracked small-caps labels are exactly what a form's panel titles are,
// we do the optically correct thing instead and document the difference.
func (f *Face) WidthFU(s string, trackFU int32) int64 {
	if s == "" {
		return 0
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	var total int64
	count := 0
	for _, r := range s {
		count++
		if f.monoAdv > 0 {
			total += int64(f.monoAdv)
			continue
		}
		adv, ok := f.advanceLocked(r)
		if !ok {
			// A missing glyph is drawn as a space by the PDF writer, so it must
			// measure as one or the layout and the ink disagree.
			adv, _ = f.advanceLocked(' ')
		}
		total += int64(adv)
	}
	if count > 1 && trackFU != 0 {
		total += int64(trackFU) * int64(count-1)
	}
	return total
}

// Width returns the advance width of a string in ticks, at a size given in
// quarter-points, with tracking given in ticks.
func (f *Face) Width(s string, sizeQpt int32, tracking geom.Tick) geom.Tick {
	if s == "" {
		return 0
	}
	// Convert tracking from ticks back to font units so the whole sum stays in
	// integer design units and rounds exactly once.
	var trackFU int32
	if tracking != 0 && sizeQpt != 0 {
		trackFU = int32(int64(tracking) * int64(f.unitsPerEm) / (int64(sizeQpt) * 4))
	}
	return f.scaleFU(f.WidthFU(s, trackFU), sizeQpt)
}

// RuneWidth returns one rune's advance in ticks, used by the wrapper when it
// needs to break inside a word that is longer than the line.
func (f *Face) RuneWidth(r rune, sizeQpt int32) geom.Tick {
	f.mu.Lock()
	adv, ok := f.advanceLocked(r)
	if !ok {
		adv, _ = f.advanceLocked(' ')
	}
	f.mu.Unlock()
	return f.scaleFU(int64(adv), sizeQpt)
}
