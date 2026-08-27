// Reading the property table into the values a decoration draws with.
//
// Every `line-*`, `dot-*`, `grid-*`, `checkbox-*`, `time-*`, `cornell-*` and
// `margin-rule-*` property is resolved here, once, at construction. Keeping the
// lookups in one place means the schema's defaults are quoted in exactly one
// file, and a decoration's Draw is pure geometry with no property access left in
// it.
package decor

import (
	"github.com/jclement/treekillbot/internal/fonts"
	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/paint"
	"github.com/jclement/treekillbot/internal/schema"
)

// Metrics the spec fixes as a proportion of line-pitch. Expressed as integer
// ratios so they go through geom.Tick.Scale and never touch a float.
const (
	checkboxSizeNum, checkboxSizeDen     = 62, 100 // 0.62 * line-pitch
	checkboxGutterNum, checkboxGutterDen = 45, 100 // 0.45 * line-pitch
	checkboxSitNum, checkboxSitDen       = 8, 100  // 0.08 * checkbox size
)

// Several decorations want a weight of their own — the layout spec calls for a
// 0.75pt checkbox, a 0.6pt divider and a 0.25pt half-hour mark against a 0.4pt
// writing rule — but the property table offers exactly one thickness knob,
// `line-width`. Hard-coding three lengths would ignore an author who thickened
// their rules, so each is instead derived from line-width by the ratio between
// the spec's figure and its 0.4pt: 15/8, 3/2 and 5/8. Quantisation puts the
// results within half a tick (0.03pt) of the spec's numbers, and every one is
// floored at minStrokeTicks.
const (
	checkboxWeightNum, checkboxWeightDen = 15, 8 // -> 0.75pt
	dividerWeightNum, dividerWeightDen   = 3, 2  // -> 0.60pt
	subRuleWeightNum, subRuleWeightDen   = 5, 8  // -> 0.25pt
)

// params is every property a decoration reads, resolved once at construction.
// Each concrete decoration embeds it, so the property table is consulted in one
// place and the defaults below are the only copy of the table's defaults.
type params struct {
	style      string
	pitch      geom.Tick
	color      paint.Color
	width      geom.Tick
	inset      geom.Edges
	count      int
	distribute string
	partial    bool

	// textDrop is how far a writing baseline sits ABOVE its rule. Zero under
	// the default `baseline-on-rule: true`, where you write on the line like
	// every ruled notebook ever made; one descent when it is false, so a
	// descender just touches the rule.
	textDrop geom.Tick

	dotPitch  geom.Tick // 0 means "follow the page grid, else line-pitch"
	dotSize   geom.Tick
	majorHint int
	majorWide geom.Tick
	originBox bool

	checkboxSize   geom.Tick
	checkboxRadius geom.Tick
	checkboxGutter geom.Tick
	checkboxRule   bool

	marginRule       bool
	marginRuleOffset geom.Tick
	marginRuleColor  paint.Color

	timeStart     string
	timeEnd       string
	timeStep      int
	timeSubdivide int
	timeGutter    geom.Tick

	cornellCue     geom.Tick
	cornellSummary geom.Dimension

	face      *fonts.Face
	sizeQpt   int32
	textColor paint.Color
}

