// Validation: resolving names against the schema and reporting what is wrong.
//
// This is where a parsed-but-meaningless tree becomes a checked one, and it is
// the stage that produces most of the errors a user ever sees. The guiding rule
// is that an error should name the thing that is wrong, say why, and propose a
// fix — so every diagnostic here carries a span tight enough to underline the
// offending token, and a help line the reader can act on without opening the
// documentation.
package schema

import (
	"fmt"
	"strings"

	"github.com/jclement/treekillbot/internal/pulp"
)

// Validate walks a parsed document, resolving every node against the schema.
//
// It reports as many independent problems as it can rather than stopping at the
// first: someone who has just written thirty lines wants all of the typos at
// once. Errors that would make later checks meaningless — an unknown element,
// so its children have no context — suppress only that subtree.
func Validate(doc *pulp.Document) pulp.Diagnostics {
	var diags pulp.Diagnostics
	v := &validator{src: doc.Source, diags: &diags}
	for _, n := range doc.TopLevel() {
		v.node(n, context{})
	}
	diags.Sort()
	return diags
}

type validator struct {
	src   *pulp.Source
	diags *pulp.Diagnostics
}

// context is what the node currently being validated sits inside.
//
// It exists because a property means different things in different places. In
// `panel`, `border-width` must be a property of panels. In `defaults panel` it
// must also be a property of panels — but in a bare `defaults` or a `style`
// block it may be a property of anything, since the bundle does not yet know
// what it will be applied to. And in `vars` or `let` the names are not
// properties at all; they are whatever the author chose to call their
// variables.
type context struct {
	element string // the containing element's name, "" at the top level
	// anyElement is set inside `defaults` and `style`, where a property is
	// legal if it is legal on any element.
	anyElement bool
	// bindings is set inside `vars` and `let`, where children are variable
	// names and the schema has no opinion about them.
	bindings bool
}

// node validates one node in the context of whatever contains it.
func (v *validator) node(n *pulp.Node, ctx context) {
	// Inside a `vars` or `let` block every name is a variable the author
	// invented, so there is nothing here to check it against.
	if ctx.bindings {
		v.binding(n)
		return
	}
	if def, ok := Element(n.Name); ok {
		v.element(n, def)
		return
	}
	if id, ok := Lookup(n.Name); ok {
		v.property(n, id, ctx)
		return
	}
	v.unknownName(n, ctx)
}

// binding validates a variable declaration inside `vars` or `let`. A variable
// with no value is not an error: it declares the document's interface, to be
// filled by --var or the environment. Whether it actually got filled is the
// resolver's question, not the schema's.
func (v *validator) binding(n *pulp.Node) {
	if len(n.Children) == 0 {
		return
	}
	for _, c := range n.Children {
		if IsElement(c.Name) {
			v.diags.Errorf(v.src, c.NameSpan, "E124",
				"a variable cannot contain the element `%s`", c.Name).
				WithLabel("unexpected element").
				WithHelp("Variables hold a single value. Write `%s: <value>` on one line.", n.Name)
		}
	}
}

// element validates an element node and recurses into its children.
func (v *validator) element(n *pulp.Node, def *ElementDef) {
	switch {
	case def.Arg == ArgRequired && !n.HasArg:
		v.diags.Errorf(v.src, n.NameSpan, "E120", "`%s` needs an argument", n.Name).
			WithLabel("missing argument").
			WithHelp("%s", argHelp(def))
	case def.Arg == ArgNone && n.HasArg:
		v.diags.Errorf(v.src, n.ArgSpan, "E121", "`%s` does not take an argument", n.Name).
			WithLabel("unexpected argument").
			WithHelp("Write `%s` on its own, and put settings on the lines beneath it.", n.Name)
	}

	if !def.Container && len(n.Children) > 0 {
		// Properties beneath a leaf element are fine — `text` takes `align` —
		// so only complain about child *elements*.
		for _, c := range n.Children {
			if IsElement(c.Name) {
				v.diags.Errorf(v.src, c.NameSpan, "E122", "`%s` cannot contain `%s`", n.Name, c.Name).
					WithLabel("not allowed here").
					WithHelp("`%s` holds no other elements. %s", n.Name, def.Doc)
			}
		}
	}

	child := childContext(n, def)
	for _, c := range n.Children {
		v.node(c, child)
	}
	v.crossChecks(n, def)
}

