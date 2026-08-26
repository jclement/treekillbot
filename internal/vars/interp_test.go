package vars

import (
	"strings"
	"testing"

	"github.com/jclement/treekillbot/internal/pulp"
)

// expand interpolates text against s and returns the result with whatever was
// reported. The span covers the whole string, so a diagnostic's Span.Start is
// the byte offset of the offending `{`.
func expand(t *testing.T, s *Scope, text string) (string, pulp.Diagnostics) {
	t.Helper()
	src := pulp.NewSource("t.pulp", text)
	var diags pulp.Diagnostics
	out := s.Interpolate(text, pulp.Span{Start: 0, End: len(text)}, src, &diags)
	return out, diags
}

// clean interpolates and fails if anything was reported.
func clean(t *testing.T, s *Scope, text string) string {
	t.Helper()
	out, diags := expand(t, s, text)
	if len(diags) > 0 {
		t.Fatalf("interpolating %q reported %s", text, diags[0].Plain())
	}
	return out
}

// planScope is a scope with the fixtures the interpolation tests share.
func planScope(t *testing.T) *Scope {
	t.Helper()
	s := NewScope(date(t, "2026-08-24"), Options{})
	s.Define("owner", NewString("jeff clement"), LayerDocument)
	s.Define("accent", NewString("#1f6feb"), LayerDocument)
	return s
}

