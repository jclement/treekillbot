// Format specs: what the `:` in `{today:%A}` means.
//
// Three vocabularies, chosen per DESIGN.md D12:
//
//   - **Named formats** are the default. `{today:long}` reads better than any
//     layout string and covers the cases a planner actually needs.
//   - **strftime** is the escape hatch. Every directive starts with `%`, so
//     literal text is always literal — which is the whole reason Go's reference
//     layout was rejected as the primary language, since `fmt(d, "Q1 2006
//     review")` silently renders "Q1 2026 review".
//   - **Go layouts** remain available, but only when explicitly tagged
//     `go:"Mon Jan 2"`, where the author has opted into the grenade.
//
// | name   | equivalent            | example                     |
// |--------|-----------------------|-----------------------------|
// | `iso`  | `%Y-%m-%d`            | 2026-08-24                  |
// | `short`| `%b %-d`              | Aug 24                      |
// | `long` | `%A, %B %-d, %Y`      | Monday, August 24, 2026     |
// | `date` | `%B %-d, %Y`          | August 24, 2026             |
// | `time` | `%-I:%M %p`           | 2:03 PM                     |
//
// Numbers take printf verbs (`%02d`) and strings take a tiny closed set
// (`upper`, `lower`, `title`, `trunc N`, `pad N`). An unrecognised spec is an
// error rather than a passthrough: a `{today:%Q}` that printed "%Q" onto a
// printed form would be discovered by the person holding the paper.
package vars

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// namedTimeFormats is the default date vocabulary. `iso` is handled separately
// because it is the only one that changes with the value's granularity.
var namedTimeFormats = map[string]string{
	"short": "%b %-d",
	"long":  "%A, %B %-d, %Y",
	"date":  "%B %-d, %Y",
	"time":  "%-I:%M %p",
}

// timeFormatNames lists the date vocabulary for did-you-mean suggestions, in a
// fixed order because map iteration is not one.
var timeFormatNames = []string{"iso", "short", "long", "date", "time"}

// stringFormatNames lists the string vocabulary, same reason.
var stringFormatNames = []string{"upper", "lower", "title", "trunc", "pad"}

// unknownFormatError is a spec the value's type does not understand. It carries
// the vocabulary that *would* have applied so the diagnostic can offer a
// did-you-mean rather than just refusing.
type unknownFormatError struct {
	Spec  string
	Kind  Kind
	Known []string
}

func (e *unknownFormatError) Error() string {
	return fmt.Sprintf("unknown format %q for a %s value", e.Spec, e.Kind)
}

// formatValue renders v according to a format spec, which is the text after
// the `:` in an interpolation. An empty spec means the value's default
// rendering. Scope.Format is the exported way in, since the spec is
// interpreted against the scope's month and day names.
func formatValue(v Value, spec string, nt *nameTable) (string, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return v.String(), nil
	}

	if layout, ok := goLayout(spec); ok {
		if v.Kind != KindTime {
			return "", fmt.Errorf(`go:"…" layouts apply to dates, and this value is %s`, article(v.Kind))
		}
		return v.Time.Format(layout), nil
	}

	if v.Kind == KindTime {
		if out, handled, err := formatTime(v, spec, nt); handled {
			return out, err
		}
	}

	if strings.HasPrefix(spec, "%") {
		return formatPrintf(v, spec)
	}

	return formatString(v, spec)
}

// goLayout recognises the `go:"…"` escape hatch and returns the layout inside
// the quotes.
func goLayout(spec string) (string, bool) {
	const prefix = `go:"`
	if !strings.HasPrefix(spec, prefix) || !strings.HasSuffix(spec, `"`) || len(spec) < len(prefix)+1 {
		return "", false
	}
	return spec[len(prefix) : len(spec)-1], true
}

// formatTime applies the date vocabularies. The handled return distinguishes
// "this spec is not a date format" — in which case the caller falls through to
// the string vocabulary, so `{today:upper}` works on the ISO rendering — from
// "it is, and it failed".
func formatTime(v Value, spec string, nt *nameTable) (out string, handled bool, err error) {
	if spec == "iso" {
		return v.String(), true, nil
	}
	if layout, ok := namedTimeFormats[spec]; ok {
		out, err := strftime(v.Time, layout, nt)
		return out, true, err
	}
	if strings.ContainsRune(spec, '%') {
		out, err := strftime(v.Time, spec, nt)
		return out, true, err
	}
	return "", false, nil
}