// childContext returns the context an element's children are validated in.
func childContext(n *pulp.Node, def *ElementDef) context {
	switch def.Name {
	case "vars", "let":
		return context{bindings: true}
	case "style":
		return context{anyElement: true}
	case "defaults":
		// `defaults panel` narrows to one element type; a bare `defaults` does
		// not, and its contents are checked only for existing at all.
		if n.HasArg {
			if target, ok := Element(strings.Trim(n.Arg, `"`)); ok {
				return context{element: target.Name}
			}
		}
		return context{anyElement: true}
	case "for", "repeat":
		// A loop is transparent: its children sit wherever the loop sits.
		if n.Parent != nil {
			if pd, ok := Element(n.Parent.Name); ok {
				return context{element: pd.Name}
			}
		}
		return context{}
	}
	return context{element: def.Name}
}

// property validates a property node: that it belongs where it is written, and
// that its value is the right shape.
func (v *validator) property(n *pulp.Node, id PropID, ctx context) {
	def := Def(id)

	if ctx.element != "" && !ctx.anyElement && !AppliesTo(id, ctx.element) {
		d := v.diags.Errorf(v.src, n.NameSpan, "E102",
			"`%s` is not a property of `%s`", n.Name, ctx.element).
			WithLabel("not valid on `%s`", ctx.element)
		if where := appliesToList(def); where != "" {
			d.WithHelp("`%s` applies to %s.", n.Name, where)
		}
		return
	}

	if len(n.Children) > 0 {
		for _, c := range n.Children {
			if IsElement(c.Name) {
				v.diags.Errorf(v.src, c.NameSpan, "E123",
					"`%s` is a property, so it cannot contain the element `%s`", n.Name, c.Name).
					WithLabel("unexpected element").
					WithHelp("Did you mean to indent `%s` one level less, so it sits beside `%s` rather than inside it?", c.Name, n.Name)
			}
		}
	}

	if !n.HasArg {
		v.diags.Errorf(v.src, n.NameSpan, "E103", "`%s` needs a value", n.Name).
			WithLabel("no value given").
			WithHelp("`%s` takes %s. For example: `%s: %s`.", n.Name, def.Kind, n.Name, exampleFor(def))
		return
	}

	// A string property takes the rest of the line verbatim. Running the value
	// lexer over it would reject perfectly good text: `time-start: 7:00` would
	// be read as the number 7 with an unknown unit, and `text: Ratio: 3:1`
	// would lose its colons.
	if def.Kind == KindString {
		return
	}

	values := pulp.ParseValues(v.src, n.ArgSpan, n.Arg, v.diags)
	v.checkKind(n, id, def, values)
}

// checkKind verifies that parsed values match the property's declared type.
func (v *validator) checkKind(n *pulp.Node, id PropID, def *PropDef, values []pulp.Value) {
	if len(values) == 0 {
		return
	}
	// An un-substituted interpolation cannot be type-checked until variables
	// are resolved, and checking it again afterwards is the resolver's job.
	for _, val := range values {
		if val.Kind == pulp.KindInterp {
			return
		}
	}
	first := values[0]

	switch def.Kind {
	case KindLength:
		if first.Kind != pulp.KindLength {
			v.badLength(n, def, first)
		}
	case KindEdges:
		for _, val := range values {
			if val.Kind != pulp.KindLength {
				v.badLength(n, def, val)
				return
			}
		}
		if len(values) > 4 {
			v.diags.Errorf(v.src, values[4].Span, "E113", "`%s` takes at most four lengths", def.Name).
				WithLabel("too many values").
				WithHelp("One value sets all sides, two set vertical then horizontal, four set top, right, bottom, left.")
		}
	case KindSize:
		if !first.IsSize() {
			if first.Kind == pulp.KindNumber {
				v.badLength(n, def, first)
				return
			}
			v.wrongKind(def, first, "a length, a percentage, `fill` or `auto`")
		}
	case KindColor:
		if first.Kind != pulp.KindColor {
			v.badColor(def, first)
		}
	case KindBool:
		if first.Kind != pulp.KindBool {
			v.wrongKind(def, first, "`true` or `false`")
		}
	case KindNumber:
		if first.Kind != pulp.KindNumber && first.Kind != pulp.KindPercent {
			v.wrongKind(def, first, "a number")
		}
	case KindInteger:
		if first.Kind != pulp.KindNumber || first.Num != float64(int(first.Num)) {
			v.wrongKind(def, first, "a whole number")
		}
	case KindEnum:
		v.checkEnum(id, def, first)
	}
}

