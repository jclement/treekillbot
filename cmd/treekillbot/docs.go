// The docs command: the reference material the error messages point at.
package main

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/jclement/treekillbot/internal/compile"
	"github.com/jclement/treekillbot/internal/docs"
	"github.com/jclement/treekillbot/internal/pulp"
	"github.com/jclement/treekillbot/internal/schema"
	"github.com/jclement/treekillbot/internal/ui"
)

func newDocsCommand(console *ui.Console) *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "docs <topic> [name]",
		Short: "Reference for properties, elements, errors, colours and page sizes",
		Long: "Topics: props, elements, errors, colors, sizes.\n\n" +
			"  treekillbot docs props              every property\n" +
			"  treekillbot docs props line-pitch   one property in detail\n" +
			"  treekillbot docs errors E101        what a diagnostic code means\n",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			topic := strings.ToLower(args[0])
			name := ""
			if len(args) > 1 {
				name = args[1]
			}
			switch topic {
			case "props", "properties", "prop":
				return docsProps(console, name, asJSON)
			case "elements", "element":
				return docsElements(console, asJSON)
			case "errors", "error":
				return docsErrors(console, name, asJSON)
			case "colors", "colours", "color", "colour":
				return docsColors(console, asJSON)
			case "sizes", "size", "pages":
				return docsSizes(console, asJSON)
			}
			topics := []string{"props", "elements", "errors", "colors", "sizes"}
			if suggestions := pulp.Suggest(topic, topics); len(suggestions) > 0 {
				return usageError{fmt.Errorf("unknown topic %q. %s", topic,
					pulp.FormatSuggestions("topic", suggestions))}
			}
			return usageError{fmt.Errorf("unknown topic %q; try %s", topic, strings.Join(topics, ", "))}
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the reference as JSON")
	return cmd
}