func TestInterpolateReferences(t *testing.T) {
	s := planScope(t)
	days, _ := s.List("week.days")
	s.Push()
	s.Bind("day", days[5]) // Saturday
	s.Bind("loop", NewLoop(0, 7))
	defer s.Pop()

	tests := []struct{ name, in, want string }{
		{"plain text is untouched", "Notes", "Notes"},
		{"a built-in", "{today}", "2026-08-24"},
		{"a member", "Week {week.number}", "Week 35"},
		{"two in one string", "{week.number}/{week.iso-year}", "35/2026"},
		{"a strftime format", "{today:%A}", "Monday"},
		{"a named format", "{today:long}", "Monday, August 24, 2026"},
		{"a format with colons in it", "{now:%H:%M}", "00:00"},
		{"a printf format", "{week.number:%03d}", "035"},
		{"a string format", "{owner:title}", "Jeff Clement"},
		{"a fallback that is not needed", "{owner|nobody}", "jeff clement"},
		{"a fallback that is", "{title|Untitled}", "Untitled"},
		{"a format and a fallback", "{missing:upper|ANON}", "ANON"},
		{"a loop binding", "{loop.n} of {loop.count}", "1 of 7"},
		{"a day item", "{day.short} {day.dd}", "Sat 29"},
		{"a day item formats as a date", "{day:%b %-d}", "Aug 29"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clean(t, s, tt.in); got != tt.want {
				t.Fatalf("%q → %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestInterpolateEscapes(t *testing.T) {
	s := planScope(t)
	tests := []struct{ name, in, want string }{
		{"doubled braces are literal", "{{today}}", "{today}"},
		{"a lone doubled open brace", "a {{ b", "a { b"},
		{"a lone doubled close brace", "a }} b", "a } b"},
		{"an unmatched close brace is left alone", "a } b", "a } b"},
		// The backslash survives on purpose: the value layer unescapes it after
		// interpolation, and stripping it here would hand the value tokenizer a
		// bare brace it would read as an interpolation.
		{"a backslash suppresses interpolation", `\{today\}`, `\{today\}`},
		{"escapes next to a real reference", "{{{today}}}", "{2026-08-24}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clean(t, s, tt.in); got != tt.want {
				t.Fatalf("%q → %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestInterpolateTernary(t *testing.T) {
	s := planScope(t)
	days, _ := s.List("week.days")
	s.Push()
	s.Bind("day", days[5]) // Saturday: weekend, not today
	s.Bind("weekday", days[0])
	s.Bind("loop", NewLoop(0, 7))
	defer s.Pop()

	tests := []struct{ name, in, want string }{
		{"true branch", `{day.weekend ? "#f7f7f4" : "#ffffff"}`, "#f7f7f4"},
		{"false branch", `{weekday.weekend ? "#f7f7f4" : "#ffffff"}`, "#ffffff"},
		{"unquoted hex is a literal", "{day.weekend ? #f7f7f4 : #ffffff}", "#f7f7f4"},
		{"a length is a literal", "{day.weekend ? 12pt : 8pt}", "12pt"},
		{"a bare identifier is a variable", `{day.weekend ? accent : "#fff"}`, "#1f6feb"},
		{"not", `{not day.weekend ? "week" : "end"}`, "end"},
		{"an empty branch", `{day.weekend ? "SAT" : }`, "SAT"},
		{"equality against a string", `{day.short == "Sat" ? "yes" : "no"}`, "yes"},
		{"equality against a bare word", `{day.short == Sat ? "yes" : "no"}`, "yes"},
		{"inequality", `{day.short != "Sun" ? "yes" : "no"}`, "yes"},
		{"numbers compare numerically", `{loop.n == 1 ? "first" : "later"}`, "first"},
		{"booleans compare by word", `{day.weekend == true ? "yes" : "no"}`, "yes"},
		{"not with a comparison", `{not day.short == "Sat" ? "yes" : "no"}`, "no"},
		{"a ternary in the middle of text", `x {day.today ? "*" : ""} y`, "x  y"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clean(t, s, tt.in); got != tt.want {
				t.Fatalf("%q → %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestInterpolateMalformed(t *testing.T) {
	s := planScope(t)
	tests := []struct{ name, in, code string }{
		{"unclosed brace", "Week {week.number", "E200"},
		{"empty interpolation", "a {} b", "E200"},
		{"not a name", "{week number}", "E200"},
		{"ternary with no else", `{day.weekend ? "a"}`, "E201"},
		{"a branch containing a colon", `{today.weekend ? a : b : c}`, "E201"},
		{"a branch containing a question mark", `{today.weekend ? a ? b : c}`, "E201"},
		{"a condition that is not a name", `{1 == 2 ? "a" : "b"}`, "E201"},
		{"an unknown format", "{today:nope}", "E202"},
		{"an unknown strftime directive", "{today:%Q}", "E202"},
		{"a number format on text", "{owner:%02d}", "E202"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := expand(t, s, tt.in)
			if len(diags) == 0 {
				t.Fatalf("%q reported nothing, want %s", tt.in, tt.code)
			}
			if diags[0].Code != tt.code {
				t.Fatalf("%q reported %s (%s), want %s", tt.in, diags[0].Code, diags[0].Message, tt.code)
			}
		})
	}
}

func TestUnresolvedReferenceIsAnErrorWithASpanAndASuggestion(t *testing.T) {
	s := planScope(t)
	const text = "Week {week.numbr} of the year"
	out, diags := expand(t, s, text)

	if out != "Week  of the year" {
		t.Errorf("output %q, want the reference rendered empty", out)
	}
	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics, want 1", len(diags))
	}
	d := diags[0]
	if d.Code != "E210" || d.Severity != pulp.SeverityError {
		t.Errorf("got %s %v, want E210 error", d.Code, d.Severity)
	}
	// The caret must land on the `{`, not on the line.
	if d.Span.Start != strings.Index(text, "{") || d.Span.End != strings.Index(text, "}")+1 {
		t.Errorf("span %v, want %d..%d", d.Span, strings.Index(text, "{"), strings.Index(text, "}")+1)
	}
	if !strings.Contains(d.Help, "number") {
		t.Errorf("help %q does not suggest `number`", d.Help)
	}
}

func TestUnknownRootSuggestsANameInScope(t *testing.T) {
	s := planScope(t)
	s.Push()
	s.Bind("day", NewString("x"))
	defer s.Pop()

	_, diags := expand(t, s, "{dya.short}")
	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics, want 1", len(diags))
	}
	if !strings.Contains(diags[0].Help, "`day`") {
		t.Fatalf("help %q does not suggest the lexical binding `day`", diags[0].Help)
	}
}

func TestAllowUndefinedDowngradesToAWarning(t *testing.T) {
	s := NewScope(date(t, "2026-08-24"), Options{AllowUndefined: true})
	out, diags := expand(t, s, "owner: {owner}")
	if out != "owner: " {
		t.Errorf("output %q, want the reference rendered empty", out)
	}
	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics, want 1", len(diags))
	}
	if diags[0].Severity != pulp.SeverityWarning || diags[0].Code != "W210" {
		t.Fatalf("got %s %v, want W210 warning", diags[0].Code, diags[0].Severity)
	}
	if diags.HasErrors() {
		t.Error("the document still has errors, so --allow-undefined would not build")
	}
}

func TestUnresolvedNamesInConditionsAndBranchesAreReported(t *testing.T) {
	// A misspelled condition that silently evaluates false means a weekend
	// column that is simply never shaded, and nothing says why.
	s := planScope(t)
	for _, text := range []string{
		`{day.weeknd ? "a" : "b"}`,
		`{owner ? bogus : "b"}`,
	} {
		_, diags := expand(t, s, text)
		if len(diags) == 0 {
			t.Errorf("%q reported nothing", text)
			continue
		}
		if diags[0].Code != "E210" {
			t.Errorf("%q reported %s, want E210", text, diags[0].Code)
		}
	}
}

func TestUnresolvedBranchWordSuggestsQuoting(t *testing.T) {
	s := planScope(t)
	_, diags := expand(t, s, `{owner ? solid : dashed}`)
	if len(diags) == 0 {
		t.Fatal("reported nothing")
	}
	if !strings.Contains(diags[0].Help, `"solid"`) {
		t.Fatalf("help %q does not suggest quoting the word", diags[0].Help)
	}
}

func TestPageNumbersAreDeferredUntilBound(t *testing.T) {
	s := planScope(t)
	const text = "{page.number} / {page.count}"
	src := pulp.NewSource("t.pulp", text)

	var early pulp.Diagnostics
	out, deferred := s.InterpolateDeferred(text, pulp.Span{Start: 0, End: len(text)}, src, &early)
	if len(early) != 0 {
		t.Fatalf("an early pass reported %s; page numbers are not a user error", early[0].Plain())
	}
	if !deferred {
		t.Fatal("the early pass did not report the text as deferred, so it would never be re-rendered")
	}
	if out != " / " {
		t.Errorf("early output %q, want the references empty", out)
	}

	// A fallback stands in for the real number until pagination has run.
	if got := clean(t, s, "{page.count|?}"); got != "?" {
		t.Errorf("deferred fallback = %q, want ?", got)
	}

	s.BindPage(2, 7)
	var late pulp.Diagnostics
	out, deferred = s.InterpolateDeferred(text, pulp.Span{Start: 0, End: len(text)}, src, &late)
	if len(late) != 0 {
		t.Fatalf("the bound pass reported %s", late[0].Plain())
	}
	if out != "2 / 7" || deferred {
		t.Fatalf("bound output %q (deferred %v), want \"2 / 7\" and false", out, deferred)
	}
}

func TestAMisspelledPageMemberIsCaughtBeforeBinding(t *testing.T) {
	// Only the value is deferred; the member name is checkable in the early
	// pass, so a typo does not survive until render time.
	s := planScope(t)
	_, diags := expand(t, s, "{page.cuont}")
	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics, want 1", len(diags))
	}
	if diags[0].Code != "E210" || !strings.Contains(diags[0].Help, "count") {
		t.Fatalf("got %s %q, want E210 suggesting `count`", diags[0].Code, diags[0].Help)
	}
}

func TestInterpolateLeavesUnbracedTextAlone(t *testing.T) {
	s := planScope(t)
	const text = "100% cotton — 50/50 — a:b|c ? d"
	if got := clean(t, s, text); got != text {
		t.Fatalf("%q → %q", text, got)
	}
}
