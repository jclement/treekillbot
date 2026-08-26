// The date built-ins: everything a document can name that is derived from the
// anchor.
//
// One anchor time seeds the lot, and `--date` reseeds it wholesale (DESIGN.md
// D12). Nothing in this package calls time.Now() — the anchor is passed into
// NewScope — which is what makes `--deterministic` a property of the caller
// rather than a flag this code has to remember to honour.
//
// Two correctness rules that are the reason half of this file exists:
//
//  1. **The ISO week and the calendar year are different numbers.** 2027-01-01
//     falls in ISO week 53 of 2026, so a header built from `week.number` and
//     `today`'s year reads "Week 53, 2027" and is wrong. `week.number` is
//     always paired with `week.iso-year`, and both are exposed so a document
//     can be right.
//  2. **`week-start` rotates the displayed week; it never moves the ISO week
//     number.** ISO 8601 defines weeks as Monday-based, full stop, so the ISO
//     fields are computed from the anchor day itself and are identical under
//     `--week-start sunday`. Only `week.start`, `week.end` and the day lists
//     rotate.
//
// The weekend is Saturday and Sunday regardless of `week-start`, because
// `week-start` is about how a spread is laid out, not about which days people
// are off.
package vars

import (
	"fmt"
	"time"
)

// nameTable holds the month and day names. It exists as an indirection purely
// so `month-names:` / `day-names:` can override English without a code change
// (DESIGN.md D12: locale is out of scope, the override hook is not).
type nameTable struct {
	months      []string // index 0 is January
	shortMonths []string
	days        []string // index 0 is Sunday, matching time.Weekday
	shortDays   []string
}

func defaultNames() *nameTable {
	return &nameTable{
		months: []string{"January", "February", "March", "April", "May", "June",
			"July", "August", "September", "October", "November", "December"},
		shortMonths: []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun",
			"Jul", "Aug", "Sep", "Oct", "Nov", "Dec"},
		days:      []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"},
		shortDays: []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"},
	}
}

func (nt *nameTable) monthName(m time.Month) string      { return nt.months[int(m)-1] }
func (nt *nameTable) shortMonthName(m time.Month) string { return nt.shortMonths[int(m)-1] }
func (nt *nameTable) dayName(w time.Weekday) string      { return nt.days[int(w)] }
func (nt *nameTable) shortDayName(w time.Weekday) string { return nt.shortDays[int(w)] }

// shorten derives short names by taking the first three runes, so a document
// that overrides `month-names:` alone still gets sensible `%b` output.
func shorten(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = truncate(n, 3)
	}
	return out
}

// ---- the built-in namespace ----

// buildBuiltins derives every date namespace from the anchor. It returns a
// record so that name lookup and did-you-mean suggestions over the built-ins
// use the same machinery as any other value.
func buildBuiltins(anchor time.Time, weekStart time.Weekday, nt *nameTable, doc DocInfo) Value {
	today := dateOf(anchor)
	return NewRecord(
		Member{"today", dayItem(today, today, nt)},
		Member{"now", NewDateTime(anchor)},
		Member{"tomorrow", dayItem(today.AddDate(0, 0, 1), today, nt)},
		Member{"yesterday", dayItem(today.AddDate(0, 0, -1), today, nt)},
		Member{"week", weekValue(startOfWeek(today, weekStart), today, today, nt)},
		Member{"month", monthValue(today, weekStart, nt)},
		Member{"quarter", quarterValue(today, nt)},
		Member{"year", yearValue(today, nt)},
		Member{"day-of-year", NewNumber(float64(today.YearDay()))},
		Member{"doc", NewRecord(
			Member{"name", NewString(doc.Name)},
			Member{"path", NewString(doc.Path)},
		)},
	)
}

