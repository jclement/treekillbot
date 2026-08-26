// Turning a parsed document into a layout tree.
//
// This stage is where the document stops being text and becomes a page: loops
// expand into real nodes, `when` drops what it drops, variables are
// substituted, and every node gets a fully resolved property set. Everything
// downstream sees an ordinary tree with no directives left in it, which is why
// the layout engine has no idea that loops exist.
package compile

import (
	"sort"
	"strconv"
	"strings"

	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/layout"
	"github.com/jclement/treekillbot/internal/pulp"
	"github.com/jclement/treekillbot/internal/schema"
	"github.com/jclement/treekillbot/internal/vars"
)

// ThemeLayer is a theme's contribution to the cascade.
//
// It is not a single property bag, because a theme needs to say different
// things to different elements. "Panels have a hairline border" is the most
// ordinary thing a theme can want, and a flat bag can only express it as
// "everything has a hairline border", which frames every section and column on
// the page. Carrying the same three shapes the cascade already understands —
// global defaults, per-element defaults, and named styles — means a theme is
// written in exactly the language a document is, which is the point of D13.
type ThemeLayer struct {
	Global *schema.Props
	ByType map[string]*schema.Props
	Styles map[string]*schema.Props

	// Constants are the theme's `vars` block: named values a document can
	// reference as {ink}, {paper}, {accent} and so on.
	//
	// Without these, a document that names any colour cannot survive a theme
	// swap — it will keep its dark ink on a dark theme's dark sheet and become
	// unreadable. Every shipped theme defines the same small vocabulary, so
	// `color: {ink}` means the right thing under all of them.
	Constants map[string]string
}

// Options configures a compilation.
type Options struct {
	// Scope resolves variables. NullScope disables substitution, which is what
	// `fmt` and a syntax-only `check` want.
	Scope Scope
	// Theme is applied beneath the document's own defaults.
	Theme *ThemeLayer
	// PageSize overrides the document's declared page size, for --page-size.
	PageSize *PageSize
}

// PageSize is a trim size in ticks.
type PageSize struct {
	Width, Height geom.Tick
	Name          string
}

// Result is a compiled document ready to be laid out.
type Result struct {
	// Page is the trim rectangle at its origin.
	Page PageSize
	// Margin is the page margin, subtracted from the trim to give the content
	// area. It is kept separate from the root node's padding so that
	// `--margin` can override it without disturbing the document.
	Margin geom.Edges
	// Root is the page node, whose children are the document's sections.
	Root *layout.Node
	// WeekStart is the resolved first day of the week, for the date built-ins.
	WeekStart string
}

// Compiler holds the state of one compilation.
type Compiler struct {
	src   *pulp.Source
	diags *pulp.Diagnostics
	scope Scope

	base   *schema.Props
	theme  *ThemeLayer
	styles map[string]*schema.Props
	// styleOrder preserves declaration order for suggestions, because map
	// iteration order must never reach the user.
	styleOrder []string
}

