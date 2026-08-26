// The element table: the names that describe structure rather than appearance.
//
// Elements are permissive about what they contain. A stricter containment
// model would let the schema catch "a column inside a text", but it would also
// reject arrangements that work fine and that someone had a reason for, and the
// error would be about grammar rather than about the page. Layout reports the
// things that actually go wrong — a box that does not fit, a column with no
// width to divide — and those errors are about the document.
//
// The one structural rule that IS enforced is the one that carries meaning: a
// run of consecutive sibling `column` nodes forms a row. That is what makes the
// original sketch's columns-inside-a-section mean what it looks like.
package schema

// ArgMode describes whether an element takes a positional argument.
type ArgMode uint8

const (
	ArgNone     ArgMode = iota // `section`
	ArgOptional                // `panel` or `panel "Notes"`
	ArgRequired                // `text "Day of {date}"`
)

// ElementDef describes one element.
type ElementDef struct {
	Name string
	Arg  ArgMode
	// ArgProp is the property a positional argument is shorthand for, so
	// `panel "Notes"` and `panel` + `title: Notes` are the same thing.
	ArgProp   PropID
	Container bool // whether children are meaningful
	Doc       string
}

// elements is the table, in the order `treekillbot docs elements` prints them:
// structure first, then content, then the directives.
var elements = []ElementDef{
	{Name: EPage, Arg: ArgNone, Container: true,
		Doc: "Page setup: size, orientation and margins. One per document, at the top."},
	{Name: ESection, Arg: ArgNone, Container: true,
		Doc: "A horizontal band stacked vertically inside its parent."},
	{Name: EColumn, Arg: ArgNone, Container: true,
		Doc: "A vertical strip. Consecutive sibling columns sit side by side, forming a row."},
	{Name: EPanel, Arg: ArgOptional, ArgProp: PTitle, Container: true,
		Doc: "A titled box, usually with a border and a line decoration inside it."},
	{Name: EBox, Arg: ArgOptional, ArgProp: PTitle, Container: true,
		Doc: "A panel without the chrome: a rectangle that groups things."},
	{Name: EGrid, Arg: ArgOptional, ArgProp: PTitle, Container: true,
		Doc: "A repeating lattice of cells, for a month calendar or a week of day boxes."},
	{Name: EText, Arg: ArgOptional, Container: false,
		Doc: "A run of text. Wraps to its box unless wrap is off."},
	{Name: ERule, Arg: ArgNone, Container: false,
		Doc: "A horizontal line across the content width."},
	{Name: ESpacer, Arg: ArgOptional, ArgProp: PHeight, Container: false,
		Doc: "Empty space. `spacer fill` pushes what follows to the bottom."},
	{Name: EImage, Arg: ArgRequired, Container: false,
		Doc: "A PNG or JPEG placed in the flow."},

	{Name: "vars", Arg: ArgNone, Container: true,
		Doc: "Declare the document's variables. Values may come from --var or the environment."},
	{Name: "defaults", Arg: ArgOptional, Container: true,
		Doc: "Set defaults for everything below. `defaults panel` narrows them to one element type."},
	{Name: "style", Arg: ArgRequired, Container: true,
		Doc: "Define a named bundle of properties, applied with `style: name`."},
	{Name: "let", Arg: ArgNone, Container: true,
		Doc: "Bind variables scoped to this subtree."},
	{Name: "theme", Arg: ArgRequired, Container: false,
		Doc: "Select a theme by name. Later declarations win."},
	{Name: "for", Arg: ArgRequired, Container: true,
		Doc: "Repeat the children once per item, as in `for day in week.days`."},
	{Name: "repeat", Arg: ArgRequired, Container: true,
		Doc: "Repeat the children a fixed number of times."},
	{Name: "include", Arg: ArgRequired, Container: false,
		Doc: "Splice in another .pulp file."},
}

var elementsByName = func() map[string]*ElementDef {
	m := make(map[string]*ElementDef, len(elements))
	for i := range elements {
		m[elements[i].Name] = &elements[i]
	}
	// Accepted synonyms, normalised by `fmt`.
	m["row"] = m[ESection]
	m["col"] = m[EColumn]
	m["hr"] = m[ERule]
	m["space"] = m[ESpacer]
	return m
}()

// Element looks up an element definition by name.
func Element(name string) (*ElementDef, bool) {
	e, ok := elementsByName[name]
	return e, ok
}

// IsElement reports whether a name is an element rather than a property.
func IsElement(name string) bool {
	_, ok := elementsByName[name]
	return ok
}

// ElementNames returns every canonical element name in table order, for
// suggestions and documentation.
func ElementNames() []string {
	out := make([]string, 0, len(elements))
	for i := range elements {
		out = append(out, elements[i].Name)
	}
	return out
}

// IsLayoutElement reports whether an element occupies space on the page, as
// opposed to a directive like `vars` or `style` that only configures things.
func IsLayoutElement(name string) bool {
	switch name {
	case EPage, ESection, EColumn, EPanel, EBox, EGrid, EText, ERule, ESpacer, EImage:
		return true
	}
	return false
}
