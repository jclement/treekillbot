package vars

import (
	"testing"
	"time"
)

// date builds a midnight-UTC anchor. UTC keeps %z/%Z assertions stable no
// matter where the test runs.
func date(t *testing.T, iso string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02", iso, time.UTC)
	if err != nil {
		t.Fatalf("bad test date %q: %v", iso, err)
	}
	return parsed
}

// lookup resolves a path or fails the test, which keeps the tables below to
// one line per case.
func lookup(t *testing.T, s *Scope, path string) Value {
	t.Helper()
	v, ok := s.Lookup(path)
	if !ok {
		t.Fatalf("%s did not resolve", path)
	}
	return v
}

func TestISOWeekAndCalendarYearAreIndependent(t *testing.T) {
	// The whole reason week.number and week.iso-year both exist: a header built
	// from week.number and the calendar year reads "Week 53, 2027" on the first
	// of January and is wrong.
	tests := []struct {
		name      string
		anchor    string
		week      string
		isoYear   string
		iso       string
		weekYear  string // calendar year of week.start, Monday-start
		weekStart string
	}{
		{
			name: "new year falls in last year's ISO week", anchor: "2027-01-01",
			week: "53", isoYear: "2026", iso: "2026-W53", weekYear: "2026", weekStart: "2026-12-28",
		},
		{
			name: "last day of 2026 is ISO week 53 of 2026", anchor: "2026-12-31",
			week: "53", isoYear: "2026", iso: "2026-W53", weekYear: "2026", weekStart: "2026-12-28",
		},
		{
			// The trap in the other direction: the last Monday of December
			// already belongs to next year's ISO week 1, so week.year and
			// week.iso-year disagree by one.
			name: "the end of December is already ISO week 1 of next year", anchor: "2019-12-30",
			week: "1", isoYear: "2020", iso: "2020-W01", weekYear: "2019", weekStart: "2019-12-30",
		},
		{
			name: "2021 opens in ISO week 53 of 2020", anchor: "2021-01-01",
			week: "53", isoYear: "2020", iso: "2020-W53", weekYear: "2020", weekStart: "2020-12-28",
		},
		{
			name: "leap day", anchor: "2024-02-29",
			week: "9", isoYear: "2024", iso: "2024-W09", weekYear: "2024", weekStart: "2024-02-26",
		},
		{
			name: "an ordinary Monday", anchor: "2026-08-24",
			week: "35", isoYear: "2026", iso: "2026-W35", weekYear: "2026", weekStart: "2026-08-24",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewScope(date(t, tt.anchor), Options{})
			for _, check := range []struct{ path, want string }{
				{"week.number", tt.week},
				{"week.iso-year", tt.isoYear},
				{"week.iso", tt.iso},
				{"week.year", tt.weekYear},
				{"week.start", tt.weekStart},
			} {
				if got := lookup(t, s, check.path).String(); got != check.want {
					t.Errorf("%s = %q, want %q", check.path, got, check.want)
				}
			}
		})
	}
}

func TestWeekStartRotatesTheWeekWithoutMovingTheISONumber(t *testing.T) {
	// Sunday 2026-08-23 is the interesting anchor: ISO puts it in week 34 (the
	// Mon 17 – Sun 23 week), while a Sunday-start spread shows it as the first
	// day of a row that mostly lies in week 35.
	tests := []struct {
		name              string
		weekStart         time.Weekday
		start, end        string
		number, isoYear   string
		firstDay, lastDay string
	}{
		{"monday", time.Monday, "2026-08-17", "2026-08-23", "34", "2026", "Mon", "Sun"},
		{"sunday", time.Sunday, "2026-08-23", "2026-08-29", "34", "2026", "Sun", "Sat"},
		{"saturday", time.Saturday, "2026-08-22", "2026-08-28", "34", "2026", "Sat", "Fri"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewScope(date(t, "2026-08-23"), Options{}.WithWeekStart(tt.weekStart))
			for _, check := range []struct{ path, want string }{
				{"week.start", tt.start},
				{"week.end", tt.end},
				{"week.number", tt.number},
				{"week.iso-year", tt.isoYear},
			} {
				if got := lookup(t, s, check.path).String(); got != check.want {
					t.Errorf("%s = %q, want %q", check.path, got, check.want)
				}
			}
			days, ok := s.List("week.days")
			if !ok || len(days) != 7 {
				t.Fatalf("week.days = %d items, want 7", len(days))
			}
			if got := member(t, days[0], "short"); got != tt.firstDay {
				t.Errorf("first day = %q, want %q", got, tt.firstDay)
			}
			if got := member(t, days[6], "short"); got != tt.lastDay {
				t.Errorf("last day = %q, want %q", got, tt.lastDay)
			}
		})
	}
}

func TestWeekDefaultsToMonday(t *testing.T) {
	s := NewScope(date(t, "2026-08-23"), Options{})
	if got := lookup(t, s, "week.start").String(); got != "2026-08-17" {
		t.Fatalf("week.start = %q, want the Monday 2026-08-17", got)
	}
}

