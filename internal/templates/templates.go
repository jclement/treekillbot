// Starter documents for `treekillbot new`.
//
// A template is an ordinary `.pulp` file embedded with go:embed. There is no
// substitution step and no placeholder syntax: what `new` writes is byte for
// byte what is in this directory, so the file someone opens is one they can
// also run through `check` and `fmt` unchanged, and the templates are covered
// by the same tests as any other document.
//
// Every template is required to render with zero error diagnostics the moment
// it is scaffolded — see templates_test.go. That is the whole contract. A
// starter that errors on first run teaches the reader that the tool is broken,
// which is worse than shipping no starter at all.
//
// The division of labour with internal/themes is deliberate and runs through
// every file here: a template owns structure — page size, panels, padding,
// borders, which decoration goes where — and leaves ink to the theme, so that
// `--theme dot` on any of them changes the drawing rather than nothing.
package templates

import (
	"embed"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jclement/treekillbot/internal/pulp"
)

// Extension is the suffix a scaffolded document gets. Templates are documents,
// so it is the ordinary one.
const Extension = ".pulp"

//go:embed builtin/*.pulp
var builtinFS embed.FS

const builtinDir = "builtin"

// Template is one starter document.
type Template struct {
	// Name is what `treekillbot new` takes.
	Name string
	// Description is the file's first comment line, which is where each
	// template states in a sentence what it is for.
	Description string
}

// Available returns every template, sorted by name. Sorted rather than in
// embed order because embed order is filesystem order, and a listing that
// changes when someone renames a file is a listing nobody trusts.
func Available() []Template {
	entries, err := builtinFS.ReadDir(builtinDir)
	if err != nil {
		return nil
	}
	out := make([]Template, 0, len(entries))
	for _, entry := range entries {
		name := strings.TrimSuffix(entry.Name(), Extension)
		text, err := builtinFS.ReadFile(builtinDir + "/" + entry.Name())
		if err != nil {
			continue
		}
		out = append(out, Template{Name: name, Description: describe(string(text))})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Names returns every template name, sorted. It is what an unknown name is
// ranked against.
func Names() []string {
	available := Available()
	out := make([]string, 0, len(available))
	for _, template := range available {
		out = append(out, template.Name)
	}
	return out
}

// Source returns a template's Pulp source, which is exactly what `new` writes.
// An unknown name is an error carrying a did-you-mean.
func Source(name string) (string, error) {
	if name == "" {
		return "", errors.New("a template name cannot be empty")
	}
	text, err := builtinFS.ReadFile(builtinDir + "/" + name + Extension)
	if err != nil {
		return "", unknownTemplate(name)
	}
	return string(text), nil
}

// unknownTemplate builds the error for a name nothing matched, using the same
// suggestion engine every other unknown name in the tool routes through.
func unknownTemplate(name string) error {
	names := Names()
	if suggestions := pulp.Suggest(name, names); len(suggestions) > 0 {
		return fmt.Errorf("unknown template %q. %s", name, pulp.FormatSuggestions("template", suggestions))
	}
	return fmt.Errorf("unknown template %q; available templates are %s", name, strings.Join(names, ", "))
}

// describe extracts a template's one-line description: the text of the file's
// first comment line. It is the same convention internal/themes uses, kept as
// its own copy rather than a shared package because the two are siblings and
// neither should depend on the other for twelve lines.
func describe(text string) string {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "#") {
			return ""
		}
		return strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
	}
	return ""
}
