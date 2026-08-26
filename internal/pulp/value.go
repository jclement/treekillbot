// Typed values inside a node's argument.
//
// A node's argument arrives here as raw text plus the span it came from. This
// file turns it into a list of typed Values, each carrying its own span, which
// is what lets an error put a caret under `200` rather than under the whole
// line.
//
// Values are parsed AFTER variable interpolation, so by the time this code
// runs, `{accent}` has already become `#1f6feb`. The one exception is `fmt` and
// `check --no-vars`, which parse without substituting; an un-substituted
// `{...}` becomes KindInterp and is passed through untouched rather than
// treated as a syntax error.
package pulp

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/paint"
)

// ValueKind discriminates a parsed value.
type ValueKind uint8

const (
	KindInvalid  ValueKind = iota
	KindNumber             // 2, 1.35 — unitless
	KindLength             // 16pt, 0.5in, 1.2em
	KindPercent            // 30%
	KindFill               // fill, fill(2)
	KindAuto               // auto
	KindNone               // none
	KindColor              // #ddd, gray(0.85), rgb(...), slategray
	KindString             // "quoted" or a bare word
	KindBool               // true/false/yes/no/on/off
	KindKeyword            // a bare word to be validated against a schema enum
	KindFunction           // ident(args...) that is not a colour function
	KindInterp             // an un-substituted {…}
)

// String returns the kind's name as it appears in diagnostics.
func (k ValueKind) String() string {
	switch k {
	case KindNumber:
		return "number"
	case KindLength:
		return "length"
	case KindPercent:
		return "percentage"
	case KindFill:
		return "fill"
	case KindAuto:
		return "auto"
	case KindNone:
		return "none"
	case KindColor:
		return "colour"
	case KindString:
		return "string"
	case KindBool:
		return "boolean"
	case KindKeyword:
		return "keyword"
	case KindFunction:
		return "function"
	case KindInterp:
		return "interpolation"
	}
	return "invalid"
}

// Value is one parsed token from an argument.
type Value struct {
	Kind ValueKind
	Span Span
	Raw  string

	Num  float64 // KindNumber, and the magnitude of KindLength/KindPercent
	Unit string  // KindLength: pt, px, mm, cm, in, pc, em, ex

	// Length is the resolved length for absolute units. Relative units (em, ex)
	// cannot be resolved until the cascade knows the font size, so they leave
	// Length zero and set Relative.
	Length   geom.Tick
	Relative bool

	Pct    int32 // KindPercent, in basis points: 3000 is 30%
	Weight int32 // KindFill, in sixteenths: 16 is `fill`, 32 is `fill(2)`

	Color paint.Color
	Str   string
	Bool  bool
	Args  []Value // KindFunction
}

// IsSize reports whether the value can appear where a width or height is
// expected.
func (v Value) IsSize() bool {
	switch v.Kind {
	case KindLength, KindPercent, KindFill, KindAuto:
		return true
	}
	return false
}

// ParseValues splits an argument into whitespace- or comma-separated values.
//
// Commas are optional and ignored, matching the CSS shorthands people expect
// (`margin: 1in 0.5in` and `margin: 1in, 0.5in` are the same). Quotes, function
// parentheses and interpolation braces all suppress splitting, so
// `rgb(1, 2, 3)` and `"a b c"` each stay one value.
func ParseValues(src *Source, span Span, text string, diags *Diagnostics) []Value {
	var out []Value
	for _, tok := range splitTokens(text) {
		v := parseValue(src, span.Sub(tok.start, tok.end), tok.text, diags)
		if v.Kind != KindInvalid {
			out = append(out, v)
		}
	}
	return out
}

// ParseValue parses an argument expected to hold exactly one value, reporting
// an error when it holds more.
func ParseValue(src *Source, span Span, text string, diags *Diagnostics) (Value, bool) {
	vs := ParseValues(src, span, text, diags)
	switch len(vs) {
	case 0:
		return Value{}, false
	case 1:
		return vs[0], true
	}
	diags.Errorf(src, vs[1].Span.Join(vs[len(vs)-1].Span), "E030",
		"expected one value, found %d", len(vs)).
		WithLabel("unexpected extra value").
		WithHelp("Did you mean to quote it? Write `\"%s\"` to keep it as one string.", text)
	return vs[0], true
}

type token struct {
	text       string
	start, end int
}

