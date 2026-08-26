// What a theme is allowed to say.
//
// A theme carries the same three shapes a document does — a global `defaults`
// block, per-element `defaults <element>` blocks, and named `style` bundles —
// so it is written in exactly the language it themes. The question is therefore
// not "what can a theme express?" but "what can a theme express without
// breaking the document it is applied to?", because the whole promise of
// `--theme` is that a document survives a swap.
//
// Two rules, both found by trying it:
//
//  1. **Page setup is never a theme's business.** A theme that changed the
//     paper size would silently reflow every document it touched.
//  2. **Box metrics and decoration switches are refused in the GLOBAL block,
//     and welcome in a per-element one.** `border-width` in a bare `defaults`
//     frames every section and column on the page; `line-style: dotted` there
//     rules the header band and the page itself. In `defaults panel` both are
//     exactly what a theme is for. Neither failure names itself, so the global
//     case is refused with the per-element form given as the fix.
package themes

import (
	"strings"

	"github.com/jclement/treekillbot/internal/pulp"
	"github.com/jclement/treekillbot/internal/schema"
)

// The reasons a property is refused, written as the help line the author reads.
const (
	reasonBoxMetric = "In a bare `defaults` block this sizes and pads sections, columns and text nodes " +
		"as well as the panels you meant. Move it into a `defaults panel` block — a theme may narrow to an " +
		"element, and that is what the narrowing is for."
	reasonBorderShorthand = "The `border` shorthand carries a width, and a width in a bare `defaults` block " +
		"frames every section and column on the page. Move it into a `defaults panel` block, or write just " +
		"`border-color` and `border-style` here."
	reasonDecorationSwitch = "In a bare `defaults` block this decorates the page, every section and every " +
		"column, not the panels you meant. Move it into a `defaults panel` block."
	reasonPageSetup = "Page setup belongs to the document. A theme that changed the paper would silently " +
		"reflow every document it was applied to."
	reasonStructural = "This describes one node, and a theme has no node to attach it to."
)

// refusedAnywhere maps a property to the help line explaining why no theme may
// set it, in any block. A property absent from both maps is fair game.
var refusedAnywhere = func() map[schema.PropID]string {
	m := map[schema.PropID]string{}
	add := func(reason string, ids ...schema.PropID) {
		for _, id := range ids {
			m[id] = reason
		}
	}
	add(reasonPageSetup, schema.PPageSize, schema.POrientation, schema.PWeekStart, schema.PBleed)
	add(reasonStructural, schema.PStyle, schema.PWhen, schema.PID, schema.PTitle)
	return m
}()

// refusedGlobally maps a property to the help line explaining why it cannot go
// in a theme's BARE `defaults` block. The same property in `defaults panel` is
// exactly what a theme should be saying.
var refusedGlobally = func() map[schema.PropID]string {
	m := map[schema.PropID]string{}
	add := func(reason string, ids ...schema.PropID) {
		for _, id := range ids {
			m[id] = reason
		}
	}

	add(reasonBoxMetric,
		schema.PWidth, schema.PHeight, schema.PMinHeight, schema.PMaxHeight,
		schema.PPadding, schema.PPaddingTop, schema.PPaddingRight, schema.PPaddingBottom, schema.PPaddingLeft,
		schema.PMargin, schema.PMarginTop, schema.PMarginRight, schema.PMarginBottom, schema.PMarginLeft,
		schema.PGap, schema.POverflow,
		schema.PBorderWidth, schema.PBorderTopWidth, schema.PBorderRightWidth,
		schema.PBorderBottomWidth, schema.PBorderLeftWidth)
	add(reasonBorderShorthand, schema.PBorder)
	add(reasonDecorationSwitch, schema.PLineStyle, schema.PLineCount, schema.PMarginRule)

	return m
}()

// validateTheme checks that a theme file says only things a theme may say.
//
// It runs before compilation, so a refusal names the line the author wrote
// rather than a consequence of it several stages later.
func validateTheme(src *pulp.Source, doc *pulp.Document) error {
	var diags pulp.Diagnostics

	for _, node := range doc.TopLevel() {
		switch node.Name {
		case "defaults":
			checkDefaultsBlock(src, node, &diags)
		case "style":
			checkStyleBlock(src, node, &diags)
		case "vars":
			// A theme's `vars` block defines named constants a document can
			// reference. Their names are the theme's own vocabulary, so there
			// is nothing here to check them against.
		default:
			diags.Errorf(src, node.NameSpan,
				"E150", "a theme is made of `defaults`, `style` and `vars` blocks, and `%s` is not one", node.Name).
				WithLabel("not allowed in a theme").
				WithHelp("A theme carries the ink a document is printed in, not the document itself. " +
					"Write `defaults`, `defaults <element>`, `style <name>` or `vars` blocks.")
		}
	}
	return firstError(diags)
}

// checkDefaultsBlock validates one `defaults` or `defaults <element>` block.
func checkDefaultsBlock(src *pulp.Source, node *pulp.Node, diags *pulp.Diagnostics) {
	global := !node.HasArg
	if !global {
		target := strings.Trim(node.Arg, `"'`)
		if _, ok := schema.Element(target); !ok {
			d := diags.Errorf(src, node.ArgSpan,
				"E151", "`defaults %s` names no element", target).
				WithLabel("not an element")
			if suggestions := pulp.Suggest(target, schema.ElementNames()); len(suggestions) > 0 {
				d.WithHelp("%s", pulp.FormatSuggestions("element", suggestions))
			} else {
				d.WithHelp("Run `treekillbot docs elements` for the list, or drop the argument to set " +
					"defaults for everything.")
			}
			return
		}
	}
	checkProperties(src, node, global, diags)
}

// checkStyleBlock validates a named style bundle.
//
// A style is applied deliberately, by a document naming it, so the global-block
// restrictions do not apply: if a document asks for `style: ruled`, being ruled
// is what it asked for.
func checkStyleBlock(src *pulp.Source, node *pulp.Node, diags *pulp.Diagnostics) {
	if !node.HasArg {
		// Belt and braces: schema.Validate runs first and reports this as E120
		// with the same advice, so this branch is not normally reached. It stays
		// so that validateTheme is correct on its own, rather than correct only
		// because of what happens to run before it.
		return
	}
	checkProperties(src, node, false, diags)
}

// checkProperties validates the children of a theme block.
func checkProperties(src *pulp.Source, node *pulp.Node, global bool, diags *pulp.Diagnostics) {
	for _, child := range node.Children {
		if schema.IsElement(child.Name) {
			diags.Errorf(src, child.NameSpan, "E150", "a theme cannot contain the element `%s`", child.Name).
				WithLabel("not allowed in a theme").
				WithHelp("A theme sets properties and nothing else. Elements belong in the document.")
			continue
		}
		id, ok := schema.Lookup(child.Name)
		if !ok {
			continue // schema.Validate has already reported it, with a suggestion
		}
		if reason, refused := refusedAnywhere[id]; refused {
			refuse(src, child, id, reason, diags)
			continue
		}
		if reason, refused := refusedGlobally[id]; refused && global {
			refuse(src, child, id, reason, diags)
		}
	}
}

func refuse(src *pulp.Source, child *pulp.Node, id schema.PropID, reason string, diags *pulp.Diagnostics) {
	diags.Errorf(src, child.NameSpan, "E152", "`%s` cannot be set here by a theme", schema.Name(id)).
		WithLabel("not allowed here").
		WithHelp("%s", reason)
}
