// The property table: every name Pulp accepts, its type, and how it cascades.
//
// This table is the schema referred to throughout the design. The parser has no
// idea whether `align` is a property or `panel` is an element; it produces a
// uniform tree of named nodes, and this file is what gives those names meaning.
// Adding a property is a row here, not a grammar change.
//
// Properties are identified by a dense PropID so that a resolved property set
// can be a fixed-size array with a bitmask, which makes the cascade a loop over
// integers rather than a map merge. That matters for more than speed: a map
// merge would put map iteration order on the path to the output, and this tool
// promises byte-identical results (DESIGN.md section 4).
package schema

// PropID identifies a property. The zero value is deliberately invalid so that
// a forgotten lookup fails loudly rather than silently addressing the first
// property in the table.
type PropID uint16

// Kind is a property's value type, used for validation and error messages.
type Kind uint8

const (
	KindLength   Kind = iota // an absolute length: 16pt, 0.5in
	KindSize                 // a length, percentage, fill or auto
	KindColor                // any colour form
	KindString               // free text
	KindEnum                 // one of a fixed set of keywords
	KindBool                 // true/false/yes/no/on/off
	KindNumber               // a unitless number
	KindEdges                // one, two or four lengths: CSS shorthand arity
	KindStyleRef             // one or more named style bundles
	KindPageSize             // a named page size or a width/height pair
	KindInteger              // a whole number, e.g. a line count
)

// String returns the kind's name as it appears in diagnostics.
func (k Kind) String() string {
	switch k {
	case KindLength:
		return "length"
	case KindSize:
		return "size"
	case KindColor:
		return "colour"
	case KindString:
		return "string"
	case KindEnum:
		return "keyword"
	case KindBool:
		return "boolean"
	case KindNumber:
		return "number"
	case KindEdges:
		return "one, two or four lengths"
	case KindStyleRef:
		return "style name"
	case KindPageSize:
		return "page size"
	case KindInteger:
		return "whole number"
	}
	return "value"
}

// PropDef describes one property.
type PropDef struct {
	Name string
	Kind Kind
	Enum []string // for KindEnum

	// Inherited follows one rule, stated once so it is not re-litigated per
	// property: if it describes ink on a glyph or on a ruled line, it inherits;
	// if it describes the box, it does not. That is CSS's model, gotcha
	// included, and it is what makes `font` on a section reach the panels
	// inside it while `height` sensibly does not.
	Inherited bool

	// Default is the value in Pulp source form, parsed once at startup. Writing
	// defaults in the language rather than as Go literals keeps them honest:
	// anything expressible as a default is expressible by a user, and
	// `treekillbot docs props` can print them verbatim.
	Default string

	// AppliesTo lists the elements the property is meaningful on. Empty means
	// every element. This drives the "…is not a property of `panel`" half of
	// the unknown-property error, which is far more useful than a flat
	// "unknown property".
	AppliesTo []string

	Doc string
}

// The property identifiers. Order here defines PropID values; it is internal
// and may change, so nothing persists a PropID.
const (
	PInvalid PropID = iota

	// ---- Typography ----
	PFont
	PFontSize
	PFontWeight
	PFontStyle
	PColor
	PLineHeight
	PTracking
	PAlign
	PValign
	PTextTransform
	PAutoShrink
	PWrap
	PNumericStyle

	// ---- Box ----
	PWidth
	PHeight
	PMinHeight
	PMaxHeight
	PPadding
	PPaddingTop
	PPaddingRight
	PPaddingBottom
	PPaddingLeft
	PMargin
	PMarginTop
	PMarginRight
	PMarginBottom
	PMarginLeft
	PGap
	POverflow

	// ---- Decoration ----
	PBackground
	PBorder
	PBorderWidth
	PBorderTopWidth
	PBorderRightWidth
	PBorderBottomWidth
	PBorderLeftWidth
	PBorderColor
	PBorderStyle
	PBorderRadius
	PBorderCollapse
	POpacity

	// ---- Panel chrome ----
	PTitle
	PTitlePosition
	PTitleAlign
	PTitleFont
	PTitleSize
	PTitleWeight
	PTitleColor
	PTitleBackground
	PTitlePadding
	PTitleStyle
	PTitleTransform
	PTitleTracking

	// ---- Line decorations ----
	PLineStyle
	PLinePitch
	PLineColor
	PLineWidth
	PLineInset
	PLineCount
	PLineDistribute
	PLinePartial
	PBaselineOnRule
	PDotPitch
	PDotSize
	PGridMajor
	PGridMajorWidth
	PCheckboxSize
	PCheckboxRadius
	PCheckboxGutter
	PCheckboxRule
	PGridOrigin
	PMarginRule
	PMarginRuleOffset
	PMarginRuleColor
	PTimeStart
	PTimeEnd
	PTimeStep
	PTimeSubdivide
	PTimeGutter
	PCornellCue
	PCornellSummary

	// ---- Page ----
	PPageSize
	POrientation
	PWeekStart
	PBleed

	// ---- Structural ----
	PStyle
	PWhen
	PID

	numProps
)

