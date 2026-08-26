// Interpolation: turning `Week {week.number} of {week.iso-year}` into text.
//
// The five forms, all of them one pair of braces (spec §3.1):
//
//	{path}                    {week.number}
//	{path:format}             {today:%A}        {day.name:upper}
//	{path|fallback}           {title|Untitled}
//	{path:format|fallback}    {owner:upper|ANON}
//	{cond ? a : b}            {day.weekend ? "#f7f7f4" : "#ffffff"}
//
// `:` introduces a format, `|` a fallback and is always last, and the spaces
// around ` ? ` and ` : ` in a ternary are required — which is what lets
// `{today:%H:%M}` and `{day.weekend ? "#eee" : "#fff"}` share one grammar with
// no lookahead and no ambiguity.
//
// The condition grammar is capped at an optional `not`, a dotted path, and an
// optional `==` / `!=` against a literal. The cap is the feature (DESIGN.md §6):
// the moment conditions grow, the next three requests are `elif`, comparison
// chaining and arithmetic, and the tool has grown a bad Jinja inside it.
//
// Every interpolation carries the byte span of its `{`, so an unresolved
// variable underlines the reference rather than the line. Offsets are relative
// to the span the text came from.
//
// **Quoting is the caller's job.** This interpolates a plain string; single
// quotes are raw in Pulp (`'{not a var}'`), and only the value layer knows
// where the quotes were, so it decides whether to call this at all. For the
// same reason `\{` is emitted verbatim, backslash included: the value layer's
// unescape turns it into a brace afterwards, and doing it here would produce a
// bare `{` that the value tokenizer would then see as an interpolation.
package vars

import (
	"errors"
	"strconv"
	"strings"

	"github.com/jclement/treekillbot/internal/pulp"
)

// Interpolate expands every `{…}` in text, reporting problems against src at
// offsets relative to span.
func (s *Scope) Interpolate(text string, span pulp.Span, src *pulp.Source, diags *pulp.Diagnostics) string {
	out, _ := s.InterpolateDeferred(text, span, src, diags)
	return out
}

// InterpolateDeferred is Interpolate plus whether the result still depends on
// values that are not bound yet — in practice `page.number` and `page.count`,
// which pagination only knows at render time.
//
// A deferred reference renders as its fallback, or empty when it has none, and
// is *not* reported as a user error. The caller keeps the original text and
// calls Interpolate again once BindPage has run; re-interpolating the source
// rather than the output is what keeps spans exact and stops a `{{` that was
// already unescaped from being expanded twice.
func (s *Scope) InterpolateDeferred(text string, span pulp.Span, src *pulp.Source, diags *pulp.Diagnostics) (string, bool) {
	if !strings.ContainsAny(text, "{}") {
		return text, false
	}
	ip := &interpolator{scope: s, span: span, src: src, diags: diags}
	return ip.run(text), ip.deferred
}

// interpolator is the state of one Interpolate call.
type interpolator struct {
	scope *Scope
	span  pulp.Span
	src   *pulp.Source
	diags *pulp.Diagnostics

	deferred bool
}