// formatPrintf applies a printf verb, which is how numbers are formatted
// (`{week.number:%02d}`). The verb decides which of the value's fields is
// passed, so `%d` on a number does not print the float64 bit pattern.
func formatPrintf(v Value, spec string) (string, error) {
	verb, ok := printfVerb(spec)
	if !ok {
		return "", fmt.Errorf("%q is not a complete printf verb", spec)
	}
	switch verb {
	case 'd', 'b', 'o', 'x', 'X', 'c', 'U':
		if v.Kind != KindNumber {
			return "", fmt.Errorf("%%%c formats a number, and this value is %s", verb, article(v.Kind))
		}
		return fmt.Sprintf(spec, int64(v.Num)), nil
	case 'e', 'E', 'f', 'F', 'g', 'G':
		if v.Kind != KindNumber {
			return "", fmt.Errorf("%%%c formats a number, and this value is %s", verb, article(v.Kind))
		}
		return fmt.Sprintf(spec, v.Num), nil
	case 's', 'q':
		return fmt.Sprintf(spec, v.String()), nil
	case 't':
		if v.Kind != KindBool {
			return "", fmt.Errorf("%%t formats a boolean, and this value is %s", article(v.Kind))
		}
		return fmt.Sprintf(spec, v.Bool), nil
	}
	return "", fmt.Errorf("%%%c is not a format verb treekillbot understands", verb)
}

// printfVerb returns the trailing verb letter of a spec like `%02d`, rejecting
// anything with a second `%` — a format spec formats exactly one value.
func printfVerb(spec string) (byte, bool) {
	if len(spec) < 2 || spec[0] != '%' || strings.Count(spec, "%") != 1 {
		return 0, false
	}
	last := spec[len(spec)-1]
	if last >= 'a' && last <= 'z' || last >= 'A' && last <= 'Z' {
		return last, true
	}
	return 0, false
}

// formatString applies the closed set of string operations, on the value's
// default rendering.
func formatString(v Value, spec string) (string, error) {
	text := v.String()
	op, arg, _ := strings.Cut(spec, " ")
	switch op {
	case "upper":
		return strings.ToUpper(text), nil
	case "lower":
		return strings.ToLower(text), nil
	case "title":
		return titleCase(text), nil
	case "trunc":
		n, err := formatArg(op, arg)
		if err != nil {
			return "", err
		}
		return truncate(text, n), nil
	case "pad":
		n, err := formatArg(op, arg)
		if err != nil {
			return "", err
		}
		return pad(text, n), nil
	}

	known := stringFormatNames
	if v.Kind == KindTime {
		known = append(append([]string{}, timeFormatNames...), stringFormatNames...)
	}
	return "", &unknownFormatError{Spec: spec, Kind: v.Kind, Known: known}
}

// formatArg parses the count on `trunc N` / `pad N`.
func formatArg(op, arg string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(arg))
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s needs a width, as in `%s 12`", op, op)
	}
	return n, nil
}

// truncate cuts text to n runes.
//
// No ellipsis is appended: these formats exist to fit text into a measured cell
// on a form, and an ellipsis would silently spend one of the n characters the
// author counted out.
func truncate(text string, n int) string {
	if utf8.RuneCountInString(text) <= n {
		return text
	}
	count := 0
	for i := range text {
		if count == n {
			return text[:i]
		}
		count++
	}
	return text
}

// pad right-pads text with spaces to n runes, left-aligning it. Left alignment
// is the useful direction here because pad's job is lining up a column of
// labels in a monospace grid; numbers that want leading zeros use `%02d`.
func pad(text string, n int) string {
	missing := n - utf8.RuneCountInString(text)
	if missing <= 0 {
		return text
	}
	return text + strings.Repeat(" ", missing)
}

