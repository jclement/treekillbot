// The property cascade.
//
// Turning a parse tree into resolved properties is the step where `defaults`,
// `style` bundles, inheritance and direct properties are reconciled. The order
// is stated in DESIGN.md section 3 and implemented in exactly one function,
// resolveProps, so there is one place to look when a property turns out not to
// be what you expected.
package compile

import (
	"strings"

	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/pulp"
	"github.com/jclement/treekillbot/internal/schema"
)

// cascadeContext carries the defaults in force for a subtree.
//
// It is copied on entry to any node that declares its own defaults, which is
// what makes "nearest wins" work: a nested `defaults` block layers over the
// enclosing one for its subtree and disappears when the subtree does.
type cascadeContext struct {
	global *schema.Props
	byType map[string]*schema.Props
}

func newCascadeContext() cascadeContext {
	return cascadeContext{global: schema.NewProps(), byType: map[string]*schema.Props{}}
}

// clone returns an independent copy so a subtree's defaults do not escape it.
func (c cascadeContext) clone() cascadeContext {
	out := cascadeContext{global: c.global.Clone(), byType: make(map[string]*schema.Props, len(c.byType))}
	for name, props := range c.byType {
		out.byType[name] = props.Clone()
	}
	return out
}

// forType returns the accumulated defaults for one element type.
func (c cascadeContext) forType(element string) *schema.Props {
	return c.byType[element]
}

// baseProps builds the root property set from the schema's built-in defaults.
//
// Defaults are written in Pulp source in the schema table and parsed here, once
// per process. That keeps them honest: anything expressible as a built-in
// default is expressible by a user, and `treekillbot docs props` can print the
// same string the engine actually uses.
func baseProps() *schema.Props {
	props := schema.NewProps()
	for _, id := range schema.AllPropIDs() {
		def := schema.Def(id)
		if def.Default == "" {
			continue
		}
		src := pulp.NewSource("<built-in defaults>", def.Default)
		var diags pulp.Diagnostics
		if def.Kind == schema.KindString {
			props.Set(id, []pulp.Value{{Kind: pulp.KindString, Str: def.Default, Raw: def.Default}})
			continue
		}
		values := pulp.ParseValues(src, pulp.Span{Start: 0, End: len(def.Default)}, def.Default, &diags)
		if diags.HasErrors() {
			// The schema's own defaults are covered by a test that parses every
			// one of them, so reaching here means the table was edited without
			// running it.
			panic("schema default does not parse: " + def.Name + " = " + def.Default)
		}
		props.Set(id, values)
	}
	return props
}

// resolveProps computes one node's properties.
//
// The order below is the cascade, lowest priority first. It is worth reading
// against DESIGN.md section 3, because the one place it departs from CSS —
// inheritance beating `defaults` — is invisible here and lives in
// Props.OverrideInherited.
func (c *Compiler) resolveProps(node *pulp.Node, element string, ctx cascadeContext, parent *schema.Props) *schema.Props {
	props := schema.NewProps()

	// 1-2. Built-in defaults, then whatever `defaults` blocks are in force.
	props.MergeFrom(c.base)
	props.MergeFrom(ctx.global)
	// A `defaults <element>` block is the author naming this element and saying
	// something about it, so its values count as explicit and propagate to the
	// element's contents the way a property written on the node would.
	//
	// They did not, and the block therefore did half its job: `defaults panel {
	// font-size: 14pt }` set the panel's own font and the text inside it fell
	// back to the document-wide default, because inheritance carried only
	// explicit values and this was not one. The global `defaults` block is
	// deliberately still not explicit — that is the whole distinction, and D8
	// turns on it.
	if typed := ctx.forType(element); typed != nil {
		props.MergeAsExplicit(typed)
	}

	// 3. Inherited values from the nearest ancestor that stated them.
	props.OverrideInherited(parent)

	// 4. Named style bundles, in the order they were listed.
	for _, name := range c.styleNames(node) {
		bundle, ok := c.styles[name]
		if !ok {
			c.unknownStyle(node, name)
			continue
		}
		props.MergeAsExplicit(bundle)
	}

	// 5. The positional argument, which is shorthand for one property.
	if def, ok := schema.Element(element); ok && node.HasArg && def.ArgProp != schema.PInvalid {
		if values, ok := c.propertyValues(node, def.ArgProp, node.Arg, node.ArgSpan); ok {
			props.SetExplicit(def.ArgProp, values)
		}
	}

	// 6. Properties written directly on the node.
	c.applyDirectProperties(node, props)

	// 7. Resolve em and ex, which the value parser deliberately left pending
	//    because they depend on a font size the cascade had not chosen yet.
	resolveRelativeLengths(props, parent)
	return props
}