// badLength produces the bespoke message for the single most common mistake in
// this language: writing a bare number where a length belongs. A generic "wrong
// type" would be technically correct and useless; naming the missing unit and
// offering the conversion is what makes it actionable.
func (v *validator) badLength(n *pulp.Node, def *PropDef, val pulp.Value) {
	if val.Kind == pulp.KindNumber {
		v.diags.Errorf(v.src, val.Span, "E021",
			"`%s` needs a length, but `%s` has no unit", def.Name, val.Raw).
			WithLabel("add a unit").
			WithHelp("Add a unit: `%spt` (about %.2fin), `%smm`, or `%.2fin`.%s",
				val.Raw, val.Num/72, val.Raw, val.Num/72, sizeExtras(def))
		return
	}
	v.wrongKind(def, val, "a length such as `16pt`, `0.5in` or `12mm`")
}

func (v *validator) badColor(def *PropDef, val pulp.Value) {
	d := v.diags.Errorf(v.src, val.Span, "E110",
		"`%s` needs a colour, but found %s", def.Name, describe(val)).
		WithLabel("not a colour")
	if val.Kind == pulp.KindKeyword {
		if ss := pulp.Suggest(strings.ToLower(val.Str), pulp.NamedColorNames()); len(ss) > 0 {
			d.WithHelp("%s", pulp.FormatSuggestions("colour", ss))
			return
		}
	}
	d.WithHelp("Write a colour as `#ddd`, `gray(0.85)`, `rgb(31 111 235)` or a CSS colour name.")
}

func (v *validator) wrongKind(def *PropDef, val pulp.Value, want string) {
	v.diags.Errorf(v.src, val.Span, "E110",
		"`%s` needs %s, but found %s", def.Name, want, describe(val)).
		WithLabel("wrong kind of value")
}

func (v *validator) checkEnum(id PropID, def *PropDef, val pulp.Value) {
	got := strings.ToLower(val.Str)
	if val.Kind == pulp.KindNone {
		got = "none"
	}
	if val.Kind == pulp.KindAuto {
		got = "auto"
	}
	if got == "" {
		got = strings.ToLower(val.Raw)
	}
	for _, ok := range def.Enum {
		if got == ok {
			return
		}
	}
	if _, isAlias := CanonicalEnum(id, got); isAlias {
		return
	}
	d := v.diags.Errorf(v.src, val.Span, "E111",
		"`%s` is not a valid value for `%s`", val.Raw, def.Name).
		WithLabel("unknown value")
	if ss := pulp.Suggest(got, def.Enum); len(ss) > 0 {
		d.WithHelp("%s", pulp.FormatSuggestions("value", ss))
		return
	}
	d.WithHelp("Valid values are: %s.", strings.Join(quoteAll(def.Enum), ", "))
}

// unknownName handles a name that is neither an element nor a property. The
// suggestion pool is scoped to what would be legal here, so a typo inside a
// panel is compared against panel properties rather than against everything.
func (v *validator) unknownName(n *pulp.Node, ctx context) {
	pool := PropertyNames()
	if ctx.element != "" && !ctx.anyElement {
		pool = PropertyNamesFor(ctx.element)
	}
	pool = append(pool, ElementNames()...)

	kind := "property"
	if ctx.element == "" && !ctx.anyElement {
		kind = "element"
	}
	d := v.diags.Errorf(v.src, n.NameSpan, "E101", "unknown %s `%s`", kind, n.Name).
		WithLabel("unknown %s", kind)

	if ss := pulp.Suggest(n.Name, pool); len(ss) > 0 {
		d.WithHelp("%s", pulp.FormatSuggestions(kind, ss))
		return
	}
	// A name that exists but is not legal here is a different mistake from a
	// misspelling, and saying so saves the reader checking the spelling.
	if id, ok := Lookup(n.Name); ok {
		if where := appliesToList(Def(id)); where != "" {
			d.WithHelp("`%s` exists, but applies to %s.", n.Name, where)
			return
		}
	}
	d.WithHelp("Run `treekillbot docs props` for the full list.")
}