// dayItem builds what `for day in week.days` binds — and also what every
// boundary date (`week.start`, `month.end`, …) is, so `{month.end.dd}` and
// `{today.dd}` are the same thing seen in two places.
//
// It is a date that also carries members, so `{day}`, `{day:%A}` and
// `{day.short}` are all the same binding seen three ways. `anchorDay` is what
// `.today` compares against.
func dayItem(day, anchorDay time.Time, nt *nameTable) Value {
	return NewDate(day).WithMembers(
		Member{"date", NewDate(day)},
		Member{"name", NewString(nt.dayName(day.Weekday()))},
		Member{"short", NewString(nt.shortDayName(day.Weekday()))},
		Member{"dd", NewString(fmt.Sprintf("%02d", day.Day()))},
		Member{"num", NewNumber(float64(day.Day()))},
		Member{"iso", NewString(day.Format("2006-01-02"))},
		Member{"weekend", NewBool(isWeekend(day))},
		Member{"today", NewBool(day.Equal(anchorDay))},
		Member{"month", monthLabel(day, nt)},
	)
}

// monthLabel is the lightweight month a day item carries.
//
// It deliberately has no `.days` or `.weeks`: a day item's month containing the
// full list of days, each of which carries its month, is an infinite structure.
// `{day.month}` is the full name, `{day.month.short}` the abbreviation.
func monthLabel(t time.Time, nt *nameTable) Value {
	return NewString(nt.monthName(t.Month())).WithMembers(
		Member{"name", NewString(nt.monthName(t.Month()))},
		Member{"short", NewString(nt.shortMonthName(t.Month()))},
		Member{"number", NewNumber(float64(t.Month()))},
	)
}

// weekValue builds a week namespace for the seven days starting at start.
//
// isoRef is the day whose ISO week the week reports. For the document's own
// `week` that is the anchor day, which is what keeps `week.number` stable under
// `--week-start sunday`; for a row of `month.weeks`, which has no anchor day,
// the caller passes the week's midweek day so the row reports the ISO week
// containing most of it.
func weekValue(start, isoRef, anchorDay time.Time, nt *nameTable) Value {
	isoYear, isoWeek := isoRef.ISOWeek()
	end := start.AddDate(0, 0, 6)

	days := make([]Value, 0, 7)
	weekdays := make([]Value, 0, 5)
	weekend := make([]Value, 0, 2)
	for i := 0; i < 7; i++ {
		day := start.AddDate(0, 0, i)
		item := dayItem(day, anchorDay, nt)
		days = append(days, item)
		if isWeekend(day) {
			weekend = append(weekend, item)
		} else {
			weekdays = append(weekdays, item)
		}
	}

	return NewString(fmt.Sprintf("%04d-W%02d", isoYear, isoWeek)).WithMembers(
		Member{"number", NewNumber(float64(isoWeek))},
		Member{"iso-year", NewNumber(float64(isoYear))},
		Member{"iso", NewString(fmt.Sprintf("%04d-W%02d", isoYear, isoWeek))},
		Member{"year", NewNumber(float64(start.Year()))},
		Member{"start", dayItem(start, anchorDay, nt)},
		Member{"end", dayItem(end, anchorDay, nt)},
		Member{"days", NewList(days)},
		Member{"weekdays", NewList(weekdays)},
		Member{"weekend", NewList(weekend)},
	)
}

// monthValue builds the month namespace containing anchorDay.
func monthValue(anchorDay time.Time, weekStart time.Weekday, nt *nameTable) Value {
	start := firstOfMonth(anchorDay)
	end := addMonths(start, 1).AddDate(0, 0, -1)

	days := make([]Value, 0, 31)
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		days = append(days, dayItem(day, anchorDay, nt))
	}

	// Weeks are full rows: they start on week-start and spill into the
	// neighbouring months, because that is what a month grid on paper looks
	// like and cropping them would leave holes in the row.
	var weeks []Value
	for row := startOfWeek(start, weekStart); !row.After(end); row = row.AddDate(0, 0, 7) {
		weeks = append(weeks, weekValue(row, row.AddDate(0, 0, 3), anchorDay, nt))
	}

	return NewString(nt.monthName(start.Month())).WithMembers(
		Member{"name", NewString(nt.monthName(start.Month()))},
		Member{"short", NewString(nt.shortMonthName(start.Month()))},
		Member{"number", NewNumber(float64(start.Month()))},
		Member{"start", dayItem(start, anchorDay, nt)},
		Member{"end", dayItem(end, anchorDay, nt)},
		Member{"days", NewList(days)},
		Member{"weeks", NewList(weeks)},
	)
}