// exPerEm is what an `ex` resolves to as a fraction of the font size.
//
// Properly, `ex` is the face's x-height, which varies: IBM Plex Mono sits at
// 0.516em, Serif at 0.520em. Resolving it exactly would make the property
// cascade depend on font loading, which would drag the font registry into a
// stage that is otherwise pure and testable without it. Half an em is within 2%
// of every face we ship, and the alternative on offer was leaving `ex` silently
// resolving to zero.
const exPerEm = 0.5

// resolveRelativeLengths turns em- and ex-relative lengths into absolute ones.
//
// Font size is resolved first and against the PARENT's size, because `font-size:
// 1.2em` means "1.2 times the inherited size" — resolving it against itself
// would be circular. Everything else then resolves against this node's own
// resolved size, which is what `padding: 0.5em` means.
func resolveRelativeLengths(props *schema.Props, parent *schema.Props) {
	fontSize := resolveFontSize(props, parent)
	if fontSize <= 0 {
		return
	}
	for _, id := range props.SetIDs() {
		if id == schema.PFontSize {
			continue
		}
		values := props.Values(id)
		resolved, changed := absoluteLengths(values, fontSize)
		if changed {
			props.Set(id, resolved)
		}
	}
}

// resolveFontSize returns the node's absolute font size, resolving an
// em-relative one against the parent's.
func resolveFontSize(props *schema.Props, parent *schema.Props) geom.Tick {
	inherited := geom.Pt(9)
	if parent != nil {
		if size := parent.Tick(schema.PFontSize, 0); size > 0 {
			inherited = size
		}
	}
	value, ok := props.First(schema.PFontSize)
	if !ok {
		return inherited
	}
	if value.Kind == pulp.KindLength && value.Relative {
		absolute := scaleBy(inherited, value.Num, value.Unit)
		value.Length, value.Relative = absolute, false
		props.Set(schema.PFontSize, []pulp.Value{value})
		return absolute
	}
	if value.Kind == pulp.KindLength && value.Length > 0 {
		return value.Length
	}
	return inherited
}

// absoluteLengths replaces every pending relative length in a value list.
func absoluteLengths(values []pulp.Value, fontSize geom.Tick) ([]pulp.Value, bool) {
	changed := false
	out := make([]pulp.Value, len(values))
	copy(out, values)
	for i, value := range out {
		if value.Kind != pulp.KindLength || !value.Relative {
			continue
		}
		out[i].Length = scaleBy(fontSize, value.Num, value.Unit)
		out[i].Relative = false
		changed = true
	}
	if !changed {
		return values, false
	}
	return out, true
}

// scaleBy multiplies a font size by a relative magnitude.
func scaleBy(fontSize geom.Tick, magnitude float64, unit string) geom.Tick {
	if unit == "ex" {
		magnitude *= exPerEm
	}
	return geom.Tick(float64(fontSize)*magnitude + 0.5)
}

// applyDirectProperties sets every property child of a node.
func (c *Compiler) applyDirectProperties(node *pulp.Node, props *schema.Props) {
	for _, child := range node.Children {
		if schema.IsElement(child.Name) {
			continue
		}
		id, ok := schema.Lookup(child.Name)
		if !ok || id == schema.PStyle {
			continue // unknown names and `style` are handled elsewhere
		}
		if id == schema.PBorder {
			c.applyBorderShorthand(child, props)
			continue
		}
		if values, ok := c.propertyValues(child, id, child.Arg, child.ArgSpan); ok {
			props.SetExplicit(id, values)
		}
	}
}

