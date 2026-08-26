package vars

import (
	"strings"
	"testing"
	"time"
)

// noon is a Monday afternoon in UTC: 2026-08-24 is day 236 of the year and
// sits in ISO week 35.
var noon = time.Date(2026, 8, 24, 14, 3, 5, 0, time.UTC)

func TestStrftimeDirectives(t *testing.T) {
	tests := []struct {
		layout, want string
	}{
		{"%Y", "2026"},
		{"%y", "26"},
		{"%m", "08"},
		{"%d", "24"},
		{"%e", "24"},
		{"%H", "14"},
		{"%I", "02"},
		{"%M", "03"},
		{"%S", "05"},
		{"%p", "PM"},
		{"%A", "Monday"},
		{"%a", "Mon"},
		{"%B", "August"},
		{"%b", "Aug"},
		{"%j", "236"},
		{"%U", "34"},
		{"%W", "34"},
		{"%V", "35"},
		{"%G", "2026"},
		{"%u", "1"},
		{"%w", "1"},
		{"%F", "2026-08-24"},
		{"%T", "14:03:05"},
		{"%z", "+0000"},
		{"%Z", "UTC"},
		{"%%", "%"},
		{"%Y-%m-%d", "2026-08-24"},
		{"%-m/%-d", "8/24"},
		// The point of strftime over Go layouts: prose is inert.
		{"Q1 2006 review", "Q1 2006 review"},
		{"Week %V of %G", "Week 35 of 2026"},
	}
	for _, tt := range tests {
		t.Run(tt.layout, func(t *testing.T) {
			got, err := strftime(noon, tt.layout, defaultNames())
			if err != nil {
				t.Fatalf("strftime(%q): %v", tt.layout, err)
			}
			if got != tt.want {
				t.Fatalf("strftime(%q) = %q, want %q", tt.layout, got, tt.want)
			}
		})
	}
}

func TestStrftimePadding(t *testing.T) {
	// A single-digit day is where the three day directives differ, and where a
	// planner header goes from "August 04" to "August 4".
	early := time.Date(2026, 8, 3, 0, 30, 0, 0, time.UTC)
	tests := []struct{ layout, want string }{
		{"%d", "03"},
		{"%e", " 3"},
		{"%-d", "3"},
		{"%-e", "3"},
		{"%H:%M", "00:30"},
		{"%I:%M %p", "12:30 AM"},
		{"%-I:%M %p", "12:30 AM"},
		{"%j", "215"},
		{"%-j", "215"},
	}
	for _, tt := range tests {
		got, err := strftime(early, tt.layout, defaultNames())
		if err != nil {
			t.Fatalf("strftime(%q): %v", tt.layout, err)
		}
		if got != tt.want {
			t.Errorf("strftime(%q) = %q, want %q", tt.layout, got, tt.want)
		}
	}
}

func TestStrftimeRejectsUnknownDirectives(t *testing.T) {
	// Emitting an unknown directive raw would put "%Q" on a printed page.
	tests := []string{"%Q", "%", "%-", "%1"}
	for _, layout := range tests {
		if got, err := strftime(noon, layout, defaultNames()); err == nil {
			t.Errorf("strftime(%q) = %q, want an error", layout, got)
		}
	}
}

func TestNamedTimeFormats(t *testing.T) {
	tests := []struct {
		spec string
		v    Value
		want string
	}{
		{"", NewDate(noon), "2026-08-24"},
		{"iso", NewDate(noon), "2026-08-24"},
		{"iso", NewDateTime(noon), "2026-08-24T14:03:05"},
		{"", NewDateTime(noon), "2026-08-24T14:03:05"},
		{"short", NewDate(noon), "Aug 24"},
		{"long", NewDate(noon), "Monday, August 24, 2026"},
		{"date", NewDate(noon), "August 24, 2026"},
		{"time", NewDateTime(noon), "2:03 PM"},
	}
	for _, tt := range tests {
		got, err := formatValue(tt.v, tt.spec, defaultNames())
		if err != nil {
			t.Fatalf("formatValue(%q): %v", tt.spec, err)
		}
		if got != tt.want {
			t.Errorf("formatValue(%q) = %q, want %q", tt.spec, got, tt.want)
		}
	}
}

