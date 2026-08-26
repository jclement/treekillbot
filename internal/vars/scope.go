// The scope stack: what `{name}` resolves to, and in what order.
//
// Two independent mechanisms, because they answer different questions.
//
// **Layers** rank the sources a document-wide name can come from. Lowest to
// highest: built-ins, theme constants, environment, the `vars` block,
// `--vars-file`, `--var`. A Define at a lower layer never displaces a higher
// one, so the order the compiler happens to process things in does not matter.
//
// **Frames** are lexical scope. `Push`/`Pop` bracket a subtree, and anything
// bound inside — a `let`, a `for` binding — is visible to that subtree only and
// beats every layer. This is why a `let` in one section does not leak to its
// siblings and why an inner `for day in …` shadows an outer one.
//
// Environment variables are declared, never ambient (DESIGN.md D11). `env.HOME`
// resolves only if the document said it wanted HOME, and an undeclared one is
// refused rather than returning empty — a `.pulp` file is a document you might
// receive, and ambient expansion makes `text: "{env.AWS_SECRET_ACCESS_KEY}"` an
// exfiltration primitive in a shared planner template.
package vars

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jclement/treekillbot/internal/pulp"
)

// Layer ranks the document-wide sources of a variable. Higher wins; lexical
// bindings (Bind) sit above all of them.
type Layer uint8

const (
	// LayerBuiltin is the date namespace derived from the anchor.
	LayerBuiltin Layer = iota
	// LayerTheme is a constant defined by a .pulptheme.
	LayerTheme
	// LayerEnv is a value that came from TKB_VAR_<NAME>.
	LayerEnv
	// LayerDocument is the document's own `vars` block.
	LayerDocument
	// LayerVarsFile is --vars-file.
	LayerVarsFile
	// LayerCLI is --var, which beats everything a file can say.
	LayerCLI
	// LayerLexical is a `let` or loop binding. Never passed to Define; it is
	// what Bind records, and what Params reports for a lexical override.
	LayerLexical
)

// String names the layer as it appears in `check --json` and in diagnostics.
func (l Layer) String() string {
	switch l {
	case LayerTheme:
		return "theme"
	case LayerEnv:
		return "environment"
	case LayerDocument:
		return "vars"
	case LayerVarsFile:
		return "vars-file"
	case LayerCLI:
		return "--var"
	case LayerLexical:
		return "let"
	}
	return "built-in"
}

// DocInfo is what the `doc` namespace reports about the file being compiled.
type DocInfo struct {
	Name string // as the user typed it, or "<stdin>"
	Path string
}

// Options configures a Scope. The zero value is usable: Monday weeks, English
// names, undefined variables are errors, and the environment is read through
// os.LookupEnv.
type Options struct {
	// WeekStart rotates the displayed week. It never moves the ISO week number.
	// The zero value is time.Sunday, which is *not* the default — use
	// Options.weekStart(), which maps the zero value to Monday.
	WeekStart time.Weekday
	// weekStartSet distinguishes "Sunday was asked for" from "nothing was set".
	weekStartSet bool

	// AllowUndefined downgrades an unresolved reference from an error to a
	// warning that renders empty. This backs --allow-undefined; CI does not
	// use it.
	AllowUndefined bool

	// UnsafeEnv permits arbitrary `env.*` without a declaration. This backs
	// --unsafe-env, and exists so the refusal has an escape hatch that is
	// visible in the command line rather than invisible in the document.
	UnsafeEnv bool

	// Environ reads the process environment. Injected so tests are hermetic;
	// nil means os.LookupEnv.
	Environ func(string) (string, bool)

	// Doc fills the `doc` namespace.
	Doc DocInfo
}

// WithWeekStart returns a copy of the options with an explicit week start, so
// that asking for Sunday is distinguishable from not asking.
func (o Options) WithWeekStart(w time.Weekday) Options {
	o.WeekStart = w
	o.weekStartSet = true
	return o
}

func (o Options) weekStart() time.Weekday {
	if !o.weekStartSet && o.WeekStart == time.Sunday {
		return time.Monday
	}
	return o.WeekStart
}

func (o Options) lookupEnv(name string) (string, bool) {
	if o.Environ != nil {
		return o.Environ(name)
	}
	return os.LookupEnv(name)
}