// propertyValues interpolates and parses one property's argument.
func (c *Compiler) propertyValues(node *pulp.Node, id schema.PropID, arg string, span pulp.Span) ([]pulp.Value, bool) {
	if isRawQuoted(arg) {
		raw := unquote(strings.TrimSpace(arg))
		return []pulp.Value{{Kind: pulp.KindString, Str: raw, Raw: arg, Span: span}}, true
	}
	text := c.scope.Interpolate(arg, span, c.src, c.diags)
	switch schema.Def(id).Kind {
	case schema.KindString, schema.KindPageSize:
		// These take the line verbatim. Running the value lexer over a string
		// would mangle text that merely looks like a value, and over a page
		// size it would read `4x6` as the number 4 with an unknown unit.
		return []pulp.Value{{Kind: pulp.KindString, Str: unquote(text), Raw: text, Span: span}}, true
	}
	values := pulp.ParseValues(c.src, span, text, c.diags)
	return values, len(values) > 0
}

// isRawQuoted reports whether text is wrapped in single quotes, which the
// language defines as fully raw: no escapes and no interpolation.
//
// It has to be checked BEFORE interpolation, not after. Substituting first and
// unquoting second means `text '{today}'` expands anyway, which defeats the one
// escape hatch the language offers for text that is mostly braces.
func isRawQuoted(text string) bool {
	trimmed := strings.TrimSpace(text)
	return len(trimmed) >= 2 && trimmed[0] == '\'' && trimmed[len(trimmed)-1] == '\''
}

// unquote strips one layer of matching surrounding quotes.
//
// A string property normally takes its line verbatim, which is what makes
// `text: Ratio: 3:1` work. But `panel "Notes"` is the same property written as a
// positional argument, and there the quotes are punctuation rather than content.
// Stripping only a fully-enclosing pair keeps both readings correct.
func unquote(text string) string {
	if len(text) < 2 {
		return text
	}
	quote := text[0]
	if quote != '"' && quote != '\'' {
		return text
	}
	if text[len(text)-1] != quote {
		return text
	}
	body := text[1 : len(text)-1]
	// A quote in the middle means these were not a matching enclosing pair, as
	// in `"a" and "b"`, so the text is left alone.
	if strings.IndexByte(body, quote) >= 0 && quote == '\'' {
		return text
	}
	if quote == '\'' {
		return body
	}
	return unescapeString(body)
}

// unescapeString resolves the escapes legal inside a double-quoted string.
func unescapeString(s string) string {
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
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// applyBorderShorthand expands `border: 0.5pt solid gray(0.7)` into the three
// properties it stands for. Order is free: a length is the width, a colour is
// the colour, and a keyword is the style, so nobody has to remember an order.
func (c *Compiler) applyBorderShorthand(node *pulp.Node, props *schema.Props) {
	text := c.scope.Interpolate(node.Arg, node.ArgSpan, c.src, c.diags)
	values := pulp.ParseValues(c.src, node.ArgSpan, text, c.diags)
	for _, v := range values {
		switch {
		case v.Kind == pulp.KindLength:
			props.SetExplicit(schema.PBorderWidth, []pulp.Value{v})
		case v.Kind == pulp.KindColor:
			props.SetExplicit(schema.PBorderColor, []pulp.Value{v})
		case v.Kind == pulp.KindNone:
			props.SetExplicit(schema.PBorderStyle, []pulp.Value{v})
		case v.Kind == pulp.KindKeyword:
			props.SetExplicit(schema.PBorderStyle, []pulp.Value{v})
		}
	}
}

// styleNames returns the style bundles a node asks for, in order.
func (c *Compiler) styleNames(node *pulp.Node) []string {
	child := node.Child("style")
	if child == nil || !child.HasArg {
		return nil
	}
	text := c.scope.Interpolate(child.Arg, child.ArgSpan, c.src, c.diags)
	return strings.Fields(strings.ReplaceAll(text, ",", " "))
}

// unknownStyle reports a reference to a style that was never defined, with a
// suggestion drawn from the styles that do exist.
func (c *Compiler) unknownStyle(node *pulp.Node, name string) {
	span := node.NameSpan
	if child := node.Child("style"); child != nil {
		span = child.ArgSpan
	}
	d := c.diags.Errorf(c.src, span, "E130", "unknown style %q", name).
		WithLabel("not defined")
	if len(c.styleOrder) == 0 {
		d.WithHelp("Define it with a `style %s` block before using it.", name)
		return
	}
	if ss := pulp.Suggest(name, c.styleOrder); len(ss) > 0 {
		d.WithHelp("%s", pulp.FormatSuggestions("style", ss))
		return
	}
	d.WithHelp("Defined styles are: %s.", strings.Join(c.styleOrder, ", "))
}