// NumProps is the size a resolved property array needs.
const NumProps = int(numProps)

// Element names used in AppliesTo, kept as constants so a typo in the table is
// a compile error rather than a property that silently applies nowhere.
const (
	EPage    = "page"
	ESection = "section"
	EColumn  = "column"
	EPanel   = "panel"
	EText    = "text"
	ERule    = "rule"
	EGrid    = "grid"
	ESpacer  = "spacer"
	EBox     = "box"
	EImage   = "image"
)

// boxElements are the elements that occupy a rectangle and so accept the box
// and decoration properties.
var boxElements = []string{EPage, ESection, EColumn, EPanel, EGrid, EBox, ESpacer, EText, EImage, ERule}

// lineHosts are the elements a line decoration can be drawn inside.
var lineHosts = []string{EPanel, EGrid, EBox, ESection, EColumn}

// strokeHosts additionally includes `rule`, which has no decoration of its own
// but draws itself with the same colour and weight properties.
var strokeHosts = []string{EPanel, EGrid, EBox, ESection, EColumn, ERule}

// props is the table. It is indexed by PropID and validated at startup.
var props = [NumProps]PropDef{
	// ---- Typography. All inherited: this is ink on a glyph. ----
	PFont:     {Name: "font", Kind: KindString, Inherited: true, Default: "IBM Plex Mono", Doc: "Font family. Names resolve case- and space-insensitively."},
	PFontSize: {Name: "font-size", Kind: KindLength, Inherited: true, Default: "9pt", Doc: "Type size."},
	PFontWeight: {Name: "font-weight", Kind: KindEnum, Inherited: true, Default: "regular",
		Enum: []string{"regular", "normal", "bold", "400", "700"}, Doc: "Weight. Only regular and bold exist; static instances are embedded, not variable fonts."},
	PFontStyle: {Name: "font-style", Kind: KindEnum, Inherited: true, Default: "normal",
		Enum: []string{"normal", "italic"}, Doc: "Upright or italic."},
	PColor:      {Name: "color", Kind: KindColor, Inherited: true, Default: "gray(0)", Doc: "Text colour."},
	PLineHeight: {Name: "line-height", Kind: KindNumber, Inherited: true, Default: "1.35", Doc: "Text leading, as a multiple of font-size. Not the ruled-line spacing — that is line-pitch."},
	PTracking:   {Name: "tracking", Kind: KindLength, Inherited: true, Default: "0pt", Doc: "Extra space between glyphs, applied between them and not after the last."},
	PAlign: {Name: "align", Kind: KindEnum, Inherited: true, Default: "left",
		Enum: []string{"left", "center", "centre", "right", "justify"}, Doc: "Horizontal alignment of text within its box."},
	PValign: {Name: "valign", Kind: KindEnum, Inherited: true, Default: "top",
		Enum: []string{"top", "middle", "center", "centre", "bottom", "baseline"},
		Doc:  "Vertical alignment within the box. Inherited, unlike CSS, because it is normally set once on a panel."},
	PTextTransform: {Name: "text-transform", Kind: KindEnum, Inherited: true, Default: "none",
		Enum: []string{"none", "upper", "lower", "title"}, Doc: "Case transformation applied at render time."},
	PAutoShrink: {Name: "auto-shrink", Kind: KindNumber, Inherited: true, Default: "0",
		Doc: "Smallest fraction of font-size text may shrink to in order to fit. 0 disables shrinking."},
	PWrap: {Name: "wrap", Kind: KindBool, Inherited: true, Default: "true", Doc: "Whether text wraps to the next line."},
	PNumericStyle: {Name: "numeric-style", Kind: KindEnum, Inherited: true, Default: "tabular",
		Enum: []string{"tabular", "proportional"}, Doc: "Tabular by default: a planner is columns of digits, and they should line up."},

	// ---- Box. None inherited: this is the box, not the ink. ----
	PWidth: {Name: "width", Kind: KindSize, Default: "fill", AppliesTo: boxElements, Doc: "Width. Everything fills its parent's content width unless told otherwise."},
	// `height` deliberately carries NO default. Seeding every node with `auto`
	// would defeat the layout engine's kind-aware default, under which a column
	// fills its row — and the symptom is subtle: every panel collapses to the
	// height of its own title, with nothing anywhere saying why.
	PHeight:    {Name: "height", Kind: KindSize, AppliesTo: boxElements, Doc: "Height: a length, a percentage, fill, fill(n), or auto. Defaults to auto, except on a column, which fills its row."},
	PMinHeight: {Name: "min-height", Kind: KindLength, Default: "0pt", AppliesTo: boxElements, Doc: "Lower bound on the resolved height."},
	PMaxHeight: {Name: "max-height", Kind: KindLength, Default: "0pt", AppliesTo: boxElements, Doc: "Upper bound on the resolved height. 0 means no bound."},
	PPadding:   {Name: "padding", Kind: KindEdges, Default: "0pt", AppliesTo: boxElements, Doc: "Space inside the border. CSS shorthand arity: 1, 2 or 4 values."},
	PMargin:    {Name: "margin", Kind: KindEdges, Default: "0pt", AppliesTo: boxElements, Doc: "Space outside the border. Margins never collapse."},

	// Per-side refinements. Each overrides its shorthand on one side only, so
	// `padding: 5pt` followed by `padding-top: 12pt` means what it looks like.
	// They carry no default: an unset side falls back to the shorthand, and
	// giving them one would silently defeat it.
	PPaddingTop:    {Name: "padding-top", Kind: KindLength, AppliesTo: boxElements, Doc: "Padding on the top edge only."},
	PPaddingRight:  {Name: "padding-right", Kind: KindLength, AppliesTo: boxElements, Doc: "Padding on the right edge only."},
	PPaddingBottom: {Name: "padding-bottom", Kind: KindLength, AppliesTo: boxElements, Doc: "Padding on the bottom edge only."},
	PPaddingLeft:   {Name: "padding-left", Kind: KindLength, AppliesTo: boxElements, Doc: "Padding on the left edge only."},
	PMarginTop:     {Name: "margin-top", Kind: KindLength, AppliesTo: boxElements, Doc: "Margin on the top edge only."},
	PMarginRight:   {Name: "margin-right", Kind: KindLength, AppliesTo: boxElements, Doc: "Margin on the right edge only."},
	PMarginBottom:  {Name: "margin-bottom", Kind: KindLength, AppliesTo: boxElements, Doc: "Margin on the bottom edge only."},
	PMarginLeft:    {Name: "margin-left", Kind: KindLength, AppliesTo: boxElements, Doc: "Margin on the left edge only."},

	PBorderTopWidth:    {Name: "border-top-width", Kind: KindLength, AppliesTo: boxElements, Doc: "Border thickness on the top edge only."},
	PBorderRightWidth:  {Name: "border-right-width", Kind: KindLength, AppliesTo: boxElements, Doc: "Border thickness on the right edge only."},
	PBorderBottomWidth: {Name: "border-bottom-width", Kind: KindLength, AppliesTo: boxElements, Doc: "Border thickness on the bottom edge only."},
	PBorderLeftWidth:   {Name: "border-left-width", Kind: KindLength, AppliesTo: boxElements, Doc: "Border thickness on the left edge only."},
	PGap:               {Name: "gap", Kind: KindLength, Default: "0pt", AppliesTo: boxElements, Doc: "Space between children."},
	POverflow: {Name: "overflow", Kind: KindEnum, Default: "error", AppliesTo: boxElements,
		Enum: []string{"error", "clip", "shrink", "visible"},
		Doc:  "What to do when content does not fit. An error by default: a form that does not fit is a document bug, and paper cannot scroll."},

	// ---- Decoration ----
	PBackground:   {Name: "background", Kind: KindColor, Default: "transparent", AppliesTo: boxElements, Doc: "Fill colour behind the content."},
	PBorder:       {Name: "border", Kind: KindString, Default: "", AppliesTo: boxElements, Doc: "Shorthand for border-width, border-style and border-color in any order."},
	PBorderWidth:  {Name: "border-width", Kind: KindEdges, Default: "0pt", AppliesTo: boxElements, Doc: "Border thickness, per side. Drawn inside the declared rect."},
	PBorderColor:  {Name: "border-color", Kind: KindColor, Default: "gray(0.75)", AppliesTo: boxElements, Doc: "Border colour."},
	PBorderStyle:  {Name: "border-style", Kind: KindEnum, Default: "solid", AppliesTo: append(append([]string{}, boxElements...), ERule), Enum: []string{"solid", "dashed", "dotted", "none"}, Doc: "Border line style."},
	PBorderRadius: {Name: "border-radius", Kind: KindLength, Default: "0pt", AppliesTo: boxElements, Doc: "Corner radius. Describes the outer silhouette; the fill and the border share it."},
	PBorderCollapse: {Name: "border-collapse", Kind: KindBool, Inherited: true, Default: "true", AppliesTo: boxElements,
		Doc: "Draw a shared edge between two touching boxes once rather than twice. On by default; turn it off to get a deliberate double rule."},
	POpacity: {Name: "opacity", Kind: KindNumber, Default: "1", AppliesTo: boxElements, Doc: "Opacity from 0 to 1. Discouraged: transparency and print do not always agree."},

	// ---- Panel chrome ----
	PTitle:           {Name: "title", Kind: KindString, AppliesTo: []string{EPanel, EBox, EGrid}, Doc: "Panel title. Usually written as the panel's argument instead."},
	PTitlePosition:   {Name: "title-position", Kind: KindEnum, Default: "top", AppliesTo: []string{EPanel, EBox, EGrid}, Enum: []string{"top", "left", "bottom", "none"}, Doc: "Which edge the title sits on."},
	PTitleAlign:      {Name: "title-align", Kind: KindEnum, Default: "left", AppliesTo: []string{EPanel, EBox, EGrid}, Enum: []string{"left", "center", "centre", "right"}, Doc: "Alignment of the title along its edge."},
	PTitleFont:       {Name: "title-font", Kind: KindString, AppliesTo: []string{EPanel, EBox, EGrid}, Doc: "Title font family; defaults to the inherited font."},
	PTitleSize:       {Name: "title-size", Kind: KindLength, Default: "6.5pt", AppliesTo: []string{EPanel, EBox, EGrid}, Doc: "Title type size."},
	PTitleWeight:     {Name: "title-weight", Kind: KindEnum, Default: "bold", AppliesTo: []string{EPanel, EBox, EGrid}, Enum: []string{"regular", "normal", "bold"}, Doc: "Title weight."},
	PTitleColor:      {Name: "title-color", Kind: KindColor, Default: "gray(0.35)", AppliesTo: []string{EPanel, EBox, EGrid}, Doc: "Title colour."},
	PTitleBackground: {Name: "title-background", Kind: KindColor, Default: "transparent", AppliesTo: []string{EPanel, EBox, EGrid}, Doc: "Fill behind the title, for the bar style."},
	PTitlePadding:    {Name: "title-padding", Kind: KindEdges, Default: "2pt 0pt", AppliesTo: []string{EPanel, EBox, EGrid}, Doc: "Space around the title text."},
	PTitleStyle: {Name: "title-style", Kind: KindEnum, Default: "plain", AppliesTo: []string{EPanel, EBox, EGrid},
		Enum: []string{"plain", "bar", "notch", "underline"}, Doc: "How the title meets the border: floating, in a filled bar, interrupting the border, or over a rule."},
	PTitleTransform: {Name: "title-transform", Kind: KindEnum, Default: "upper", AppliesTo: []string{EPanel, EBox, EGrid}, Enum: []string{"none", "upper", "lower", "title"}, Doc: "Case transformation for the title."},
	PTitleTracking:  {Name: "title-tracking", Kind: KindLength, Default: "0.5pt", AppliesTo: []string{EPanel, EBox, EGrid}, Doc: "Letter-spacing for the title. Small caps want a little air."},

	// ---- Line decorations. Inherited, because a ruled line is ink too. ----
	PLineStyle: {Name: "line-style", Kind: KindEnum, Inherited: true, Default: "none", AppliesTo: lineHosts,
		Enum: []string{"none", "ruled", "dotted", "graph", "checkbox", "cornell", "time-grid"},
		Doc:  "What to draw inside the box: writing rules, a dot grid, graph squares, checkbox rows, a Cornell layout, or an hour grid."},
	PLinePitch: {Name: "line-pitch", Kind: KindLength, Inherited: true, Default: "6mm", AppliesTo: lineHosts, Doc: "Centre-to-centre spacing of whatever line-style draws. Distinct from line-height, which is text leading."},
	PLineColor: {Name: "line-color", Kind: KindColor, Inherited: true, Default: "gray(0.78)", AppliesTo: strokeHosts, Doc: "Colour of ruled lines, dots and rules."},
	PLineWidth: {Name: "line-width", Kind: KindLength, Inherited: true, Default: "0.4pt", AppliesTo: strokeHosts, Doc: "Thickness of ruled lines and rules. Below 0.25pt a printer decides for you."},
	PLineInset: {Name: "line-inset", Kind: KindEdges, Inherited: true, Default: "0pt", AppliesTo: lineHosts, Doc: "Inset of the ruled area from the content box."},
	PLineCount: {Name: "lines", Kind: KindInteger, Default: "0", AppliesTo: lineHosts, Doc: "Fix the number of rules instead of filling the height. 0 fills."},
	PLineDistribute: {Name: "line-distribute", Kind: KindEnum, Inherited: true, Default: "center", AppliesTo: lineHosts,
		Enum: []string{"center", "centre", "start", "end", "grow"},
		Doc:  "Where the leftover height goes when the pitch does not divide evenly. `grow` stretches the pitch so the last rule meets the bottom edge."},
	PLinePartial:    {Name: "line-partial", Kind: KindBool, Inherited: true, Default: "false", AppliesTo: lineHosts, Doc: "Allow a final stub line. Off by default: a truncated last line is the difference between designed and merely generated."},
	PBaselineOnRule: {Name: "baseline-on-rule", Kind: KindBool, Inherited: true, Default: "true", AppliesTo: lineHosts, Doc: "Whether text sits on the rule or above it."},
	PDotPitch:       {Name: "dot-pitch", Kind: KindLength, Inherited: true, Default: "0pt", AppliesTo: lineHosts, Doc: "Dot grid spacing. Defaults to line-pitch, so a square grid is one number."},
	PDotSize:        {Name: "dot-size", Kind: KindLength, Inherited: true, Default: "0.7pt", AppliesTo: lineHosts, Doc: "Dot diameter."},
	PGridMajor:      {Name: "grid-major", Kind: KindInteger, Inherited: true, Default: "5", AppliesTo: lineHosts, Doc: "Draw a heavier line every N squares in the graph style."},
	PGridMajorWidth: {Name: "grid-major-width", Kind: KindLength, Inherited: true, Default: "0.6pt", AppliesTo: lineHosts, Doc: "Thickness of the major grid lines."},
	PCheckboxSize:   {Name: "checkbox-size", Kind: KindLength, Inherited: true, Default: "0pt", AppliesTo: lineHosts, Doc: "Checkbox edge length. Defaults to 0.62 of line-pitch."},
	PCheckboxRadius: {Name: "checkbox-radius", Kind: KindLength, Inherited: true, Default: "0.5pt", AppliesTo: lineHosts, Doc: "Checkbox corner radius."},
	PCheckboxGutter: {Name: "checkbox-gutter", Kind: KindLength, Inherited: true, Default: "0pt", AppliesTo: lineHosts, Doc: "Space between the checkbox and the rule beside it. Defaults to 0.45 of line-pitch."},
	PCheckboxRule:   {Name: "checkbox-rule", Kind: KindBool, Inherited: true, Default: "true", AppliesTo: lineHosts, Doc: "Draw a writing rule beside each checkbox."},
	PGridOrigin: {Name: "grid-origin", Kind: KindEnum, Inherited: true, Default: "page", AppliesTo: lineHosts,
		Enum: []string{"page", "box"},
		Doc:  "Anchor dot and graph lattices to the page or to each box. Page by default, so dots line up across adjacent panels and the sheet reads as one piece of grid paper."},
	PMarginRule:       {Name: "margin-rule", Kind: KindBool, Inherited: true, Default: "false", AppliesTo: lineHosts, Doc: "Draw a vertical margin rule, the red line down the left of a school exercise book."},
	PMarginRuleOffset: {Name: "margin-rule-offset", Kind: KindLength, Inherited: true, Default: "28pt", AppliesTo: lineHosts, Doc: "Distance of the margin rule from the content's left edge."},
	PMarginRuleColor:  {Name: "margin-rule-color", Kind: KindColor, Inherited: true, Default: "#e05a55", AppliesTo: lineHosts, Doc: "Margin rule colour."},
	PTimeStart:        {Name: "time-start", Kind: KindString, Inherited: true, Default: "7:00", AppliesTo: lineHosts, Doc: "First hour of a time-grid."},
	PTimeEnd:          {Name: "time-end", Kind: KindString, Inherited: true, Default: "21:00", AppliesTo: lineHosts, Doc: "Last hour of a time-grid."},
	PTimeStep:         {Name: "time-step", Kind: KindInteger, Inherited: true, Default: "60", AppliesTo: lineHosts, Doc: "Minutes per labelled slot in a time-grid."},
	PTimeSubdivide:    {Name: "time-subdivide", Kind: KindInteger, Inherited: true, Default: "2", AppliesTo: lineHosts, Doc: "Unlabelled marks per slot. 2 gives half-hours."},
	PTimeGutter:       {Name: "time-gutter", Kind: KindLength, Inherited: true, Default: "34pt", AppliesTo: lineHosts, Doc: "Width of the hour-label column."},
	PCornellCue:       {Name: "cornell-cue", Kind: KindLength, Inherited: true, Default: "63pt", AppliesTo: lineHosts, Doc: "Width of the Cornell cue column."},
	PCornellSummary:   {Name: "cornell-summary", Kind: KindSize, Inherited: true, Default: "20%", AppliesTo: lineHosts, Doc: "Height of the Cornell summary band."},

	// ---- Page ----
	PPageSize:    {Name: "size", Kind: KindPageSize, Default: "letter", AppliesTo: []string{EPage}, Doc: "Named page size, or a width and height pair."},
	POrientation: {Name: "orientation", Kind: KindEnum, Default: "portrait", AppliesTo: []string{EPage}, Enum: []string{"portrait", "landscape"}, Doc: "Page orientation."},
	PWeekStart:   {Name: "week-start", Kind: KindEnum, Default: "monday", AppliesTo: []string{EPage}, Enum: []string{"monday", "sunday", "saturday"}, Doc: "Which day a week starts on. Does not affect the ISO week number."},
	PBleed:       {Name: "bleed", Kind: KindLength, Default: "0pt", AppliesTo: []string{EPage}, Doc: "Bleed area outside the trim size."},

	// ---- Structural ----
	PStyle: {Name: "style", Kind: KindStyleRef, Doc: "Apply one or more named style bundles, later ones winning."},
	PWhen:  {Name: "when", Kind: KindBool, Default: "true", Doc: "Drop this node and its children when false."},
	PID:    {Name: "id", Kind: KindString, Doc: "A name for this node, used in diagnostics and by --dump-layout."},
}