// Compile builds a layout tree from a parsed document.
//
// It returns a result even when diagnostics contain errors, so that a caller
// which wants to report several problems — or to render a best-effort preview
// while the author is still typing — can keep going.
func Compile(doc *pulp.Document, opts Options) (*Result, pulp.Diagnostics) {
	var diags pulp.Diagnostics
	scope := opts.Scope
	if scope == nil {
		scope = NullScope()
	}
	c := &Compiler{
		src:    doc.Source,
		diags:  &diags,
		scope:  scope,
		base:   baseProps(),
		theme:  opts.Theme,
		styles: map[string]*schema.Props{},
	}

	ctx := newCascadeContext()
	c.applyTheme(ctx)

	// Directives first: a `style` used before its definition should still work,
	// because a document is a description rather than a program.
	c.collectDirectives(doc.TopLevel(), ctx)

	result := &Result{Root: layout.NewNode(layout.KindPage)}
	pageProps := c.pageSetup(doc, ctx, result, opts)
	result.Root.Props = pageProps
	result.Root.Source = c.src

	// Both shapes are legal and both appear in the wild: settings in a `page`
	// block with the content as its siblings, or the content nested inside the
	// `page` block. Compiling the page node's own layout children is what makes
	// the second one work; without it they are silently dropped, which is the
	// worst possible outcome for a document that parses cleanly.
	for _, node := range doc.TopLevel() {
		if node.Name == schema.EPage {
			for _, child := range node.Children {
				if !isContentNode(child.Name) {
					continue
				}
				for _, built := range c.buildNode(child, ctx, pageProps) {
					result.Root.Append(built)
				}
			}
			continue
		}
		if !isContentNode(node.Name) {
			continue
		}
		for _, built := range c.buildNode(node, ctx, pageProps) {
			result.Root.Append(built)
		}
	}

	diags.Sort()
	return result, diags
}

// applyTheme seeds the cascade with a theme, beneath everything the document
// says. Styles a theme defines are registered first so a document may reference
// them, and may also replace them by defining one of the same name.
func (c *Compiler) applyTheme(ctx cascadeContext) {
	if c.theme == nil {
		return
	}
	if c.theme.Global != nil {
		ctx.global.MergeFrom(c.theme.Global)
	}
	for _, name := range sortedKeys(c.theme.ByType) {
		target, ok := ctx.byType[name]
		if !ok {
			target = schema.NewProps()
			ctx.byType[name] = target
		}
		target.MergeFrom(c.theme.ByType[name])
	}
	for _, name := range sortedKeys(c.theme.Styles) {
		if _, exists := c.styles[name]; !exists {
			c.styleOrder = append(c.styleOrder, name)
		}
		c.styles[name] = c.theme.Styles[name]
	}
}