func member(t *testing.T, v Value, name string) string {
	t.Helper()
	f, ok := v.Field(name)
	if !ok {
		t.Fatalf("no member %q on %v", name, v)
	}
	return f.String()
}

func TestWeekendIsSaturdayAndSundayWhateverTheWeekStarts(t *testing.T) {
	for _, weekStart := range []time.Weekday{time.Monday, time.Sunday, time.Saturday} {
		s := NewScope(date(t, "2026-08-24"), Options{}.WithWeekStart(weekStart))
		weekdays, _ := s.List("week.weekdays")
		weekend, _ := s.List("week.weekend")
		if len(weekdays) != 5 || len(weekend) != 2 {
			t.Fatalf("%v: %d weekdays and %d weekend days, want 5 and 2", weekStart, len(weekdays), len(weekend))
		}
		for _, day := range weekend {
			if short := member(t, day, "short"); short != "Sat" && short != "Sun" {
				t.Errorf("%v: %q counted as weekend", weekStart, short)
			}
		}
	}
}

func TestDayItemMembers(t *testing.T) {
	s := NewScope(date(t, "2026-08-24"), Options{})
	days, ok := s.List("week.days")
	if !ok {
		t.Fatal("week.days is not a list")
	}
	// week.days[5] is Saturday 2026-08-29 under a Monday start.
	saturday := days[5]

	tests := []struct{ member, want string }{
		{"date", "2026-08-29"},
		{"name", "Saturday"},
		{"short", "Sat"},
		{"dd", "29"},
		{"num", "29"},
		{"iso", "2026-08-29"},
		{"weekend", "true"},
		{"today", "false"},
		{"month", "August"},
	}
	for _, tt := range tests {
		if got := member(t, saturday, tt.member); got != tt.want {
			t.Errorf("day.%s = %q, want %q", tt.member, got, tt.want)
		}
	}
	if got := saturday.String(); got != "2026-08-29" {
		t.Errorf("a bare day item renders %q, want the ISO date", got)
	}
	if got := member(t, days[0], "today"); got != "true" {
		t.Errorf("the anchor day's .today = %q, want true", got)
	}
	monthOfDay, _ := saturday.Field("month")
	if got := member(t, monthOfDay, "short"); got != "Aug" {
		t.Errorf("day.month.short = %q, want Aug", got)
	}
	if _, ok := monthOfDay.Field("days"); ok {
		t.Error("day.month carries .days, which makes the value structure infinite")
	}
}

func TestSingleDigitDayPads(t *testing.T) {
	s := NewScope(date(t, "2026-08-03"), Options{})
	today := lookup(t, s, "today")
	if got := member(t, today, "dd"); got != "03" {
		t.Errorf("today.dd = %q, want 03", got)
	}
	if got := member(t, today, "num"); got != "3" {
		t.Errorf("today.num = %q, want 3", got)
	}
}

func TestMonthNamespace(t *testing.T) {
	s := NewScope(date(t, "2024-02-15"), Options{})
	tests := []struct{ path, want string }{
		{"month", "February"},
		{"month.name", "February"},
		{"month.short", "Feb"},
		{"month.number", "2"},
		{"month.start", "2024-02-01"},
		{"month.end", "2024-02-29"},
	}
	for _, tt := range tests {
		if got := lookup(t, s, tt.path).String(); got != tt.want {
			t.Errorf("%s = %q, want %q", tt.path, got, tt.want)
		}
	}

	days, _ := s.List("month.days")
	if len(days) != 29 {
		t.Errorf("February 2024 has %d days, want 29", len(days))
	}

	// The grid rows are whole weeks, spilling into January and March, because a
	// month grid with holes in the first row is not a month grid.
	weeks, _ := s.List("month.weeks")
	if len(weeks) != 5 {
		t.Fatalf("February 2024 spans %d week rows, want 5", len(weeks))
	}
	if got := member(t, weeks[0], "start"); got != "2024-01-29" {
		t.Errorf("first row starts %q, want the Monday 2024-01-29", got)
	}
	if got := member(t, weeks[4], "end"); got != "2024-03-03" {
		t.Errorf("last row ends %q, want 2024-03-03", got)
	}
}