// byName indexes the table for lookup. Built once at init; never iterated,
// so it introduces no ordering into the output.
var byName = func() map[string]PropID {
	m := make(map[string]PropID, NumProps)
	for id := PropID(1); id < numProps; id++ {
		if props[id].Name != "" {
			m[props[id].Name] = id
		}
	}
	// Accepted spellings that normalise to a canonical property. `fmt` rewrites
	// these; keeping them out of the main table means they never appear in
	// suggestions or documentation as if they were separate properties.
	m["colour"] = PColor
	m["line-colour"] = PLineColor
	m["border-colour"] = PBorderColor
	m["title-colour"] = PTitleColor
	m["margin-rule-colour"] = PMarginRuleColor
	m["letter-spacing"] = PTracking
	m["page-size"] = PPageSize
	return m
}()

// enumAliases accepts the spellings people reach for that are far enough from
// the canonical word that the did-you-mean search will not find them.
//
// `dots` is three edits from `dotted`, which is past the suggestion threshold —
// so without this, a natural spelling gets a bare "not valid" and no help. The
// alternative to accepting it is being right and unhelpful at the same time.
// `treekillbot fmt` rewrites these to the canonical spelling.
var enumAliases = map[PropID]map[string]string{
	PLineStyle: {
		"dots":       "dotted",
		"dot":        "dotted",
		"dotgrid":    "dotted",
		"dot-grid":   "dotted",
		"lined":      "ruled",
		"lines":      "ruled",
		"rule":       "ruled",
		"squared":    "graph",
		"grid":       "graph",
		"squares":    "graph",
		"checkboxes": "checkbox",
		"todo":       "checkbox",
		"time":       "time-grid",
		"hours":      "time-grid",
		"blank":      "none",
	},
	PAlign:  {"start": "left", "end": "right", "middle": "center"},
	PValign: {"start": "top", "end": "bottom"},
	PBorderStyle: {
		"dot":  "dotted",
		"dots": "dotted",
		"dash": "dashed",
	},
	POrientation: {"portrait-mode": "portrait", "wide": "landscape", "tall": "portrait"},
}