// crossChecks catches combinations that are individually valid but together
// mean the author expected something that will not happen.
func (v *validator) crossChecks(n *pulp.Node, def *ElementDef) {
	if !IsLayoutElement(def.Name) {
		return
	}

	// The line-height / line-pitch trap. Both names are real, so a document
	// setting line-height on a ruled panel and expecting the rules to move gets
	// no error at all — it silently renders with the default pitch. This is the
	// cost of keeping line-height's CSS meaning (DESIGN.md D6), and a warning is
	// how we pay it back.
	if lh := n.Child("line-height"); lh != nil {
		if ls := n.Child("line-style"); ls != nil && ls.Arg != "none" && n.Child("line-pitch") == nil {
			v.diags.Warnf(v.src, lh.NameSpan, "W030",
				"`line-height` sets text leading, not the spacing of ruled lines").
				WithLabel("did you mean `line-pitch`?").
				WithHelp("This `%s` has `line-style: %s` but no `line-pitch`, so its rules will use the default spacing. Set `line-pitch` to move them.",
					n.Name, ls.Arg)
		}
	}

	// A width on something that is not in a row has nothing to divide.
	if w := n.Child("width"); w != nil && def.Name == ESection {
		v.diags.Warnf(v.src, w.NameSpan, "W031",
			"`width` on a `section` has no effect").
			WithLabel("ignored").
			WithHelp("Sections always fill their parent's width. To divide space horizontally, put `column` nodes inside this section.")
	}
}

// ---- helpers ----

func describe(v pulp.Value) string {
	switch v.Kind {
	case pulp.KindNumber:
		return fmt.Sprintf("the number `%s`", v.Raw)
	case pulp.KindKeyword:
		return fmt.Sprintf("the word `%s`", v.Raw)
	case pulp.KindString:
		return fmt.Sprintf("the text %s", v.Raw)
	}
	return fmt.Sprintf("%s `%s`", v.Kind, v.Raw)
}

func appliesToList(def *PropDef) string {
	if len(def.AppliesTo) == 0 {
		return ""
	}
	return strings.Join(quoteAll(def.AppliesTo), ", ")
}

func quoteAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = "`" + s + "`"
	}
	return out
}

// sizeExtras adds a sentence about the flexible spellings when the property
// accepts them, so the help for `height` mentions fill and auto while the help
// for `font-size` — which accepts neither — does not mislead.
func sizeExtras(def *PropDef) string {
	if def.Kind == KindSize {
		return " This property also accepts a percentage, `fill` and `auto`."
	}
	return ""
}

func exampleFor(def *PropDef) string {
	if def.Default != "" {
		return def.Default
	}
	switch def.Kind {
	case KindLength:
		return "8pt"
	case KindSize:
		return "fill"
	case KindColor:
		return "gray(0.8)"
	case KindBool:
		return "true"
	case KindEnum:
		if len(def.Enum) > 0 {
			return def.Enum[0]
		}
	}
	return "…"
}

func argHelp(def *ElementDef) string {
	switch def.Name {
	case "style":
		return "Write `style <name>`, then the properties it bundles beneath it."
	case "for":
		return "Write `for <name> in <list>`, as in `for day in week.days`."
	case "repeat":
		return "Write `repeat <count>`."
	case "theme":
		return "Write `theme <name>`. Run `treekillbot themes` to see what is available."
	case "include":
		return "Write `include <path>`."
	case EImage:
		return "Write `image <path>`."
	}
	return "Give it a value on the same line."
}