func TestQuarterAndYear(t *testing.T) {
	s := NewScope(date(t, "2024-08-24"), Options{})
	tests := []struct{ path, want string }{
		{"quarter", "Q3"},
		{"quarter.number", "3"},
		{"quarter.name", "Q3"},
		{"quarter.start", "2024-07-01"},
		{"quarter.end", "2024-09-30"},
		{"quarter.year", "2024"},
		{"year", "2024"},
		{"year.number", "2024"},
		{"year.start", "2024-01-01"},
		{"year.end", "2024-12-31"},
		{"year.leap", "true"},
		{"day-of-year", "237"},
	}
	for _, tt := range tests {
		if got := lookup(t, s, tt.path).String(); got != tt.want {
			t.Errorf("%s = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestTodayTomorrowYesterdayAndNow(t *testing.T) {
	anchor := time.Date(2026, 8, 24, 14, 3, 5, 0, time.UTC)
	s := NewScope(anchor, Options{})
	tests := []struct{ path, want string }{
		{"today", "2026-08-24"},
		{"tomorrow", "2026-08-25"},
		{"yesterday", "2026-08-23"},
		{"now", "2026-08-24T14:03:05"},
		{"tomorrow.name", "Tuesday"},
		{"yesterday.weekend", "true"},
	}
	for _, tt := range tests {
		if got := lookup(t, s, tt.path).String(); got != tt.want {
			t.Errorf("%s = %q, want %q", tt.path, got, tt.want)
		}
	}
	if got := s.Anchor(); !got.Equal(anchor) {
		t.Errorf("Anchor() = %v, want the injected %v", got, anchor)
	}
}

func TestAddMonthsClampsToTheLastDayOfTheTargetMonth(t *testing.T) {
	// The rule: the day of the month is clamped, never rolled forward. Go's
	// AddDate turns 31 January plus a month into 3 March, which would make a
	// month grid generated from the 31st skip February entirely.
	tests := []struct {
		name  string
		from  string
		add   int
		want  string
		place string
	}{
		{name: "jan 31 plus one month clamps to february", from: "2026-01-31", add: 1, want: "2026-02-28"},
		{name: "jan 31 plus one month in a leap year", from: "2024-01-31", add: 1, want: "2024-02-29"},
		{name: "march 31 minus one month clamps", from: "2024-03-31", add: -1, want: "2024-02-29"},
		{name: "may 31 plus one month clamps to 30", from: "2026-05-31", add: 1, want: "2026-06-30"},
		{name: "a safe day is untouched", from: "2026-01-15", add: 1, want: "2026-02-15"},
		{name: "crossing into the next year", from: "2026-11-30", add: 2, want: "2027-01-30"},
		{name: "crossing into the previous year", from: "2026-01-15", add: -1, want: "2025-12-15"},
		{name: "thirteen months back", from: "2026-03-31", add: -13, want: "2025-02-28"},
		{name: "twelve months is the same day", from: "2024-02-29", add: 12, want: "2025-02-28"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := addMonths(date(t, tt.from), tt.add).Format("2006-01-02")
			if got != tt.want {
				t.Fatalf("addMonths(%s, %d) = %s, want %s", tt.from, tt.add, got, tt.want)
			}
		})
	}
}

func TestMonthEndOnEveryMonthOfALeapYear(t *testing.T) {
	want := []string{"31", "29", "31", "30", "31", "30", "31", "31", "30", "31", "30", "31"}
	for month := 1; month <= 12; month++ {
		anchor := time.Date(2024, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
		s := NewScope(anchor, Options{})
		end := lookup(t, s, "month.end")
		if got := member(t, end, "dd"); got != want[month-1] {
			t.Errorf("month %d ends on the %s, want %s", month, got, want[month-1])
		}
	}
}

func TestNameOverridesRebuildTheBuiltins(t *testing.T) {
	s := NewScope(date(t, "2026-08-24"), Options{})
	months := []string{"janvier", "février", "mars", "avril", "mai", "juin",
		"juillet", "août", "septembre", "octobre", "novembre", "décembre"}
	days := []string{"dimanche", "lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi"}
	if err := s.SetMonthNames(months, nil); err != nil {
		t.Fatalf("SetMonthNames: %v", err)
	}
	if err := s.SetDayNames(days, nil); err != nil {
		t.Fatalf("SetDayNames: %v", err)
	}
	if got := lookup(t, s, "month.name").String(); got != "août" {
		t.Errorf("month.name = %q, want août", got)
	}
	if got := lookup(t, s, "today.name").String(); got != "lundi" {
		t.Errorf("today.name = %q, want lundi", got)
	}
	// Short names are derived by truncation, in runes — "août" is 5 bytes.
	if got := lookup(t, s, "month.short").String(); got != "aoû" {
		t.Errorf("month.short = %q, want aoû", got)
	}
	if err := s.SetDayNames([]string{"one"}, nil); err == nil {
		t.Error("SetDayNames accepted a list of 1")
	}
}

func TestParseAnchor(t *testing.T) {
	tests := []struct {
		name, in string
		want     string
		wantErr  bool
	}{
		{name: "bare date", in: "2026-08-24", want: "2026-08-24 00:00:00"},
		{name: "with a time", in: "2026-08-24T14:03:05", want: "2026-08-24 14:03:05"},
		{name: "surrounding space", in: " 2026-08-24 ", want: "2026-08-24 00:00:00"},
		{name: "not a date", in: "next tuesday", wantErr: true},
		{name: "american order is refused", in: "08/24/2026", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAnchor(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseAnchor(%q) succeeded, want an error", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseAnchor(%q): %v", tt.in, err)
			}
			if got.Format("2006-01-02 15:04:05") != tt.want {
				t.Fatalf("ParseAnchor(%q) = %v, want %s", tt.in, got, tt.want)
			}
		})
	}
}