// CanonicalEnum resolves an enum alias to the value the engine uses, reporting
// whether the input was an alias rather than the canonical spelling.
func CanonicalEnum(id PropID, value string) (string, bool) {
	aliases, ok := enumAliases[id]
	if !ok {
		return value, false
	}
	canonical, ok := aliases[value]
	if !ok {
		return value, false
	}
	return canonical, true
}

// sideOverrides maps a shorthand edge property to its four per-side
// refinements, in top/right/bottom/left order. Props.Edges consults it so the
// override logic lives in one place rather than at every call site.
var sideOverrides = map[PropID][4]PropID{
	PPadding:     {PPaddingTop, PPaddingRight, PPaddingBottom, PPaddingLeft},
	PMargin:      {PMarginTop, PMarginRight, PMarginBottom, PMarginLeft},
	PBorderWidth: {PBorderTopWidth, PBorderRightWidth, PBorderBottomWidth, PBorderLeftWidth},
}

// SideOverrides returns the per-side properties that refine a shorthand, and
// whether the shorthand has any.
func SideOverrides(id PropID) ([4]PropID, bool) {
	sides, ok := sideOverrides[id]
	return sides, ok
}

// Lookup resolves a property name, reporting whether it exists.
func Lookup(name string) (PropID, bool) {
	id, ok := byName[name]
	return id, ok
}

