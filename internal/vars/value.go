// The variable value model: what a `{…}` can resolve to.
//
// This is deliberately a small closed set — string, number, bool, time, list,
// record — and not a general dynamic value. Pulp has no arithmetic and no
// expressions (DESIGN.md §6), so the only things a value must do are render
// itself, answer "is this true?", and hand back a named member. Anything richer
// would be the first half of an expression evaluator we have decided not to
// build.
//
// Fields are orthogonal to kind: a day item is a *time* that also carries
// `.name`, `.short`, `.dd` and friends, which is why `{day}`, `{day:%A}` and
// `{day.short}` all work on the same binding. Fields are held in a slice rather
// than a map because everything downstream of here is required to be
// deterministic (DESIGN.md §4) and because no record has enough members for the
// linear scan to matter.
package vars

import (
	"strconv"
	"strings"
	"time"
)

// Kind discriminates a variable value.
type Kind uint8

const (
	// KindInvalid is the zero Value: nothing resolved.
	KindInvalid Kind = iota
	KindString
	KindNumber
	KindBool
	// KindTime is a date, or a date and a time of day when HasClock is set.
	KindTime
	// KindList is what `for x in …` iterates.
	KindList
	// KindRecord is a namespace with members but no sensible scalar rendering,
	// such as `page` or `loop`.
	KindRecord
)

// String returns the kind's name as it appears in diagnostics.
func (k Kind) String() string {
	switch k {
	case KindString:
		return "text"
	case KindNumber:
		return "number"
	case KindBool:
		return "boolean"
	case KindTime:
		return "date"
	case KindList:
		return "list"
	case KindRecord:
		return "namespace"
	}
	return "nothing"
}

// field is one named member of a value.
type field struct {
	name string
	val  Value
}

// Value is one resolved variable.
type Value struct {
	Kind Kind

	Str  string
	Num  float64
	Bool bool
	Time time.Time
	// HasClock distinguishes `now` from `today`: both are KindTime, but only
	// one renders its time of day under the `iso` format.
	HasClock bool
	List     []Value

	fields []field
}

// NewString returns a text value.
func NewString(s string) Value { return Value{Kind: KindString, Str: s} }

// NewNumber returns a numeric value.
func NewNumber(n float64) Value { return Value{Kind: KindNumber, Num: n} }

// NewBool returns a boolean value.
func NewBool(b bool) Value { return Value{Kind: KindBool, Bool: b} }

// NewDate returns a date-only time value, which renders as `2026-08-24`.
func NewDate(t time.Time) Value { return Value{Kind: KindTime, Time: t} }

// NewDateTime returns a time value carrying a time of day, which renders as
// `2026-08-24T14:03:05`.
func NewDateTime(t time.Time) Value { return Value{Kind: KindTime, Time: t, HasClock: true} }

// NewList returns an iterable list value.
func NewList(items []Value) Value { return Value{Kind: KindList, List: items} }

// NewRecord returns a bare namespace with the given members. Order is the
// order given, and it is the order suggestions are ranked in.
func NewRecord(members ...Member) Value {
	return Value{Kind: KindRecord}.withMembers(members...)
}

// NewLoop returns the `loop` record bound inside a `for` or `repeat` body.
//
// It lives here rather than in the compiler so that the loop vocabulary —
// `index`, `n`, `first`, `last`, `count` — is defined once, next to everything
// else a document can name.
func NewLoop(index, count int) Value {
	return NewRecord(
		Member{"index", NewNumber(float64(index))},
		Member{"n", NewNumber(float64(index + 1))},
		Member{"first", NewBool(index == 0)},
		Member{"last", NewBool(index == count-1)},
		Member{"count", NewNumber(float64(count))},
	)
}

// Member is a named field, used when building a record.
type Member struct {
	Name  string
	Value Value
}

// WithMembers returns a copy of v carrying the given members, so a scalar can
// also be a namespace — this is what makes a day item both a date and a record.
func (v Value) WithMembers(members ...Member) Value { return v.withMembers(members...) }

func (v Value) withMembers(members ...Member) Value {
	v.fields = make([]field, 0, len(v.fields)+len(members))
	for _, m := range members {
		v.fields = append(v.fields, field{m.Name, m.Value})
	}
	return v
}

// Field returns a named member.
func (v Value) Field(name string) (Value, bool) {
	for _, f := range v.fields {
		if f.name == name {
			return f.val, true
		}
	}
	return Value{}, false
}

// FieldNames returns the member names in declaration order, for did-you-mean
// suggestions when a member is misspelled.
func (v Value) FieldNames() []string {
	if len(v.fields) == 0 {
		return nil
	}
	out := make([]string, 0, len(v.fields))
	for _, f := range v.fields {
		out = append(out, f.name)
	}
	return out
}

// String renders the value the way `{name}` with no format spec renders it.
//
// Times render as `iso` because that is the one format that is unambiguous on
// paper anywhere in the world; every other default would be a locale decision,
// and locale is out of scope (DESIGN.md D12).
func (v Value) String() string {
	switch v.Kind {
	case KindString:
		return v.Str
	case KindNumber:
		return formatNumber(v.Num)
	case KindBool:
		if v.Bool {
			return "true"
		}
		return "false"
	case KindTime:
		if v.HasClock {
			return v.Time.Format("2006-01-02T15:04:05")
		}
		return v.Time.Format("2006-01-02")
	case KindList:
		parts := make([]string, 0, len(v.List))
		for _, item := range v.List {
			parts = append(parts, item.String())
		}
		return strings.Join(parts, ", ")
	case KindRecord:
		return v.Str
	}
	return ""
}

// Truthy reports whether the value counts as true in a `when:` or a ternary.
//
// The rules are the ones a form author would guess: empty is false, zero is
// false, a bound namespace is true. The words "false", "no" and "off" are
// false as text as well, because a `--var draft=false` that rendered the draft
// watermark anyway would be a genuinely confusing bug.
func (v Value) Truthy() bool {
	switch v.Kind {
	case KindString:
		switch strings.ToLower(strings.TrimSpace(v.Str)) {
		case "", "false", "no", "off", "0":
			return false
		}
		return true
	case KindNumber:
		return v.Num != 0
	case KindBool:
		return v.Bool
	case KindTime:
		return !v.Time.IsZero()
	case KindList:
		return len(v.List) > 0
	case KindRecord:
		return true
	}
	return false
}

// formatNumber renders a float without a trailing `.0`, so `{week.number}` is
// "35" rather than "35.0" — every number a document can name is conceptually an
// integer, but they are carried as float64 so that a `--var ratio=0.5` needs no
// second numeric type.
func formatNumber(n float64) string {
	return strconv.FormatFloat(n, 'f', -1, 64)
}