// sortedKeys returns a map's keys in a fixed order. Every walk over a map in
// this package goes through here, because map order reaching the output is the
// one thing DESIGN.md section 4 will not tolerate.
func sortedKeys(m map[string]*schema.Props) []string {
	out := make([]string, 0, len(m))
	for name := range m {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// CompileTheme reads a theme document into a ThemeLayer.
//
// A theme is a Pulp file, so this reuses the same directive collection a
// document goes through rather than a parallel reader — which is what stops a
// theme and a document from ever disagreeing about what `defaults panel` means.
func CompileTheme(doc *pulp.Document) (*ThemeLayer, pulp.Diagnostics) {
	var diags pulp.Diagnostics
	c := &Compiler{
		src:    doc.Source,
		diags:  &diags,
		scope:  NullScope(),
		base:   schema.NewProps(), // a theme contributes only what it states
		styles: map[string]*schema.Props{},
	}
	ctx := newCascadeContext()
	c.collectDirectives(doc.TopLevel(), ctx)
	diags.Sort()
	return &ThemeLayer{
		Global:    ctx.global,
		ByType:    ctx.byType,
		Styles:    c.styles,
		Constants: themeConstants(doc),
	}, diags
}

// themeConstants reads a theme's `vars` block.
//
// The values are kept as source text rather than resolved here, because the
// scope that will resolve them does not exist yet — and because a constant is
// substituted into a document's property, which then goes through the same
// value parser everything else does.
func themeConstants(doc *pulp.Document) map[string]string {
	out := map[string]string{}
	for _, node := range doc.TopLevel() {
		if node.Name != "vars" {
			continue
		}
		for _, child := range node.Children {
			if !child.HasArg {
				continue
			}
			out[child.Name] = unquote(child.Arg)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// IsEmpty reports whether a theme contributes nothing at all, which is almost
// always a truncated or misread file rather than an intention.
func (t *ThemeLayer) IsEmpty() bool {
	if t == nil {
		return true
	}
	if t.Global != nil && len(t.Global.SetIDs()) > 0 {
		return false
	}
	return len(t.ByType) == 0 && len(t.Styles) == 0 && len(t.Constants) == 0
}

// collectDirectives gathers `vars`, `defaults` and `style` blocks at one level
// into the cascade context.
func (c *Compiler) collectDirectives(nodes []*pulp.Node, ctx cascadeContext) {
	for _, node := range nodes {
		switch node.Name {
		case "style":
			if isStyleReference(node) {
				continue // a reference, resolved by the cascade, not a definition
			}
			c.defineStyle(node)
		case "defaults":
			c.applyDefaults(node, ctx)
		case "vars":
			c.declareVars(node)
		case "let":
			c.declareLet(node)
		}
	}
}

// defineStyle records a named property bundle.
func (c *Compiler) defineStyle(node *pulp.Node) {
	if !node.HasArg {
		return
	}
	name := strings.Trim(node.Arg, `"'`)
	props := schema.NewProps()
	c.applyDirectProperties(node, props)
	if _, exists := c.styles[name]; !exists {
		c.styleOrder = append(c.styleOrder, name)
	}
	c.styles[name] = props
}

// applyDefaults merges a `defaults` block into the cascade context, either
// globally or narrowed to one element type.
func (c *Compiler) applyDefaults(node *pulp.Node, ctx cascadeContext) {
	target := ""
	if node.HasArg {
		target = strings.Trim(node.Arg, `"'`)
		if def, ok := schema.Element(target); ok {
			target = def.Name
		}
	}
	props := schema.NewProps()
	c.applyDirectProperties(node, props)
	// Defaults are deliberately NOT explicit: they are a baseline, and marking
	// them explicit would let them defeat inheritance, which is the CSS gotcha
	// DESIGN.md section 3 exists to avoid.
	demoted := schema.NewProps()
	for _, id := range props.SetIDs() {
		demoted.Set(id, props.Values(id))
	}

	if target == "" {
		ctx.global.MergeFrom(demoted)
		return
	}
	existing, ok := ctx.byType[target]
	if !ok {
		existing = schema.NewProps()
		ctx.byType[target] = existing
	}
	existing.MergeFrom(demoted)
}

// declareVars processes a document's `vars` block.
//
// An entry WITH a value is a default the document supplies; it goes in at the
// document layer so that --var and --vars-file still outrank it. An entry
// WITHOUT one declares a required parameter: it fills from --var or
// TKB_VAR_<NAME>, is an error if neither supplies it, and permits {env.<NAME>}.
// That asymmetry is the whole of DESIGN.md D11 — a `.pulp` file you receive
// from someone else can only read the environment it asked for by name, in
// writing, where you can see it.
func (c *Compiler) declareVars(node *pulp.Node) {
	for _, child := range node.Children {
		if !child.HasArg {
			c.scope.Declare(child.Name, child.NameSpan, c.src, c.diags)
			continue
		}
		value := c.scope.Interpolate(child.Arg, child.ArgSpan, c.src, c.diags)
		c.scope.Define(child.Name, vars.NewString(unquote(value)), vars.LayerDocument)
	}
}

// declareLet binds a subtree-scoped `let` block. The caller is responsible for
// having pushed a scope.
func (c *Compiler) declareLet(node *pulp.Node) {
	for _, child := range node.Children {
		value := c.scope.Interpolate(child.Arg, child.ArgSpan, c.src, c.diags)
		c.scope.Bind(child.Name, vars.NewString(unquote(value)))
	}
}

// pageSetup resolves the `page` element into a trim size and margins.
func (c *Compiler) pageSetup(doc *pulp.Document, ctx cascadeContext, result *Result, opts Options) *schema.Props {
	var pageNode *pulp.Node
	for _, node := range doc.TopLevel() {
		if node.Name == schema.EPage {
			pageNode = node
			break
		}
	}

	props := schema.NewProps()
	props.MergeFrom(c.base)
	props.MergeFrom(ctx.global)
	if typed := ctx.forType(schema.EPage); typed != nil {
		props.MergeFrom(typed)
	}
	if pageNode != nil {
		c.applyDirectProperties(pageNode, props)
	}

	size := NamedPageSize(props.Str(schema.PPageSize, "letter"))
	if pageNode != nil {
		if explicit, ok := c.explicitPageSize(pageNode); ok {
			size = explicit
		}
	}
	if props.Enum(schema.POrientation, "portrait") == "landscape" {
		size.Width, size.Height = size.Height, size.Width
	}
	if opts.PageSize != nil {
		size = *opts.PageSize
	}

	result.Page = size
	result.Margin = pageMargin(props)
	result.WeekStart = props.Enum(schema.PWeekStart, "monday")

	// The page's margin becomes the root node's padding, so the layout engine
	// sees an ordinary box with padding rather than a special case.
	props.Set(schema.PPadding, marginValues(result.Margin))
	return props
}

// defaultPageMargin is what a page gets when it asks for nothing. Half an inch
// is inside every consumer printer's unprintable area and is what every word
// processor defaults to, so a document that says nothing about margins still
// prints.
var defaultPageMargin = geom.In(0.5)

// pageMargin resolves a page's margin.
//
// On a page, `margin` and `padding` mean the same thing — the white border
// around the content — because a page has nothing outside it for a margin to
// separate it from. Accepting both is friendlier than being right about which
// one is technically correct, and silently ignoring whichever the author did
// not pick would be much worse.
func pageMargin(props *schema.Props) geom.Edges {
	if props.IsExplicit(schema.PMargin) {
		return props.Edges(schema.PMargin, geom.EdgesAll(defaultPageMargin))
	}
	if props.IsExplicit(schema.PPadding) {
		return props.Edges(schema.PPadding, geom.EdgesAll(defaultPageMargin))
	}
	return geom.EdgesAll(defaultPageMargin)
}

// explicitPageSize reads a `size: 200mm 300mm` pair, which the named-size path
// cannot express.
func (c *Compiler) explicitPageSize(pageNode *pulp.Node) (PageSize, bool) {
	sizeNode := pageNode.Child("size")
	if sizeNode == nil {
		sizeNode = pageNode.Child("page-size")
	}
	if sizeNode == nil || !sizeNode.HasArg {
		return PageSize{}, false
	}
	text := unquote(c.scope.Interpolate(sizeNode.Arg, sizeNode.ArgSpan, c.src, c.diags))

	// Try the whole argument as a name before lexing it. Several real page
	// sizes are spelled in a way the value lexer would misread — `4x6` looks
	// like a number with a unit, and `US Letter` like two keywords.
	if size, ok := lookupPageSize(text); ok {
		return size, true
	}

	var scratch pulp.Diagnostics
	values := pulp.ParseValues(c.src, sizeNode.ArgSpan, text, &scratch)
	if len(values) == 2 && values[0].Kind == pulp.KindLength && values[1].Kind == pulp.KindLength {
		return PageSize{Width: values[0].Length, Height: values[1].Length, Name: "custom"}, true
	}
	if len(values) != 2 {
		name := text
		d := c.diags.Errorf(c.src, sizeNode.ArgSpan, "E140", "unknown page size %q", name).
			WithLabel("unknown size")
		if ss := pulp.Suggest(name, PageSizeNames()); len(ss) > 0 {
			d.WithHelp("%s", pulp.FormatSuggestions("page size", ss))
		} else {
			d.WithHelp("Known sizes: %s. You can also give a width and height, as in `size: 200mm 300mm`.",
				strings.Join(PageSizeNames(), ", "))
		}
	}
	return PageSize{}, false
}

// buildNode converts one parse node into zero or more layout nodes. Loops and
// `when` are why the return is a slice.
func (c *Compiler) buildNode(node *pulp.Node, ctx cascadeContext, parent *schema.Props) []*layout.Node {
	switch node.Name {
	case "for":
		return c.buildLoop(node, ctx, parent)
	case "repeat":
		return c.buildRepeat(node, ctx, parent)
	}

	kind, ok := layout.KindFor(node.Name)
	if !ok {
		return nil
	}

	// A node's own `defaults` and `let` apply to it and its subtree only.
	childCtx := ctx
	if declaresDirectives(node) {
		childCtx = ctx.clone()
		c.scope.Push()
		defer c.scope.Pop()
		c.collectDirectives(node.Children, childCtx)
	}

	props := c.resolveProps(node, node.Name, childCtx, parent)
	if !props.Bool(schema.PWhen, true) {
		return nil
	}

	out := layout.NewNode(kind)
	out.Props = props
	out.Source = c.src
	out.Span = node.NameSpan.Join(node.ArgSpan)

	switch kind {
	case layout.KindText:
		out.Text = c.textContent(node, props)
	case layout.KindPanel, layout.KindBox, layout.KindGrid:
		out.Title = applyTransform(props.Str(schema.PTitle, ""), props.Enum(schema.PTitleTransform, "upper"))
	}

	for _, child := range node.Children {
		if !isContentNode(child.Name) {
			continue
		}
		for _, built := range c.buildNode(child, childCtx, props) {
			out.Append(built)
		}
	}
	return []*layout.Node{out}
}

// textContent resolves a text node's content, from either its argument or a
// `content:` property, and applies text-transform.
func (c *Compiler) textContent(node *pulp.Node, props *schema.Props) string {
	text := node.Arg
	span := node.ArgSpan
	if content := node.Child("content"); content != nil {
		text, span = content.Arg, content.ArgSpan
	}
	// Single quotes are fully raw, so the check has to come before substitution.
	if isRawQuoted(text) {
		return applyTransform(unquote(strings.TrimSpace(text)), props.Enum(schema.PTextTransform, "none"))
	}
	text = unquote(c.scope.Interpolate(text, span, c.src, c.diags))
	return applyTransform(text, props.Enum(schema.PTextTransform, "none"))
}

// buildLoop expands `for <name> in <path>`.
func (c *Compiler) buildLoop(node *pulp.Node, ctx cascadeContext, parent *schema.Props) []*layout.Node {
	name, path, ok := parseForClause(node.Arg)
	if !ok {
		c.diags.Errorf(c.src, node.ArgSpan, "E141", "`for` needs the form `for <name> in <list>`").
			WithLabel("malformed loop").
			WithHelp("For example: `for day in week.days`.")
		return nil
	}

	items, ok := c.scope.List(path)
	if !ok {
		// A literal list is the other accepted form: `for tag in Home, Work`.
		if literal := splitLiteralList(path); len(literal) > 0 {
			items = literal
		} else {
			c.diags.Errorf(c.src, node.ArgSpan, "E142", "cannot iterate %q", path).
				WithLabel("not a list").
				WithHelp("Iterate a built-in list such as `week.days` or `week.weekdays`, or give a comma-separated list.")
			return nil
		}
	}

	var out []*layout.Node
	for index, item := range items {
		c.scope.Push()
		c.scope.Bind(name, item)
		c.scope.Bind("loop", vars.NewLoop(index, len(items)))
		for _, child := range node.Children {
			out = append(out, c.buildNode(child, ctx, parent)...)
		}
		c.scope.Pop()
	}
	return out
}

// buildRepeat expands `repeat <count>`.
func (c *Compiler) buildRepeat(node *pulp.Node, ctx cascadeContext, parent *schema.Props) []*layout.Node {
	text := c.scope.Interpolate(node.Arg, node.ArgSpan, c.src, c.diags)
	count, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || count < 0 {
		c.diags.Errorf(c.src, node.ArgSpan, "E143", "`repeat` needs a whole number, got %q", text).
			WithLabel("not a count")
		return nil
	}
	const maxRepeat = 10000
	if count > maxRepeat {
		c.diags.Errorf(c.src, node.ArgSpan, "E144", "`repeat %d` is too many", count).
			WithLabel("too many repetitions").
			WithHelp("The limit is %d. A form with more rows than that is almost certainly a mistake.", maxRepeat)
		return nil
	}

	var out []*layout.Node
	for index := 0; index < count; index++ {
		c.scope.Push()
		c.scope.Bind("loop", vars.NewLoop(index, count))
		for _, child := range node.Children {
			out = append(out, c.buildNode(child, ctx, parent)...)
		}
		c.scope.Pop()
	}
	return out
}

// isContentNode reports whether a name produces something on the page.
//
// Loops count: they are transparent, and expand to whatever they contain. This
// is one predicate rather than a repeated `IsLayoutElement || for || repeat`
// because the version at the top level once omitted the loop cases, and a
// top-level `for` was then dropped in silence — a blank page and exit 0, which
// is the worst way for a tool to be wrong.
// isStyleReference distinguishes `style: name` from a `style <name>` block, the
// same way the schema validator does.
func isStyleReference(n *pulp.Node) bool {
	return n.Name == "style" && n.HasArg && len(n.Children) == 0
}

func isContentNode(name string) bool {
	return schema.IsLayoutElement(name) || name == "for" || name == "repeat"
}

// declaresDirectives reports whether a node carries its own defaults, styles or
// bindings, and so needs a cloned cascade context.
func declaresDirectives(node *pulp.Node) bool {
	for _, child := range node.Children {
		switch child.Name {
		case "defaults", "style", "let", "vars":
			// `style: name` on a node is a reference, not a definition; only a
			// `style <name>` block with children defines one.
			if child.Name == "style" && len(child.Children) == 0 {
				continue
			}
			return true
		}
	}
	return false
}

// parseForClause splits `day in week.days` into its parts.
func parseForClause(arg string) (name, path string, ok bool) {
	fields := strings.Fields(arg)
	if len(fields) < 3 || fields[1] != "in" {
		return "", "", false
	}
	return fields[0], strings.Join(fields[2:], " "), true
}

// splitLiteralList reads the comma-separated form, `for tag in Home, Work`.
func splitLiteralList(text string) []vars.Value {
	if !strings.Contains(text, ",") {
		return nil
	}
	var out []vars.Value
	for _, part := range strings.Split(text, ",") {
		if trimmed := strings.TrimSpace(strings.Trim(strings.TrimSpace(part), `"'`)); trimmed != "" {
			out = append(out, vars.NewString(trimmed))
		}
	}
	return out
}

// applyTransform applies text-transform. It duplicates the layout package's
// helper rather than importing it, because compile sits below layout in the
// dependency order and a shared package for one switch statement would cost
// more than it saves.
func applyTransform(s, transform string) string {
	switch transform {
	case "upper":
		return strings.ToUpper(s)
	case "lower":
		return strings.ToLower(s)
	case "title":
		return titleCase(s)
	}
	return s
}

func titleCase(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	atWordStart := true
	for _, r := range s {
		if atWordStart {
			b.WriteString(strings.ToUpper(string(r)))
		} else {
			b.WriteRune(r)
		}
		atWordStart = r == ' ' || r == '\t' || r == '-' || r == '/'
	}
	return b.String()
}

// marginValues converts resolved edges back into property values, so the page
// margin can be installed as the root node's padding.
func marginValues(e geom.Edges) []pulp.Value {
	lengths := []geom.Tick{e.Top, e.Right, e.Bottom, e.Left}
	out := make([]pulp.Value, 0, 4)
	for _, l := range lengths {
		out = append(out, pulp.Value{Kind: pulp.KindLength, Length: l, Unit: "pt", Num: l.Points()})
	}
	return out
}