// binding is one document-wide value plus the layer that supplied it.
type binding struct {
	val   Value
	layer Layer
}

// Param is a variable the document declared without a value: a required input.
// It is what `check --json` reports as the document's interface.
type Param struct {
	Name string
	// From is where the value came from once resolved. An unsatisfied
	// parameter is a hard error, so a Param in the list has always been filled.
	From  Layer
	Value string
}

// Use is one variable reference the document made.
type Use struct {
	Name     string // the dotted path as written
	Resolved bool
}

// Scope resolves variable references for one document.
//
// It is not safe for concurrent use; one document is compiled by one
// goroutine, and adding a mutex would only hide a bug where that stops being
// true.
type Scope struct {
	anchor   time.Time
	opts     Options
	names    *nameTable
	builtins Value

	globals map[string]binding
	frames  []map[string]Value

	// page is filled in late, at render time, once pagination is known.
	page      Value
	pageBound bool

	permittedEnv map[string]bool
	params       []Param
	used         map[string]bool
}

// NewScope builds a scope whose date built-ins all derive from anchor.
//
// The anchor is passed in rather than read from the clock: that single
// parameter is what makes `--date`, `--deterministic` and every test in this
// package the same code path.
func NewScope(anchor time.Time, opts Options) *Scope {
	s := &Scope{
		anchor:       anchor,
		opts:         opts,
		names:        defaultNames(),
		globals:      map[string]binding{},
		permittedEnv: map[string]bool{},
		used:         map[string]bool{},
	}
	s.rebuild()
	return s
}

// rebuild recomputes the built-in namespace. Called at construction and again
// whenever the name tables are overridden, since month and day names are baked
// into every day item.
func (s *Scope) rebuild() {
	s.builtins = buildBuiltins(s.anchor, s.opts.weekStart(), s.names, s.opts.Doc)
}

// Anchor returns the time every date built-in derives from.
func (s *Scope) Anchor() time.Time { return s.anchor }