func docsProps(console *ui.Console, name string, asJSON bool) error {
	if name != "" {
		return docsOneProp(console, name, asJSON)
	}
	if asJSON {
		type entry struct {
			Name      string   `json:"name"`
			Kind      string   `json:"kind"`
			Default   string   `json:"default,omitempty"`
			Inherited bool     `json:"inherited"`
			AppliesTo []string `json:"appliesTo,omitempty"`
			Enum      []string `json:"values,omitempty"`
			Doc       string   `json:"doc"`
		}
		var out []entry
		for _, id := range schema.AllPropIDs() {
			def := schema.Def(id)
			out = append(out, entry{def.Name, def.Kind.String(), def.Default, def.Inherited, def.AppliesTo, def.Enum, def.Doc})
		}
		return encodeJSON(console.Out, out)
	}

	w := tabwriter.NewWriter(console.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PROPERTY\tTYPE\tDEFAULT\tINHERITS\tDESCRIPTION")
	for _, id := range schema.AllPropIDs() {
		def := schema.Def(id)
		inherits := ""
		if def.Inherited {
			inherits = "yes"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", def.Name, def.Kind, def.Default, inherits, firstSentence(def.Doc))
	}
	return w.Flush()
}

func docsOneProp(console *ui.Console, name string, asJSON bool) error {
	id, ok := schema.Lookup(name)
	if !ok {
		message := fmt.Sprintf("no property named %q", name)
		if suggestions := pulp.Suggest(name, schema.PropertyNames()); len(suggestions) > 0 {
			message += ". " + pulp.FormatSuggestions("property", suggestions)
		}
		return usageError{fmt.Errorf("%s", message)}
	}
	def := schema.Def(id)
	if asJSON {
		return encodeJSON(console.Out, def)
	}

	p := console.Palette
	fmt.Fprintf(console.Out, "\n  %s  %s\n\n", p.Heading.Render(def.Name), p.Dim.Render(def.Kind.String()))
	fmt.Fprintf(console.Out, "  %s\n\n", wrapText(def.Doc, 76, "  "))
	if def.Default != "" {
		fmt.Fprintf(console.Out, "  %s     %s\n", p.Dim.Render("default"), def.Default)
	}
	fmt.Fprintf(console.Out, "  %s   %s\n", p.Dim.Render("inherits"), yesNo(def.Inherited))
	if len(def.Enum) > 0 {
		fmt.Fprintf(console.Out, "  %s      %s\n", p.Dim.Render("values"), strings.Join(def.Enum, ", "))
	}
	applies := "every element"
	if len(def.AppliesTo) > 0 {
		applies = strings.Join(def.AppliesTo, ", ")
	}
	fmt.Fprintf(console.Out, "  %s  %s\n\n", p.Dim.Render("applies to"), applies)
	return nil
}

func docsElements(console *ui.Console, asJSON bool) error {
	if asJSON {
		type entry struct {
			Name      string `json:"name"`
			Container bool   `json:"container"`
			Doc       string `json:"doc"`
		}
		var out []entry
		for _, name := range schema.ElementNames() {
			def, _ := schema.Element(name)
			out = append(out, entry{def.Name, def.Container, def.Doc})
		}
		return encodeJSON(console.Out, out)
	}
	w := tabwriter.NewWriter(console.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ELEMENT\tDESCRIPTION")
	for _, name := range schema.ElementNames() {
		def, _ := schema.Element(name)
		fmt.Fprintf(w, "%s\t%s\n", def.Name, def.Doc)
	}
	return w.Flush()
}

func docsErrors(console *ui.Console, code string, asJSON bool) error {
	if code == "" {
		if asJSON {
			return encodeJSON(console.Out, docs.AllErrors())
		}
		w := tabwriter.NewWriter(console.Out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "CODE\tMEANING")
		for _, doc := range docs.AllErrors() {
			fmt.Fprintf(w, "%s\t%s\n", doc.Code, doc.Title)
		}
		return w.Flush()
	}

	upper := strings.ToUpper(code)
	doc, ok := docs.LookupError(upper)
	if !ok {
		message := fmt.Sprintf("no diagnostic named %q", code)
		if suggestions := pulp.Suggest(upper, docs.ErrorCodes()); len(suggestions) > 0 {
			message += ". " + pulp.FormatSuggestions("code", suggestions)
		}
		return usageError{fmt.Errorf("%s", message)}
	}
	if asJSON {
		return encodeJSON(console.Out, doc)
	}

	p := console.Palette
	fmt.Fprintf(console.Out, "\n  %s  %s\n\n", p.Heading.Render(doc.Code), doc.Title)
	fmt.Fprintf(console.Out, "%s\n\n", wrapText(doc.Explanation, 74, "  "))
	if doc.Fix != "" {
		fmt.Fprintf(console.Out, "  %s\n%s\n\n", p.Dim.Render("what to do"), wrapText(doc.Fix, 74, "  "))
	}
	if doc.Example != "" {
		fmt.Fprintf(console.Out, "  %s\n\n", p.Dim.Render("example"))
		for _, line := range strings.Split(doc.Example, "\n") {
			fmt.Fprintf(console.Out, "    %s\n", p.Code.Render(line))
		}
		fmt.Fprintln(console.Out)
	}
	return nil
}

func docsColors(console *ui.Console, asJSON bool) error {
	names := pulp.NamedColorNames()
	if asJSON {
		return encodeJSON(console.Out, names)
	}
	fmt.Fprintln(console.Out, "Colour forms:")
	for _, form := range []string{
		"#ddd, #dddd, #dddddd, #dddddd80    hex, 3/4/6/8 digits",
		"gray(0.85), gray(0.85, 0.5)        DeviceGray — preferred for print",
		"rgb(31 111 235), rgb(31, 111, 235) DeviceRGB",
		"cmyk(0 0 0 0.13)                   DeviceCMYK, passed through untouched",
		"transparent                        no ink at all",
	} {
		fmt.Fprintf(console.Out, "  %s\n", form)
	}
	fmt.Fprintf(console.Out, "\n%d CSS colour names:\n", len(names))
	for i, name := range names {
		fmt.Fprintf(console.Out, "  %-22s", name)
		if i%4 == 3 {
			fmt.Fprintln(console.Out)
		}
	}
	fmt.Fprintln(console.Out)
	return nil
}

func docsSizes(console *ui.Console, asJSON bool) error {
	names := compile.PageSizeNames()
	if asJSON {
		return encodeJSON(console.Out, names)
	}
	w := tabwriter.NewWriter(console.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tINCHES\tMILLIMETRES")
	for _, name := range names {
		size := compile.NamedPageSize(name)
		fmt.Fprintf(w, "%s\t%.2f × %.2f\t%.0f × %.0f\n",
			name, size.Width.Inches(), size.Height.Inches(), size.Width.Mm(), size.Height.Mm())
	}
	fmt.Fprintln(w, "\nA custom size is a width and height pair, as in `size: 200mm 300mm`.")
	return w.Flush()
}

// firstSentence trims a doc string to its first sentence, for table rows.
func firstSentence(text string) string {
	if i := strings.Index(text, ". "); i >= 0 {
		return text[:i+1]
	}
	return text
}

// wrapText wraps prose to a width with a fixed indent, for the detail views.
func wrapText(text string, width int, indent string) string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return ""
	}
	var (
		out  strings.Builder
		line = indent
	)
	for _, word := range words {
		if len(line)+len(word)+1 > width && len(line) > len(indent) {
			out.WriteString(line + "\n")
			line = indent
		}
		if len(line) > len(indent) {
			line += " "
		}
		line += word
	}
	out.WriteString(line)
	return out.String()
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