func (ip *interpolator) run(text string) string {
	var b strings.Builder
	b.Grow(len(text))

	for i := 0; i < len(text); {
		c := text[i]
		switch {
		case c == '{' && i+1 < len(text) && text[i+1] == '{':
			b.WriteByte('{')
			i += 2
		case c == '}' && i+1 < len(text) && text[i+1] == '}':
			b.WriteByte('}')
			i += 2
		case c == '\\' && i+1 < len(text) && (text[i+1] == '{' || text[i+1] == '}'):
			// Left for the value layer to unescape; see the file comment.
			b.WriteString(text[i : i+2])
			i += 2
		case c == '{':
			end := findClose(text, i)
			if end < 0 {
				ip.diags.Errorf(ip.src, ip.sub(i, len(text)), "E200", "unclosed `{`").
					WithLabel("no closing brace").
					WithHelp("Close it with `}`, or write `{{` for a literal brace.")
				b.WriteString(text[i:])
				return b.String()
			}
			b.WriteString(ip.expand(text[i+1:end], ip.sub(i, end+1)))
			i = end + 1
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// sub converts offsets within the interpolated text into whole-file span,
// clamping to the span the text came from so a diagnostic can never underline
// past it.
func (ip *interpolator) sub(start, end int) pulp.Span {
	sp := ip.span.Sub(start, end)
	if ip.span.IsZero() {
		return sp
	}
	if sp.Start > ip.span.End {
		sp.Start = ip.span.End
	}
	if sp.End > ip.span.End {
		sp.End = ip.span.End
	}
	return sp
}

// expand evaluates one interpolation body — everything between the braces.
func (ip *interpolator) expand(body string, sp pulp.Span) string {
	if strings.TrimSpace(body) == "" {
		ip.diags.Errorf(ip.src, sp, "E200", "empty interpolation").
			WithHelp("Write a variable name, as in `{today}`, or `{{` for a literal brace.")
		return ""
	}
	if i := indexOutside(body, " ? "); i >= 0 {
		return ip.ternary(body[:i], body[i+3:], sp)
	}
	return ip.reference(body, sp)
}

// ---- {path:format|fallback} ----

func (ip *interpolator) reference(body string, sp pulp.Span) string {
	expr := body

	fallback := ""
	hasFallback := false
	if i := indexOutside(expr, "|"); i >= 0 {
		fallback, hasFallback = literalText(expr[i+1:]), true
		expr = expr[:i]
	}

	// The format spec runs to the end of the expression: `%H:%M` and
	// `go:"Mon Jan 2"` both contain colons of their own, so only the first one
	// separates.
	format := ""
	if i := indexOutside(expr, ":"); i >= 0 {
		format = expr[i+1:]
		expr = expr[:i]
	}

	path := strings.TrimSpace(expr)
	if !validPath(path) {
		ip.diags.Errorf(ip.src, sp, "E200", "`%s` is not a variable name", path).
			WithHelp("Names are letters, digits and dashes, with `.` between parts: `{week.iso-year}`.")
		return ""
	}

	r := ip.scope.resolve(path)
	switch r.status {
	case resolveFound:
		out, err := formatValue(r.val, format, ip.scope.names)
		if err != nil {
			ip.formatError(err, format, sp)
			return ""
		}
		return out
	case resolveDeferred:
		ip.deferred = true
		return fallback
	case resolveEnvUndeclared:
		ip.refuseEnv(r.member, sp)
		return ""
	}

	if hasFallback {
		return fallback
	}
	ip.unresolved(r, path, sp, false)
	return ""
}

// ---- {cond ? a : b} ----

func (ip *interpolator) ternary(cond, rest string, sp pulp.Span) string {
	i := indexOutside(rest, " : ")
	if i < 0 {
		ip.diags.Errorf(ip.src, sp, "E201", "conditional has no `:` branch").
			WithHelp("Write it as `{cond ? a : b}`, with spaces around both `?` and `:`.")
		return ""
	}
	yes, no := rest[:i], rest[i+3:]
	for _, branch := range []string{yes, no} {
		if indexOutside(branch, "?") >= 0 || indexOutside(branch, ":") >= 0 {
			ip.diags.Errorf(ip.src, sp, "E201", "a conditional branch may not contain `?` or `:`").
				WithHelp("Conditionals do not nest. Bind the inner result with `let` and name it here.")
			return ""
		}
	}
	if ip.condition(cond, sp) {
		return ip.operand(yes, sp)
	}
	return ip.operand(no, sp)
}

// condition evaluates the capped boolean grammar: [not] path [(==|!=) literal].
func (ip *interpolator) condition(text string, sp pulp.Span) bool {
	text = strings.TrimSpace(text)

	negate := false
	if rest, ok := strings.CutPrefix(text, "not "); ok {
		negate, text = true, strings.TrimSpace(rest)
	}

	op, lhs, rhs := "", text, ""
	for _, candidate := range []string{"==", "!="} {
		if i := indexOutside(text, candidate); i >= 0 {
			op, lhs, rhs = candidate, text[:i], text[i+2:]
			break
		}
	}

	path := strings.TrimSpace(lhs)
	if !validPath(path) {
		ip.diags.Errorf(ip.src, sp, "E201", "`%s` is not a variable name", path).
			WithHelp("A condition is a variable, optionally `not`, optionally compared with `==` or `!=`.")
		return false
	}

	v := ip.conditionValue(path, sp)
	result := false
	switch op {
	case "":
		result = v.Truthy()
	case "==":
		result = valueEquals(v, rhs)
	case "!=":
		result = !valueEquals(v, rhs)
	}
	return result != negate
}

// conditionValue resolves the left-hand side of a condition. An unresolved
// name is reported exactly as it would be anywhere else — a condition on a
// misspelled variable is silently false otherwise, which on paper means a
// weekend column that is simply never shaded.
func (ip *interpolator) conditionValue(path string, sp pulp.Span) Value {
	r := ip.scope.resolve(path)
	switch r.status {
	case resolveFound:
		return r.val
	case resolveDeferred:
		ip.deferred = true
	case resolveEnvUndeclared:
		ip.refuseEnv(r.member, sp)
	default:
		ip.unresolved(r, path, sp, false)
	}
	return Value{}
}

// operand evaluates one branch of a ternary. A bare identifier is a variable;
// `#hex`, numbers and lengths are literals; quoted text is a string literal.
func (ip *interpolator) operand(text string, sp pulp.Span) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if isQuoted(text) {
		return text[1 : len(text)-1]
	}
	if !validPath(text) {
		// `#f7f7f4`, `12pt`, `50%` — anything that cannot be a name is itself.
		return text
	}

	r := ip.scope.resolve(text)
	switch r.status {
	case resolveFound:
		return r.val.String()
	case resolveDeferred:
		ip.deferred = true
		return ""
	case resolveEnvUndeclared:
		ip.refuseEnv(r.member, sp)
		return ""
	}
	ip.unresolved(r, text, sp, true)
	return ""
}

// ---- diagnostics ----

// unresolved reports a reference that did not resolve, as an error by default
// and as a warning under --allow-undefined. inBranch adds the hint that only
// applies inside a ternary, where quoting is the fix for a word that was meant
// literally.
func (ip *interpolator) unresolved(r resolution, path string, sp pulp.Span, inBranch bool) {
	report := ip.diags.Errorf
	code := "E210"
	if ip.scope.opts.AllowUndefined {
		report = ip.diags.Warnf
		code = "W210"
	}

	switch r.status {
	case resolveEnvUnset:
		report(ip.src, sp, code, "environment variable `%s` is not set", r.member).
			WithLabel("no value").
			WithHelp("Give it a fallback: `{env.%s|something}`.", r.member)
		return
	case resolveMissingMember:
		d := report(ip.src, sp, code, "`%s` has no member `%s`", r.root, r.member).
			WithLabel("unknown member")
		if help := pulp.FormatSuggestions("member", pulp.Suggest(r.member, r.candidates)); help != "" {
			d.WithHelp("%s", help)
		}
		return
	}

	d := report(ip.src, sp, code, "unknown variable `%s`", path).
		WithLabel("not defined here")
	help := pulp.FormatSuggestions("variable", pulp.Suggest(r.member, r.candidates))
	switch {
	case help != "":
		d.WithHelp("%s", help)
	case inBranch:
		d.WithHelp("Quote it — `\"%s\"` — to use it as literal text.", path)
	default:
		d.WithHelp("Declare it in a `vars` block, or pass `--var %s=…`.", path)
	}
}

// refuseEnv reports an environment variable the document never declared. This
// is a refusal, not a lookup failure: a fallback does not make it legal, and
// --allow-undefined does not downgrade it (DESIGN.md D11).
func (ip *interpolator) refuseEnv(name string, sp pulp.Span) {
	ip.diags.Errorf(ip.src, sp, "E212", "environment variable `%s` is not declared", name).
		WithLabel("undeclared").
		WithHelp("Declare `%s` in the document's `vars` block, or pass --unsafe-env.", name).
		WithNote("Environment variables are declared, never ambient, so a document you received cannot read your shell.")
}

// formatError turns a Format failure into a diagnostic, adding a did-you-mean
// over the vocabulary that applied to the value's type.
func (ip *interpolator) formatError(err error, spec string, sp pulp.Span) {
	d := ip.diags.Errorf(ip.src, sp, "E202", "%s", err.Error()).WithLabel("bad format")
	var unknown *unknownFormatError
	if errors.As(err, &unknown) {
		op, _, _ := strings.Cut(strings.TrimSpace(spec), " ")
		if help := pulp.FormatSuggestions("format", pulp.Suggest(op, unknown.Known)); help != "" {
			d.WithHelp("%s", help)
			return
		}
		d.WithHelp("Formats for a %s value: %s.", unknown.Kind, strings.Join(unknown.Known, ", "))
	}
}

// ---- text helpers ----

// findClose returns the index of the `}` closing the interpolation that starts
// at open, or -1. Quoted text is skipped so `{x ? "}" : "{"}` closes where the
// author meant it to.
func findClose(text string, open int) int {
	quote := byte(0)
	for i := open + 1; i < len(text); i++ {
		c := text[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '}':
			return i
		}
	}
	return -1
}

// indexOutside returns the index of the first occurrence of sub that is not
// inside quotes, or -1.
func indexOutside(s, sub string) int {
	quote := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			continue
		}
		if strings.HasPrefix(s[i:], sub) {
			return i
		}
	}
	return -1
}