func TestGoLayoutsOnlyApplyWhenTagged(t *testing.T) {
	// The grenade, defused by being opt-in. DESIGN.md D12 cites this layout as
	// rendering "Q1 2026 review"; it is in fact worse, because "1" is the month
	// as well — August turns the quarter label into Q8.
	got, err := formatValue(NewDate(noon), `go:"Q1 2006 review"`, defaultNames())
	if err != nil {
		t.Fatalf("go layout: %v", err)
	}
	if got != "Q8 2026 review" {
		t.Fatalf(`go:"Q1 2006 review" = %q, want "Q8 2026 review"`, got)
	}
	if _, err := formatValue(NewString("x"), `go:"2006"`, defaultNames()); err == nil {
		t.Error("a go layout on a string succeeded, want an error")
	}
}

func TestStringFormats(t *testing.T) {
	tests := []struct {
		name, spec, in, want string
		wantErr              bool
	}{
		{name: "upper", spec: "upper", in: "notes", want: "NOTES"},
		{name: "lower", spec: "lower", in: "NOTES", want: "notes"},
		{name: "title", spec: "title", in: "jeff clement", want: "Jeff Clement"},
		{name: "title lowercases the rest", spec: "title", in: "JEFF CLEMENT", want: "Jeff Clement"},
		{name: "trunc cuts", spec: "trunc 4", in: "Wednesday", want: "Wedn"},
		{name: "trunc leaves short text alone", spec: "trunc 40", in: "Wed", want: "Wed"},
		{name: "trunc counts runes", spec: "trunc 3", in: "décembre", want: "déc"},
		{name: "pad right-pads", spec: "pad 5", in: "Mon", want: "Mon  "},
		{name: "pad never truncates", spec: "pad 2", in: "Wednesday", want: "Wednesday"},
		{name: "trunc needs a width", spec: "trunc", in: "x", wantErr: true},
		{name: "unknown format", spec: "capitalise", in: "x", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := formatValue(NewString(tt.in), tt.spec, defaultNames())
			if tt.wantErr {
				if err == nil {
					t.Fatalf("formatValue(%q) = %q, want an error", tt.spec, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("formatValue(%q): %v", tt.spec, err)
			}
			if got != tt.want {
				t.Fatalf("formatValue(%q) = %q, want %q", tt.spec, got, tt.want)
			}
		})
	}
}

func TestNumberFormats(t *testing.T) {
	tests := []struct {
		name, spec string
		v          Value
		want       string
		wantErr    bool
	}{
		{name: "default drops the decimal point", spec: "", v: NewNumber(35), want: "35"},
		{name: "zero padded", spec: "%02d", v: NewNumber(3), want: "03"},
		{name: "width", spec: "%4d", v: NewNumber(35), want: "  35"},
		{name: "float", spec: "%.2f", v: NewNumber(1.5), want: "1.50"},
		{name: "fractions survive the default", spec: "", v: NewNumber(1.5), want: "1.5"},
		{name: "string verb on a number", spec: "%s", v: NewNumber(35), want: "35"},
		{name: "number verb on text", spec: "%02d", v: NewString("x"), wantErr: true},
		{name: "two verbs", spec: "%d%d", v: NewNumber(1), wantErr: true},
		{name: "incomplete verb", spec: "%0", v: NewNumber(1), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := formatValue(tt.v, tt.spec, defaultNames())
			if tt.wantErr {
				if err == nil {
					t.Fatalf("formatValue(%q) = %q, want an error", tt.spec, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("formatValue(%q): %v", tt.spec, err)
			}
			if got != tt.want {
				t.Fatalf("formatValue(%q) = %q, want %q", tt.spec, got, tt.want)
			}
		})
	}
}

func TestUnknownFormatCarriesItsVocabulary(t *testing.T) {
	_, err := formatValue(NewDate(noon), "nope", defaultNames())
	if err == nil {
		t.Fatal("an unknown date format succeeded")
	}
	var unknown *unknownFormatError
	if !asUnknownFormat(err, &unknown) {
		t.Fatalf("error %v is not an unknownFormatError, so no suggestion can be offered", err)
	}
	if !strings.Contains(strings.Join(unknown.Known, " "), "long") {
		t.Errorf("date vocabulary %v does not include the named formats", unknown.Known)
	}
}

// asUnknownFormat keeps the errors.As dance out of the test body.
func asUnknownFormat(err error, target **unknownFormatError) bool {
	u, ok := err.(*unknownFormatError)
	if ok {
		*target = u
	}
	return ok
}

func TestNameOverridesReachStrftime(t *testing.T) {
	nt := defaultNames()
	nt.days = []string{"dimanche", "lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi"}
	nt.months = shorten(nt.months) // any change is enough to prove the table is consulted
	got, err := strftime(noon, "%A", nt)
	if err != nil {
		t.Fatalf("strftime: %v", err)
	}
	if got != "lundi" {
		t.Fatalf("%%A = %q, want lundi", got)
	}
}