// Def returns a property's definition.
func Def(id PropID) *PropDef {
	if id == PInvalid || int(id) >= NumProps {
		return &PropDef{}
	}
	return &props[id]
}

// Name returns a property's canonical name.
func Name(id PropID) string { return Def(id).Name }

// AppliesTo reports whether a property is meaningful on an element.
func AppliesTo(id PropID, element string) bool {
	def := Def(id)
	if len(def.AppliesTo) == 0 {
		return true
	}
	for _, e := range def.AppliesTo {
		if e == element {
			return true
		}
	}
	return false
}

// AllPropIDs returns every property identifier in table order, so callers can
// walk the schema without knowing how it is stored.
func AllPropIDs() []PropID {
	out := make([]PropID, 0, NumProps)
	for id := PropID(1); id < numProps; id++ {
		if props[id].Name != "" {
			out = append(out, id)
		}
	}
	return out
}

// PropertyNames returns every canonical property name in table order, for
// suggestions and for `treekillbot docs props`. Table order is deterministic
// and groups related properties, which reads better than alphabetical.
func PropertyNames() []string {
	out := make([]string, 0, NumProps)
	for id := PropID(1); id < numProps; id++ {
		if props[id].Name != "" {
			out = append(out, props[id].Name)
		}
	}
	return out
}

// PropertyNamesFor returns the property names valid on one element.
func PropertyNamesFor(element string) []string {
	out := make([]string, 0, NumProps)
	for id := PropID(1); id < numProps; id++ {
		if props[id].Name != "" && AppliesTo(id, element) {
			out = append(out, props[id].Name)
		}
	}
	return out
}
