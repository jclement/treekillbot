// Package docs holds the reference material `treekillbot docs` prints.
//
// The error registry exists because every diagnostic ends with
// "treekillbot docs errors E101". A tool that tells you to run a command that
// does not work, or that prints nothing useful when you do, has spent your
// trust for nothing — so every code a diagnostic can carry has an entry here,
// and a test asserts that.
package docs

import "sort"

// ErrorDoc explains one diagnostic code.
type ErrorDoc struct {
	Code string
	// Title restates the problem in one line.
	Title string
	// Explanation says why the rule exists. This is the part worth reading:
	// the message already told you what happened.
	Explanation string
	// Fix is what to do about it.
	Fix string
	// Example is a short snippet showing the mistake and its correction.
	Example string
}

var errorDocs = []ErrorDoc{
	{
		Code:  "E001",
		Title: "A line does not begin with a name",
		Explanation: "Every line in Pulp is a node, written as a name followed by an optional " +
			"argument. A line that starts with something else — a digit, a bracket, punctuation — " +
			"cannot be one.",
		Fix:     "Start the line with a name such as `panel`, `text` or `font-size`.",
		Example: "  2. buy milk        # not a name\n  text: 2. buy milk  # a text node whose content is \"2. buy milk\"",
	},
	{
		Code:  "E002",
		Title: "A tab appears in indentation",
		Explanation: "Pulp indents with spaces only. Tabs are refused rather than assigned a width " +
			"because every choice of width is wrong for somebody, and the resulting misalignment is " +
			"invisible in the file — it silently reparents a node and produces a valid document that " +
			"is not the one you wrote.",
		Fix: "Run `treekillbot fmt <file>` to convert the file, or set your editor to insert spaces.",
	},
	{
		Code:  "E003",
		Title: "Indentation does not match any enclosing block",
		Explanation: "The first child of a block fixes that block's column, and every sibling must " +
			"start at exactly that column. Dedenting to a column no enclosing block is open at is " +
			"refused for the same reason tabs are: the alternative is silently attaching the line to " +
			"the wrong parent.",
		Fix:     "Indent the line to one of the columns the error lists, or run `treekillbot fmt`.",
		Example: "section\n  column\n    panel\n  text: fine     # column 3, which `section` is open at\n   text: refused # column 4, which nothing is open at",
	},
	{
		Code:  "E010",
		Title: "Content does not fit its box",
		Explanation: "A form that does not fit is a document bug, and paper cannot scroll. The most " +
			"useful thing the tool can do is say which box is short and by how much, rather than " +
			"silently squashing something and letting you discover it after printing.",
		Fix: "Give the box more room, reduce what is inside it, or set `overflow: clip` on the box " +
			"to allow it deliberately. `--allow-overflow` downgrades every one of these to a warning.",
	},
	{
		Code:  "E021",
		Title: "A length is missing its unit",
		Explanation: "Lengths always carry a unit. A bare number would have to mean points by " +
			"convention, and a convention that silently changes a measurement by a factor of 72 is " +
			"not worth the two characters it saves.",
		Fix:     "Add pt, in, mm, cm, pc, px or em.",
		Example: "height: 200     # refused\nheight: 200pt   # 200 points\nheight: 200mm   # 200 millimetres",
	},
	{
		Code:        "E030",
		Title:       "More values than the property accepts",
		Explanation: "The property takes a single value, and more than one was given.",
		Fix:         "Remove the extra values, or quote the whole thing if it was meant as one string.",
	},
	{
		Code:        "E031",
		Title:       "A string is not closed",
		Explanation: "A quoted string ran to the end of the line without its closing quote.",
		Fix:         "Add the closing quote. Single quotes are raw: no escapes and no interpolation.",
	},
	{
		Code:        "E032",
		Title:       "A value that looks like a number is not one",
		Explanation: "The value begins with a digit, a sign or a decimal point but does not parse as a number.",
		Fix:         "Correct the number, or quote the value if it was meant as text.",
	},
	{
		Code:        "E033",
		Title:       "Unknown unit",
		Explanation: "The suffix after a number is not a unit Pulp knows.",
		Fix:         "Use pt, in, mm, cm, pc, px, em or ex. A percentage is written `30%` with no space.",
	},
	{
		Code:  "E034",
		Title: "Not a valid colour",
		Explanation: "A hex colour is `#` followed by exactly 3, 4, 6 or 8 hex digits. Other lengths " +
			"are refused rather than padded, because guessing at what `#abcde` meant is worse than asking.",
		Fix:     "Write `#ddd`, `#dddddd`, `gray(0.85)`, `rgb(31 111 235)`, `cmyk(0 0 0 0.2)` or a CSS colour name.",
		Example: "background: #ddd\nline-color: gray(0.78)   # preferred: DeviceGray survives to the printer unchanged",
	},
	{
		Code:        "E035",
		Title:       "Wrong number of function arguments",
		Explanation: "A colour or sizing function was given a different number of arguments than it takes.",
		Fix:         "`gray()` takes 1 or 2, `rgb()` takes 3 or 4, `cmyk()` takes 4, `fill()` takes 1.",
	},
	{
		Code:        "E036",
		Title:       "A fill weight must be positive",
		Explanation: "`fill(n)` takes a share of the leftover space proportional to n, so a weight of zero or less has no meaning.",
		Fix:         "Use `fill` for an equal share, or `fill(2)` for twice as much as a bare `fill`.",
	},
	{
		Code:  "E101",
		Title: "Unknown property or element",
		Explanation: "The name is not in the schema. Because Pulp resolves names against a known set " +
			"with a known parent, the tool can usually tell you what you meant.",
		Fix: "Check the spelling, or run `treekillbot docs props` for the full list. Pulp spells names " +
			"in kebab-case, so `lineStyle` and `line_style` are both `line-style`.",
	},
	{
		Code:  "E102",
		Title: "The property is real but does not apply here",
		Explanation: "The name exists, but not on this element. `line-style` means something on a " +
			"panel and nothing on a text node, so allowing it there would let a setting be silently ignored.",
		Fix: "Move it to an element it applies to. The error lists them.",
	},
	{
		Code:        "E103",
		Title:       "A property was given no value",
		Explanation: "The property name is there but nothing follows it.",
		Fix:         "Write `name: value`. Inside a `vars` block, a name with no value is a required parameter and is legal.",
	},
	{
		Code:        "E110",
		Title:       "The value is the wrong kind",
		Explanation: "The property expects one kind of value and was given another.",
		Fix:         "Check what the property takes with `treekillbot docs props <name>`.",
	},
	{
		Code:        "E111",
		Title:       "Not a valid value for this property",
		Explanation: "The property accepts a fixed set of keywords and this is not one of them.",
		Fix:         "The error lists the valid values, and suggests the nearest one when there is a close match.",
	},
	{
		Code:        "E113",
		Title:       "Too many lengths for an edge property",
		Explanation: "Padding, margin and border-width follow CSS shorthand arity.",
		Fix:         "One value sets all sides, two set vertical then horizontal, three set top/horizontal/bottom, four set top, right, bottom, left.",
	},
	{
		Code:        "E120",
		Title:       "An element needs an argument",
		Explanation: "Some elements are meaningless without one: `style` needs a name, `for` needs a loop clause.",
		Fix:         "Give it the argument the error describes.",
	},
	{
		Code:        "E121",
		Title:       "An element takes no argument",
		Explanation: "`section` and `column` are pure structure; there is nothing for an argument to mean.",
		Fix:         "Put the settings on indented lines beneath the element instead.",
	},
	{
		Code:        "E122",
		Title:       "This element cannot contain that one",
		Explanation: "The containing element holds no other elements. A `text` node holds text, not panels.",
		Fix:         "Move the inner element out, or wrap both in a `box` or `section`.",
	},
	{
		Code:  "E123",
		Title: "A property cannot contain an element",
		Explanation: "A property is a leaf. Finding an element indented beneath one almost always " +
			"means a line is indented one level too far.",
		Fix: "Dedent the element by one level so it sits beside the property rather than inside it.",
	},
	{
		Code:        "E124",
		Title:       "A variable cannot contain an element",
		Explanation: "Entries in a `vars` or `let` block hold a single value.",
		Fix:         "Write `name: value` on one line.",
	},
	{
		Code:        "E130",
		Title:       "Unknown style",
		Explanation: "A `style:` reference names a bundle that was never defined.",
		Fix:         "Define it with a `style <name>` block, or correct the name. Styles may be defined after they are used.",
	},
	{
		Code:        "E140",
		Title:       "Unknown page size",
		Explanation: "The `size` property names a page size the tool does not know.",
		Fix:         "Run `treekillbot docs sizes` for the list, or give an explicit pair such as `size: 200mm 300mm`.",
	},
	{
		Code:        "E141",
		Title:       "Malformed loop",
		Explanation: "`for` takes a name, the word `in`, and something to iterate.",
		Fix:         "Write `for day in week.days`, or `for tag in Home, Work, Errands` for a literal list.",
	},
	{
		Code:        "E142",
		Title:       "That is not something you can iterate",
		Explanation: "The path after `in` does not resolve to a list.",
		Fix:         "Use a built-in list such as `week.days`, `week.weekdays`, `week.weekend` or `month.weeks`, or give a comma-separated literal list.",
	},
	{
		Code:        "E143",
		Title:       "`repeat` needs a whole number",
		Explanation: "The count did not parse as a non-negative integer.",
		Fix:         "Write `repeat 24`. To repeat over data instead, use `for`.",
	},
	{
		Code:        "E144",
		Title:       "Too many repetitions",
		Explanation: "The repeat count exceeds the limit. A form with that many rows is almost always a mistake, usually a variable that resolved to something unexpected.",
		Fix:         "Reduce the count, or check the variable it came from.",
	},
	{
		Code:  "E150",
		Title: "A theme contains something that is not a `defaults` or `style` block",
		Explanation: "A theme carries the ink a document is printed in, not the document. It cannot " +
			"contain sections, panels or any other element, because a theme has to be swappable: a " +
			"document must render with any theme applied, and a theme that contributed content would " +
			"break that the moment it was swapped for one contributing different content.",
		Fix: "Move the element into the document. A theme holds `defaults`, `defaults <element>` and " +
			"`style <name>` blocks, and nothing else.",
		Example: "defaults\n  font: IBM Plex Mono\n\ndefaults panel\n  border-color: gray(0.78)\n\nstyle ruled\n  line-style: ruled",
	},
	{
		Code:  "E151",
		Title: "A theme's `defaults` block named something that is not an element",
		Explanation: "`defaults <name>` narrows a set of defaults to one element type, so the name has " +
			"to be an element.",
		Fix: "Use an element name — run `treekillbot docs elements` for the list — or drop the argument " +
			"to set defaults for everything.",
	},
	{
		Code:  "E152",
		Title: "A theme cannot set that property here",
		Explanation: "Two separate rules land on this code. Page setup — the paper size, orientation, " +
			"week start — is never a theme's business, because a theme that changed the paper would " +
			"silently reflow every document it touched. Box metrics and decoration switches are refused " +
			"only in a BARE `defaults` block, which applies to every element: `border-width` there frames " +
			"every section and column on the page, and `line-style` there rules the header band and the " +
			"page itself. In a `defaults panel` block both are exactly what a theme is for.",
		Fix: "For a box metric or a decoration, move it into `defaults <element>`. For page setup, set it " +
			"in the document or pass the corresponding flag.",
		Example: "defaults\n  color: gray(0.15)        # ink: fine anywhere\n\ndefaults panel\n  border-width: 0.5pt      # a box metric: fine here, refused in the bare block\n  line-style: ruled",
	},
	{
		Code:        "E153",
		Title:       "A `theme` directive names no theme",
		Explanation: "`theme` selects a theme by name, and no name was given.",
		Fix:         "Write `theme mono`. Run `treekillbot themes` to see what is available.",
	},
	{
		Code:  "E154",
		Title: "The theme could not be loaded",
		Explanation: "A `theme` directive named a theme that does not exist, or one that exists and is " +
			"invalid. This is an error rather than a silent fallback: rendering unthemed after someone " +
			"asked for a theme produces a page that looks fine and is not the one they wanted.",
		Fix: "Check the name against `treekillbot themes`. A theme file beside the document shadows a " +
			"built-in of the same name, so an empty or broken file there will hide the built-in.",
	},
	{
		Code:        "E200",
		Title:       "Malformed interpolation",
		Explanation: "A `{…}` reference is not well formed — usually an unclosed brace.",
		Fix: "Write `{name}`, `{name:format}`, `{name|fallback}` or `{cond ? a : b}`. " +
			"For a literal brace, write `{{` and `}}`, or use 'single quotes', which are fully raw.",
	},
	{
		Code:        "E201",
		Title:       "Malformed conditional",
		Explanation: "A `{a ? b : c}` conditional does not have the shape the language accepts. The condition grammar is deliberately capped at an optional `not`, a path, and an optional comparison against a literal.",
		Fix:         "Write `{day.weekend ? \"#f7f7f4\" : \"#ffffff\"}`. Spaces around `?` and `:` are required. For anything more involved, compute it with a `let`.",
	},
	{
		Code:        "E202",
		Title:       "Unknown format specifier",
		Explanation: "The text after `:` in an interpolation is not a format this value understands.",
		Fix:         "Dates take named formats (`iso`, `short`, `long`) or strftime (`%Y-%m-%d`). Numbers take printf verbs. Strings take `upper`, `lower`, `title`, `trunc N`, `pad N`.",
	},
	{
		Code:        "E210",
		Title:       "Unresolved variable",
		Explanation: "A `{…}` reference names something that is not defined and has no fallback.",
		Fix: "Define it in a `vars` block, pass `--var name=value`, or give the reference a fallback " +
			"with `{name|default}`. `--allow-undefined` renders it empty instead, which is for drafts, not for printing.",
	},
	{
		Code:  "E211",
		Title: "A required variable was not supplied",
		Explanation: "The document declared the variable in its `vars` block without a value, which " +
			"makes it a required parameter. Rendering a blank where a value was promised is worse on " +
			"paper than anywhere else, because you find out after printing.",
		Fix: "Pass `--var name=value`, or set the TKB_VAR_<NAME> environment variable.",
	},
	{
		Code:  "E212",
		Title: "Undeclared environment variable",
		Explanation: "Environment access is declared, never ambient. A `.pulp` file is a document you " +
			"might receive from someone else, and ambient expansion would make " +
			"`text: {env.AWS_SECRET_ACCESS_KEY}` an exfiltration primitive in a shared planner template.",
		Fix: "Declare the name in the document's `vars` block, which both permits `{env.NAME}` and " +
			"lets the value come from `--var` or TKB_VAR_<NAME>. `--unsafe-env` lifts the restriction " +
			"entirely, and is named to discourage it.",
	},
	{
		Code:        "W010",
		Title:       "Content overflows (warning)",
		Explanation: "The same condition as E010, downgraded because `overflow` was set to something other than `error`, or `--allow-overflow` was given.",
		Fix:         "Nothing is required. If it was not deliberate, see E010.",
	},
	{
		Code:        "W020",
		Title:       "Text was shrunk to fit",
		Explanation: "`auto-shrink` reduced the type size so the text would fit its box. That is always worth saying: the document asked for one size and got another.",
		Fix:         "Give the box more height, shorten the text, or set `auto-shrink: 0` to make it an error instead.",
	},
	{
		Code:  "W021",
		Title: "Text was clipped",
		Explanation: "A whole line of text fell outside its box, so it was clipped to the box rather " +
			"than painted past the border. `auto-shrink` is off by default, so this is the ordinary " +
			"outcome for text that does not fit; with shrinking enabled it means the text did not fit " +
			"even at the smallest size allowed.\n\nA descender grazing the bottom edge is not this: " +
			"the check asks whether a line's baseline falls more than a descender's depth below the box, " +
			"so a box sized snugly to its own text does not warn.",
		Fix: "Give the box more room, shorten the text, or set `auto-shrink` to let it shrink to fit.",
	},
	{
		Code:  "W030",
		Title: "`line-height` set where `line-pitch` was probably meant",
		Explanation: "`line-height` is text leading, in the CSS sense. The spacing of ruled lines, " +
			"dots, graph squares and checkbox rows is `line-pitch`. Both names are real, so setting " +
			"the wrong one changes nothing and reports nothing — which is why this warning exists.",
		Fix:     "Set `line-pitch` to move the rules. Set both if you also want to change the leading of text drawn on them.",
		Example: "panel \"Notes\"\n  line-style: ruled\n  line-pitch: 15pt   # the distance between rules",
	},
	{
		Code:        "W210",
		Title:       "Unresolved variable rendered as empty",
		Explanation: "The same condition as E210, downgraded by `--allow-undefined`.",
		Fix:         "Fine for a draft. Do not print it, and do not use `--allow-undefined` in CI.",
	},
}

var byCode = func() map[string]*ErrorDoc {
	m := make(map[string]*ErrorDoc, len(errorDocs))
	for i := range errorDocs {
		m[errorDocs[i].Code] = &errorDocs[i]
	}
	return m
}()

// LookupError returns the documentation for a diagnostic code.
func LookupError(code string) (*ErrorDoc, bool) {
	doc, ok := byCode[code]
	return doc, ok
}

// AllErrors returns every documented code, sorted, so listings are stable.
func AllErrors() []ErrorDoc {
	out := make([]ErrorDoc, len(errorDocs))
	copy(out, errorDocs)
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// ErrorCodes returns every documented code, sorted.
func ErrorCodes() []string {
	out := make([]string, 0, len(errorDocs))
	for _, doc := range errorDocs {
		out = append(out, doc.Code)
	}
	sort.Strings(out)
	return out
}