func isQuoted(s string) bool {
	return len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0]
}

// literalText is a fallback or a literal as written: trimmed, and unwrapped if
// it was quoted. It is used as-is, never formatted — `{owner:upper|ANON}`
// applies `upper` to the owner, not to the fallback the author already typed
// in the case they wanted.
func literalText(s string) string {
	s = strings.TrimSpace(s)
	if isQuoted(s) {
		return s[1 : len(s)-1]
	}
	return s
}

// validPath reports whether s is a dotted variable path: segments that start
// with a letter or underscore and continue with letters, digits, `-` or `_`.
func validPath(s string) bool {
	if s == "" {
		return false
	}
	for _, segment := range strings.Split(s, ".") {
		if segment == "" {
			return false
		}
		for i := 0; i < len(segment); i++ {
			c := segment[i]
			switch {
			case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_':
			case i > 0 && (c >= '0' && c <= '9' || c == '-'):
			default:
				return false
			}
		}
	}
	return true
}

// valueEquals compares a value against a literal from a condition. Numbers
// compare numerically so `{loop.n == 1}` is true rather than "1" != "1.0";
// everything else compares as rendered text.
func valueEquals(v Value, literal string) bool {
	text := literalText(literal)
	if v.Kind == KindNumber {
		if n, err := strconv.ParseFloat(text, 64); err == nil {
			return v.Num == n
		}
	}
	if v.Kind == KindBool {
		switch strings.ToLower(text) {
		case "true", "yes", "on":
			return v.Bool
		case "false", "no", "off":
			return !v.Bool
		}
	}
	return v.String() == text
}