// ParseAnchor parses the value of `--date`.
//
// A bare date is midnight local, because a planner page is about a day and
// "midnight local" is the only reading that keeps `today` equal to what the
// user typed. RFC 3339 is accepted for the rare case where the time of day
// matters (`{now:time}` on a dated draft).
func ParseAnchor(text string) (time.Time, error) {
	text = strings.TrimSpace(text)
	for _, layout := range []string{"2006-01-02", "2006-01-02T15:04:05", time.RFC3339} {
		if t, err := time.ParseInLocation(layout, text, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("%q is not a date; write it as 2026-08-24", text)
}

// SetMonthNames overrides the twelve month names, January first. Passing nil
// short names derives them from the first three characters of each long name.
//
// This is the locale hook from DESIGN.md D12: locale is out of scope, but a
// French planner should be a few lines of document rather than a code change.
func (s *Scope) SetMonthNames(long, short []string) error {
	if len(long) != 12 {
		return fmt.Errorf("month-names needs 12 names, got %d", len(long))
	}
	if short == nil {
		short = shorten(long)
	}
	if len(short) != 12 {
		return fmt.Errorf("month-names short form needs 12 names, got %d", len(short))
	}
	s.names.months = append([]string{}, long...)
	s.names.shortMonths = append([]string{}, short...)
	s.rebuild()
	return nil
}

// SetDayNames overrides the seven day names, **Sunday first** to match
// time.Weekday. Passing nil short names derives them as SetMonthNames does.
func (s *Scope) SetDayNames(long, short []string) error {
	if len(long) != 7 {
		return fmt.Errorf("day-names needs 7 names, got %d", len(long))
	}
	if short == nil {
		short = shorten(long)
	}
	if len(short) != 7 {
		return fmt.Errorf("day-names short form needs 7 names, got %d", len(short))
	}
	s.names.days = append([]string{}, long...)
	s.names.shortDays = append([]string{}, short...)
	s.rebuild()
	return nil
}

// ---- binding ----

// Push opens a lexical scope. Every Push must be matched by a Pop.
func (s *Scope) Push() { s.frames = append(s.frames, nil) }

// Pop closes the innermost lexical scope, discarding its bindings.
func (s *Scope) Pop() {
	if len(s.frames) == 0 {
		panic("vars: Pop without a matching Push")
	}
	s.frames = s.frames[:len(s.frames)-1]
}

// Depth reports how many lexical scopes are open, so a compiler can assert it
// unwound cleanly.
func (s *Scope) Depth() int { return len(s.frames) }

// Bind binds a name in the innermost lexical scope: a `let`, or a loop
// variable. It shadows every layer and every outer frame, and disappears at
// Pop.
func (s *Scope) Bind(name string, v Value) {
	if len(s.frames) == 0 {
		s.Push()
	}
	frame := s.frames[len(s.frames)-1]
	if frame == nil {
		frame = map[string]Value{}
		s.frames[len(s.frames)-1] = frame
	}
	frame[name] = v
}

// Define binds a document-wide name at a layer.
//
// A definition at a lower layer than the one already holding the name is
// discarded, so the compiler may process `--var`, the vars file and the `vars`
// block in whatever order is convenient and still get the documented
// precedence.
func (s *Scope) Define(name string, v Value, layer Layer) {
	if existing, ok := s.globals[name]; ok && existing.layer > layer {
		return
	}
	s.globals[name] = binding{val: v, layer: layer}
}

// PermitEnv allows `env.<name>` to be read.
//
// Nothing permits itself: this is the only way an environment variable becomes
// readable, short of --unsafe-env. Declare calls it for the name it declares.
func (s *Scope) PermitEnv(name string) { s.permittedEnv[name] = true }

// Declare registers a `vars` entry written without a value: a required
// parameter of the document.
//
// It is filled from `--var` or `--vars-file` if either supplied it, otherwise
// from `TKB_VAR_<NAME>` — uppercased, with dashes as underscores. If it is
// still unset that is a hard error naming what to pass, because the
// alternative is a silent blank on a printed form (DESIGN.md D11).
//
// Declaring a name also permits `env.<name>`.
func (s *Scope) Declare(name string, span pulp.Span, src *pulp.Source, diags *pulp.Diagnostics) {
	s.PermitEnv(name)

	if b, ok := s.globals[name]; ok {
		s.params = append(s.params, Param{Name: name, From: b.layer, Value: b.val.String()})
		return
	}

	envName := EnvVarName(name)
	if text, ok := s.opts.lookupEnv(envName); ok {
		s.Define(name, NewString(text), LayerEnv)
		s.params = append(s.params, Param{Name: name, From: LayerEnv, Value: text})
		return
	}

	diags.Errorf(src, span, "E211", "required variable `%s` was not provided", name).
		WithLabel("declared with no value").
		WithHelp("Pass `--var %s=…` or set %s in the environment.", name, envName)
}

// EnvVarName maps a declared variable name to the environment variable that
// fills it: `owner-name` → `TKB_VAR_OWNER_NAME`.
func EnvVarName(name string) string {
	return "TKB_VAR_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
}

// BindPage supplies the page numbers, which are only known once pagination has
// run. Until it is called, `page.number` and `page.count` resolve as deferred
// rather than as user errors — see Interpolate.
func (s *Scope) BindPage(number, count int) {
	s.page = NewRecord(
		Member{"number", NewNumber(float64(number))},
		Member{"count", NewNumber(float64(count))},
	)
	s.pageBound = true
}

// PageBound reports whether page numbers have been supplied.
func (s *Scope) PageBound() bool { return s.pageBound }

// pageMembers is the shape of the `page` namespace before it is bound, used to
// tell a deferred `page.count` from a misspelled `page.cuont`.
var pageMembers = []string{"number", "count"}

// Format renders a value with a format spec — the text that would follow the
// `:` in an interpolation — against this scope's month and day names.
func (s *Scope) Format(v Value, spec string) (string, error) {
	return formatValue(v, spec, s.names)
}

// ---- resolution ----

// resolveStatus is why a path did or did not resolve.
type resolveStatus uint8

const (
	resolveFound resolveStatus = iota
	// resolveMissingRoot: nothing in scope has that name.
	resolveMissingRoot
	// resolveMissingMember: the root resolved but has no such member.
	resolveMissingMember
	// resolveDeferred: a real `page` member that is not bound yet.
	resolveDeferred
	// resolveEnvUndeclared: `env.X` where X was never declared.
	resolveEnvUndeclared
	// resolveEnvUnset: `env.X` declared, but absent from the environment.
	resolveEnvUnset
)

// resolution is the detail Interpolate needs to write a good diagnostic:
// which segment failed, and what names were available at that point.
type resolution struct {
	val        Value
	status     resolveStatus
	root       string
	member     string   // the segment that failed
	candidates []string // names that were in scope where it failed
}

// Lookup resolves a dotted path, reporting whether it resolved.
func (s *Scope) Lookup(path string) (Value, bool) {
	r := s.resolve(path)
	return r.val, r.status == resolveFound
}

// List resolves a path to an iterable list — `week.days`, `month.weeks`, a
// `--var` bound to a list. It reports false for anything that is not a list, so
// `for day in today` is a compile error rather than a one-item loop.
func (s *Scope) List(path string) ([]Value, bool) {
	v, ok := s.Lookup(path)
	if !ok || v.Kind != KindList {
		return nil, false
	}
	return v.List, true
}

// resolve walks a dotted path, recording the reference in the consumed set.
func (s *Scope) resolve(path string) resolution {
	r := s.resolveUnrecorded(path)
	s.used[path] = s.used[path] || r.status == resolveFound
	return r
}

func (s *Scope) resolveUnrecorded(path string) resolution {
	root, rest, _ := strings.Cut(path, ".")
	if root == "env" {
		return s.resolveEnv(rest)
	}

	val, ok := s.root(root)
	if !ok {
		if root == "page" {
			return s.resolvePage(rest)
		}
		return resolution{status: resolveMissingRoot, root: root, member: root, candidates: s.Names()}
	}

	for rest != "" {
		var member string
		member, rest, _ = strings.Cut(rest, ".")
		next, ok := val.Field(member)
		if !ok {
			return resolution{
				status:     resolveMissingMember,
				root:       root,
				member:     member,
				candidates: val.FieldNames(),
			}
		}
		val = next
	}
	return resolution{val: val, status: resolveFound, root: root}
}

// root resolves the first segment: lexical frames innermost outward, then the
// document-wide layers, then the built-ins.
func (s *Scope) root(name string) (Value, bool) {
	for i := len(s.frames) - 1; i >= 0; i-- {
		if v, ok := s.frames[i][name]; ok {
			return v, true
		}
	}
	if b, ok := s.globals[name]; ok {
		return b.val, true
	}
	if s.pageBound && name == "page" {
		return s.page, true
	}
	return s.builtins.Field(name)
}

// resolvePage answers a `page.*` reference made before pagination has run. The
// member name is still checked, so a typo is caught in the early pass and only
// the *value* is deferred.
func (s *Scope) resolvePage(member string) resolution {
	for _, known := range pageMembers {
		if member == known {
			return resolution{status: resolveDeferred, root: "page", member: member}
		}
	}
	return resolution{status: resolveMissingMember, root: "page", member: member, candidates: pageMembers}
}

// resolveEnv answers `env.NAME`, refusing anything the document did not
// declare.
func (s *Scope) resolveEnv(name string) resolution {
	if name == "" || strings.Contains(name, ".") {
		return resolution{status: resolveMissingMember, root: "env", member: name}
	}
	if !s.opts.UnsafeEnv && !s.permittedEnv[name] {
		return resolution{status: resolveEnvUndeclared, root: "env", member: name}
	}
	text, ok := s.opts.lookupEnv(name)
	if !ok {
		return resolution{status: resolveEnvUnset, root: "env", member: name}
	}
	return resolution{val: NewString(text), status: resolveFound, root: "env"}
}

// Names returns every name resolvable as the first segment of a path, sorted.
// It is what unknown-variable suggestions are ranked against, so it must
// include the lexical bindings that are open right now.
func (s *Scope) Names() []string {
	seen := map[string]bool{"env": true, "page": true}
	for _, frame := range s.frames {
		for name := range frame {
			seen[name] = true
		}
	}
	for name := range s.globals {
		seen[name] = true
	}
	for _, name := range s.builtins.FieldNames() {
		seen[name] = true
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Consumed returns every variable path the document referenced, sorted. This is
// the document's interface as `check --json` reports it: what it reads, and
// whether the reference resolved.
func (s *Scope) Consumed() []Use {
	out := make([]Use, 0, len(s.used))
	for name, resolved := range s.used {
		out = append(out, Use{Name: name, Resolved: resolved})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Params returns the variables the document declared without a value — its
// required inputs — in declaration order, each with the source that filled it.
func (s *Scope) Params() []Param { return s.params }