// splitTokens breaks argument text into value tokens, tracking nesting so that
// separators inside quotes, parentheses or braces do not split.
func splitTokens(s string) []token {
	var (
		out   []token
		depth int
		quote byte
		start = -1
	)
	flush := func(end int) {
		if start >= 0 {
			out = append(out, token{text: s[start:end], start: start, end: end})
			start = -1
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == '\\' && quote == '"' && i+1 < len(s) {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			if start < 0 {
				start = i
			}
			quote = c
		case '(', '{', '[':
			if start < 0 {
				start = i
			}
			depth++
		case ')', '}', ']':
			if depth > 0 {
				depth--
			}
		case ' ', '\t', ',':
			if depth == 0 {
				flush(i)
				continue
			}
		default:
			if start < 0 {
				start = i
			}
		}
		if start < 0 {
			start = i
		}
	}
	flush(len(s))
	return out
}

// parseValue classifies and parses a single token.
func parseValue(src *Source, span Span, text string, diags *Diagnostics) Value {
	v := Value{Span: span, Raw: text}
	if text == "" {
		return v
	}

	switch {
	case text[0] == '{':
		v.Kind, v.Str = KindInterp, text
		return v
	case text[0] == '"' || text[0] == '\'':
		return parseStringValue(src, span, text, diags)
	case text[0] == '#':
		return parseHexColor(src, span, text, diags)
	case text[0] == '-' || text[0] == '.' || (text[0] >= '0' && text[0] <= '9'):
		return parseNumeric(src, span, text, diags)
	}

	// A bare word: a keyword, a boolean, a named colour, or a function call.
	if i := strings.IndexByte(text, '('); i > 0 && strings.HasSuffix(text, ")") {
		return parseFunction(src, span, text[:i], text[i+1:len(text)-1], span.Sub(i+1, len(text)-1), diags)
	}
	lower := strings.ToLower(text)
	switch lower {
	case "auto":
		v.Kind = KindAuto
		return v
	case "none":
		v.Kind = KindNone
		return v
	case "fill":
		v.Kind, v.Weight = KindFill, 16
		return v
	case "true", "yes", "on":
		v.Kind, v.Bool = KindBool, true
		return v
	case "false", "no", "off":
		v.Kind, v.Bool = KindBool, false
		return v
	}
	if c, ok := NamedColor(lower); ok {
		v.Kind, v.Color = KindColor, c
		return v
	}
	v.Kind, v.Str = KindKeyword, text
	return v
}

func parseStringValue(src *Source, span Span, text string, diags *Diagnostics) Value {
	v := Value{Kind: KindString, Span: span, Raw: text}
	quote := text[0]
	if len(text) < 2 || text[len(text)-1] != quote {
		diags.Errorf(src, span, "E031", "unterminated string").
			WithLabel("no closing %c", quote).
			WithHelp("Add a closing %c at the end of the value.", quote)
		v.Str = text[1:]
		return v
	}
	body := text[1 : len(text)-1]
	if quote == '\'' {
		// Single quotes are fully raw: no escapes, no interpolation. This is
		// the escape hatch for text that is mostly braces.
		v.Str = body
		return v
	}
	v.Str = unescape(body)
	return v
}

// unescape resolves the backslash escapes legal inside a double-quoted string.
func unescape(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		default:
			// Covers \" \\ \{ \} \# and leaves anything else as written, which
			// is friendlier than erroring on a stray backslash in prose.
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// parseNumeric handles numbers, lengths and percentages — everything that
// starts with a digit, a sign or a decimal point.
func parseNumeric(src *Source, span Span, text string, diags *Diagnostics) Value {
	v := Value{Span: span, Raw: text}

	end := 0
	for end < len(text) && (text[end] == '-' || text[end] == '+' || text[end] == '.' ||
		(text[end] >= '0' && text[end] <= '9')) {
		end++
	}
	num, err := strconv.ParseFloat(text[:end], 64)
	if err != nil {
		diags.Errorf(src, span, "E032", "%q is not a number", text).
			WithLabel("not a number")
		return v
	}
	v.Num = num
	suffix := text[end:]

	switch suffix {
	case "":
		v.Kind = KindNumber
		return v
	case "%":
		v.Kind = KindPercent
		v.Pct = int32(num*100 + 0.5)
		if num < 0 {
			v.Pct = int32(num*100 - 0.5)
		}
		return v
	}

	if ticks, relative, ok := convertUnit(num, suffix); ok {
		v.Kind, v.Unit, v.Length, v.Relative = KindLength, strings.ToLower(suffix), ticks, relative
		return v
	}

	diags.Errorf(src, span, "E033", "unknown unit %q", suffix).
		WithLabel("unknown unit").
		WithHelp("Lengths use pt, in, mm, cm, pc, px or em. Try %g%s.", num, "pt")
	return v
}

// convertUnit turns a magnitude and a unit suffix into ticks. Relative units
// report themselves as such and leave the length unresolved, because em and ex
// depend on a font size that the cascade has not chosen yet.
func convertUnit(num float64, suffix string) (ticks geom.Tick, relative, ok bool) {
	switch strings.ToLower(suffix) {
	case "pt":
		return geom.Pt(num), false, true
	case "in":
		return geom.In(num), false, true
	case "mm":
		return geom.Mm(num), false, true
	case "cm":
		return geom.Cm(num), false, true
	case "pc":
		return geom.Pc(num), false, true
	case "px":
		return geom.Px(num), false, true
	case "em", "ex":
		return 0, true, true
	}
	return 0, false, false
}

// parseHexColor handles #rgb, #rgba, #rrggbb and #rrggbbaa.
func parseHexColor(src *Source, span Span, text string, diags *Diagnostics) Value {
	v := Value{Span: span, Raw: text}
	digits := text[1:]
	expand := func(i int) uint8 {
		c := digits[i]
		hi := hexVal(c)
		return hi<<4 | hi
	}
	pair := func(i int) uint8 { return hexVal(digits[i])<<4 | hexVal(digits[i+1]) }

	for i := 0; i < len(digits); i++ {
		if !isHexDigit(digits[i]) {
			diags.Errorf(src, span, "E034", "%q is not a colour", text).
				WithLabel("bad hex digit %q", digits[i]).
				WithHelp("A hex colour is # followed by 3, 4, 6 or 8 hex digits, as in #ddd or #1f6feb.")
			return v
		}
	}

	var r, g, b, a uint8 = 0, 0, 0, 255
	switch len(digits) {
	case 3:
		r, g, b = expand(0), expand(1), expand(2)
	case 4:
		r, g, b, a = expand(0), expand(1), expand(2), expand(3)
	case 6:
		r, g, b = pair(0), pair(2), pair(4)
	case 8:
		r, g, b, a = pair(0), pair(2), pair(4), pair(6)
	default:
		diags.Errorf(src, span, "E034", "%q has %d hex digits", text, len(digits)).
			WithLabel("wrong length").
			WithHelp("A hex colour needs 3, 4, 6 or 8 digits, as in #ddd or #dddddd.")
		return v
	}
	v.Kind = KindColor
	v.Color = paint.RGB8(r, g, b).WithAlpha(float64(a) / 255)
	return v
}

func hexVal(c byte) uint8 {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	}
	return 0
}

// parseFunction handles the parenthesised forms: the colour constructors, and
// fill(n).
func parseFunction(src *Source, span Span, name, argText string, argSpan Span, diags *Diagnostics) Value {
	v := Value{Span: span, Raw: name + "(" + argText + ")"}
	args := ParseValues(src, argSpan, argText, diags)
	lower := strings.ToLower(name)

	num := func(i int) float64 {
		if i < len(args) {
			if args[i].Kind == KindPercent {
				return float64(args[i].Pct) / 10000
			}
			return args[i].Num
		}
		return 0
	}
	wantArgs := func(min, max int) bool {
		if len(args) >= min && len(args) <= max {
			return true
		}
		diags.Errorf(src, span, "E035", "%s() takes %s arguments, got %d", lower, argRange(min, max), len(args)).
			WithLabel("wrong number of arguments")
		return false
	}

	switch lower {
	case "fill":
		if !wantArgs(1, 1) {
			return v
		}
		weight := num(0)
		if weight <= 0 {
			diags.Errorf(src, span, "E036", "fill weight must be positive, got %g", weight).
				WithLabel("not a positive weight").
				WithHelp("`fill` takes a share of the leftover space; `fill(2)` takes twice as much as `fill`.")
			return v
		}
		v.Kind, v.Weight = KindFill, int32(weight*16+0.5)
		return v

	case "gray", "grey":
		if !wantArgs(1, 2) {
			return v
		}
		c := paint.GrayN(num(0))
		if len(args) == 2 {
			c = c.WithAlpha(num(1))
		}
		v.Kind, v.Color = KindColor, c
		return v

	case "rgb", "rgba":
		if !wantArgs(3, 4) {
			return v
		}
		c := paint.RGB(channel(args[0]), channel(args[1]), channel(args[2]))
		if len(args) == 4 {
			c = c.WithAlpha(num(3))
		}
		v.Kind, v.Color = KindColor, c
		return v

	case "cmyk":
		if !wantArgs(4, 4) {
			return v
		}
		v.Kind, v.Color = KindColor, paint.CMYK(num(0), num(1), num(2), num(3))
		return v
	}

	v.Kind, v.Str, v.Args = KindFunction, lower, args
	return v
}

// channel reads an rgb() component, accepting both the 0-255 form people write
// from a colour picker and the 0-1 / percentage forms CSS Color 4 allows.
func channel(v Value) float64 {
	if v.Kind == KindPercent {
		return float64(v.Pct) / 10000
	}
	if v.Num > 1 {
		return v.Num / 255
	}
	return v.Num
}

func argRange(min, max int) string {
	if min == max {
		return strconv.Itoa(min)
	}
	return fmt.Sprintf("%d to %d", min, max)
}