// readParams resolves the property table into params, using each property's
// documented default as the fallback so that an unresolved bag still draws the
// thing the schema promises.
func readParams(p *schema.Props, resolver FontResolver) params {
	pitch := p.Tick(schema.PLinePitch, geom.Mm(6))
	width := p.Tick(schema.PLineWidth, geom.Pt(0.4))
	sizeQpt := quarterPoints(p.Tick(schema.PFontSize, geom.Pt(9)))

	prm := params{
		style:      p.Enum(schema.PLineStyle, "none"),
		pitch:      pitch,
		color:      p.Color(schema.PLineColor, paint.GrayN(0.78)),
		width:      width,
		inset:      p.Edges(schema.PLineInset, geom.Edges{}),
		count:      p.Int(schema.PLineCount, 0),
		distribute: p.Enum(schema.PLineDistribute, "center"),
		partial:    p.Bool(schema.PLinePartial, false),

		dotPitch:  p.Tick(schema.PDotPitch, 0),
		dotSize:   p.Tick(schema.PDotSize, geom.Pt(0.7)),
		majorHint: p.Int(schema.PGridMajor, 5),
		majorWide: p.Tick(schema.PGridMajorWidth, geom.Pt(0.6)),
		originBox: p.Enum(schema.PGridOrigin, "page") == "box",

		checkboxSize:   p.Tick(schema.PCheckboxSize, 0),
		checkboxRadius: p.Tick(schema.PCheckboxRadius, geom.Pt(0.5)),
		checkboxGutter: p.Tick(schema.PCheckboxGutter, 0),
		checkboxRule:   p.Bool(schema.PCheckboxRule, true),

		marginRule:       p.Bool(schema.PMarginRule, false),
		marginRuleOffset: p.Tick(schema.PMarginRuleOffset, geom.Pt(28)),
		marginRuleColor:  p.Color(schema.PMarginRuleColor, paint.RGB8(0xe0, 0x5a, 0x55)),

		timeStart:     p.Str(schema.PTimeStart, "7:00"),
		timeEnd:       p.Str(schema.PTimeEnd, "21:00"),
		timeStep:      p.Int(schema.PTimeStep, 60),
		timeSubdivide: p.Int(schema.PTimeSubdivide, 2),
		timeGutter:    p.Tick(schema.PTimeGutter, geom.Pt(34)),

		cornellCue:     p.Tick(schema.PCornellCue, geom.Pt(63)),
		cornellSummary: p.Dimension(schema.PCornellSummary, geom.Percent(20)),

		sizeQpt:   sizeQpt,
		textColor: p.Color(schema.PColor, paint.Black),
	}

	// Sizes that default to a proportion of line-pitch rather than to a length,
	// so a square dot grid or a checklist is one number to tune.
	if prm.checkboxSize <= 0 {
		prm.checkboxSize = CheckboxSide(0, pitch)
	}
	if prm.checkboxGutter <= 0 {
		prm.checkboxGutter = pitch.Scale(checkboxGutterNum, checkboxGutterDen)
	}

	if resolver != nil {
		prm.face = resolver.Resolve(p.Str(schema.PFont, "IBM Plex Mono"), fonts.Regular)
	}
	// With baseline-on-rule off, text sits a descent above the rule. Without a
	// face there is no descent to ask for, so the baseline stays on the rule
	// rather than being displaced by a guessed constant.
	if !p.Bool(schema.PBaselineOnRule, true) && prm.face != nil {
		prm.textDrop = prm.face.Descent(prm.sizeQpt)
	}
	return prm
}

// quarterPoints converts a length to quarter-points, the unit font sizes are
// carried in. Mirrors layout's function of the same name; decor needs it for
// the time grid's hour labels and cannot import layout.
func quarterPoints(t geom.Tick) int32 {
	q := int32((int64(t)*4 + int64(geom.TicksPerPt)/2) / int64(geom.TicksPerPt))
	if q < 1 {
		q = 1
	}
	return q
}

// CheckboxSide is the edge length of a checkbox: the stated `checkbox-size`, or
// a proportion of the row pitch when the document did not state one.
//
// Exported so a caller that needs to report the size — the ink dump, which
// exists to catch exactly this sort of resolved value drifting — computes it
// the same way the painter does rather than keeping a second copy of the
// constant.
//
// Note the consequence of the proportional default: two checklists on one sheet
// with different row pitches get visibly different boxes unless the document
// says otherwise. That is the right default for a lone list and the wrong one
// for a page of them, which is a document's call to make.
func CheckboxSide(stated, pitch geom.Tick) geom.Tick {
	if stated > 0 {
		return stated
	}
	return pitch.Scale(checkboxSizeNum, checkboxSizeDen)
}