// titleCase capitalises the first letter of each whitespace-separated word and
// lowercases the rest, which is what makes `{owner:title}` fix a name typed in
// caps rather than leaving it in caps.
func titleCase(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	startOfWord := true
	for _, r := range text {
		switch {
		case unicode.IsSpace(r):
			startOfWord = true
			b.WriteRune(r)
		case startOfWord:
			startOfWord = false
			b.WriteRune(unicode.ToTitle(r))
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// article renders a kind for use mid-sentence in an error message.
func article(k Kind) string {
	if k == KindInvalid {
		return "unset"
	}
	return "a " + k.String()
}

// ---- strftime ----

// strftime renders t according to a strftime(3) layout.
//
// The supported directive set is the one a paper form needs, plus the ISO
// trio (%V, %G, %u) that Go's own layouts cannot express at all. A `-` flag
// suppresses zero padding (`%-d` → "4"), which is the difference between
// "August 4, 2026" and "August 04, 2026" and therefore not optional.
//
// An unknown directive is an error. Emitting it raw would put "%Q" on a printed
// page, where the feedback loop is a person holding the paper.
func strftime(t time.Time, layout string, nt *nameTable) (string, error) {
	var b strings.Builder
	b.Grow(len(layout) + 16)

	for i := 0; i < len(layout); i++ {
		if layout[i] != '%' {
			b.WriteByte(layout[i])
			continue
		}
		i++
		if i >= len(layout) {
			return "", fmt.Errorf("format ends with a lone %%; write %%%% for a literal percent sign")
		}
		noPad := false
		if layout[i] == '-' {
			noPad = true
			i++
			if i >= len(layout) {
				return "", fmt.Errorf("format ends with a lone %%-; write %%%% for a literal percent sign")
			}
		}
		out, err := directive(t, layout[i], noPad, nt)
		if err != nil {
			return "", err
		}
		b.WriteString(out)
	}
	return b.String(), nil
}

// directive renders one strftime directive.
func directive(t time.Time, c byte, noPad bool, nt *nameTable) (string, error) {
	isoYear, isoWeek := t.ISOWeek()
	switch c {
	case 'Y':
		return num(t.Year(), 4, noPad), nil
	case 'y':
		return num(t.Year()%100, 2, noPad), nil
	case 'm':
		return num(int(t.Month()), 2, noPad), nil
	case 'd':
		return num(t.Day(), 2, noPad), nil
	case 'e':
		if noPad {
			return strconv.Itoa(t.Day()), nil
		}
		return fmt.Sprintf("%2d", t.Day()), nil
	case 'H':
		return num(t.Hour(), 2, noPad), nil
	case 'I':
		return num(hour12(t), 2, noPad), nil
	case 'M':
		return num(t.Minute(), 2, noPad), nil
	case 'S':
		return num(t.Second(), 2, noPad), nil
	case 'p':
		if t.Hour() < 12 {
			return "AM", nil
		}
		return "PM", nil
	case 'A':
		return nt.dayName(t.Weekday()), nil
	case 'a':
		return nt.shortDayName(t.Weekday()), nil
	case 'B':
		return nt.monthName(t.Month()), nil
	case 'b':
		return nt.shortMonthName(t.Month()), nil
	case 'j':
		return num(t.YearDay(), 3, noPad), nil
	case 'U':
		return num(weekOfYear(t, time.Sunday), 2, noPad), nil
	case 'W':
		return num(weekOfYear(t, time.Monday), 2, noPad), nil
	case 'V':
		return num(isoWeek, 2, noPad), nil
	case 'G':
		return num(isoYear, 4, noPad), nil
	case 'u':
		// ISO weekday, Monday=1 through Sunday=7.
		wd := int(t.Weekday())
		if wd == 0 {
			wd = 7
		}
		return strconv.Itoa(wd), nil
	case 'w':
		return strconv.Itoa(int(t.Weekday())), nil
	case 'F':
		return t.Format("2006-01-02"), nil
	case 'T':
		return t.Format("15:04:05"), nil
	case 'z':
		return t.Format("-0700"), nil
	case 'Z':
		return t.Format("MST"), nil
	case 'n':
		return "\n", nil
	case 't':
		return "\t", nil
	case '%':
		return "%", nil
	}
	return "", fmt.Errorf("unknown date directive %%%c; write %%%% for a literal percent sign", c)
}

// num renders an integer zero-padded to width, or bare when the `-` flag was
// given.
func num(n, width int, noPad bool) string {
	if noPad {
		return strconv.Itoa(n)
	}
	return fmt.Sprintf("%0*d", width, n)
}

func hour12(t time.Time) int {
	h := t.Hour() % 12
	if h == 0 {
		return 12
	}
	return h
}

// weekOfYear implements %U and %W: the count of complete weeks since the first
// `first` weekday of the year, so days before it are week 00. This is not the
// ISO week (that is %V), and the difference is exactly the bug DESIGN.md D12
// exists to prevent.
func weekOfYear(t time.Time, first time.Weekday) int {
	offset := (int(t.Weekday()) - int(first) + 7) % 7
	return (t.YearDay() + 6 - offset) / 7
}