// quarterValue builds the quarter namespace containing anchorDay.
func quarterValue(anchorDay time.Time, nt *nameTable) Value {
	q := (int(anchorDay.Month()) - 1) / 3
	start := time.Date(anchorDay.Year(), time.Month(q*3+1), 1, 0, 0, 0, 0, anchorDay.Location())
	end := addMonths(start, 3).AddDate(0, 0, -1)
	return NewString(fmt.Sprintf("Q%d", q+1)).WithMembers(
		Member{"number", NewNumber(float64(q + 1))},
		Member{"name", NewString(fmt.Sprintf("Q%d", q+1))},
		Member{"year", NewNumber(float64(start.Year()))},
		Member{"start", dayItem(start, anchorDay, nt)},
		Member{"end", dayItem(end, anchorDay, nt)},
	)
}

// yearValue builds the year namespace containing anchorDay.
func yearValue(anchorDay time.Time, nt *nameTable) Value {
	start := time.Date(anchorDay.Year(), time.January, 1, 0, 0, 0, 0, anchorDay.Location())
	end := time.Date(anchorDay.Year(), time.December, 31, 0, 0, 0, 0, anchorDay.Location())
	return NewNumber(float64(anchorDay.Year())).WithMembers(
		Member{"number", NewNumber(float64(anchorDay.Year()))},
		Member{"start", dayItem(start, anchorDay, nt)},
		Member{"end", dayItem(end, anchorDay, nt)},
		Member{"leap", NewBool(isLeap(anchorDay.Year()))},
	)
}

// ---- date arithmetic ----

// dateOf strips the time of day, keeping the anchor's location. Everything
// date-shaped is midnight-local so that day comparisons are exact equality.
func dateOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func firstOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
}

// startOfWeek returns the most recent weekStart weekday on or before t.
func startOfWeek(t time.Time, weekStart time.Weekday) time.Time {
	offset := (int(t.Weekday()) - int(weekStart) + 7) % 7
	return t.AddDate(0, 0, -offset)
}

// isWeekend reports Saturday and Sunday. This is fixed, not derived from
// week-start: a Sunday-start spread still shades Saturday and Sunday.
func isWeekend(t time.Time) bool {
	return t.Weekday() == time.Saturday || t.Weekday() == time.Sunday
}

// addMonths adds n months, **clamping the day of the month to the last day of
// the target month**: 31 January plus one month is 28 February (29 in a leap
// year), not 3 March.
//
// Go's own AddDate normalises the overflow forward instead, which is the right
// answer for a duration and the wrong one for a calendar. A month grid built on
// AddDate skips February entirely when it is generated from the 31st.
func addMonths(t time.Time, n int) time.Time {
	year, month, day := t.Date()
	total := int(month) - 1 + n
	targetYear := year + floorDiv(total, 12)
	targetMonth := time.Month(floorMod(total, 12) + 1)
	if last := daysInMonth(targetYear, targetMonth); day > last {
		day = last
	}
	return time.Date(targetYear, targetMonth, day, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
}

func daysInMonth(year int, month time.Month) int {
	// The zeroth day of the next month is the last day of this one, and Go
	// normalises that for us without a table of month lengths or a leap-year
	// special case.
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func isLeap(year int) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}

// floorDiv and floorMod round towards negative infinity, so subtracting months
// across a year boundary lands on the right year. Go's / and % truncate towards
// zero, which puts January minus one month in month 0 of the same year.
func floorDiv(a, b int) int {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

func floorMod(a, b int) int {
	m := a % b
	if m != 0 && (m < 0) != (b < 0) {
		m += b
	}
	return m
}
