// Package examples serves the example documents compiled into the binary.
//
// Someone who installed treekillbot from Homebrew has no repository to look at,
// so the examples have to travel with the binary or they may as well not exist.
// They are the finished, designed documents — the ones worth printing — as
// distinct from the deliberately plain starting points in internal/templates.
package examples

import (
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	treekillbot "github.com/jclement/treekillbot"
	"github.com/jclement/treekillbot/internal/pulp"
)

// Extension is the file extension of a Pulp document.
const Extension = ".pulp"

// Example is one shipped document.
type Example struct {
	// Name is how it is asked for: "weekly", "stress/01-units".
	Name string
	// Description is the first line of the file's header comment.
	Description string
	// Group separates the printable reference sheets from the rest, so a
	// listing can lead with the documents most people want.
	Group string
}

// The two groups a shipped document can belong to.
const (
	GroupDocument = "document"
	GroupStress   = "stress"
)

// Available returns every example, documents first and then the stress sheets,
// each group sorted by name. Sorted because a listing whose order depends on
// filesystem or map iteration is a listing that changes for no reason.
func Available() []Example {
	var out []Example
	_ = fs.WalkDir(treekillbot.Examples, "examples", func(p string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(p, Extension) {
			return nil
		}
		name := strings.TrimSuffix(strings.TrimPrefix(p, "examples/"), Extension)
		data, err := treekillbot.Examples.ReadFile(p)
		if err != nil {
			return nil
		}
		group := GroupDocument
		if strings.HasPrefix(name, "stress/") || name == "stress-test" {
			group = GroupStress
		}
		out = append(out, Example{Name: name, Description: describe(string(data)), Group: group})
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group == GroupDocument
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Names returns every example name, in the same order as Available.
func Names() []string {
	available := Available()
	out := make([]string, 0, len(available))
	for _, example := range available {
		out = append(out, example.Name)
	}
	return out
}

// Source returns an example's Pulp source.
//
// An unknown name gets a did-you-mean through the shared suggestion engine, so
// a mistyped example behaves like a mistyped property.
func Source(name string) (string, error) {
	clean := path.Clean(strings.TrimSuffix(name, Extension))
	// A name becomes a path segment, so it must not be able to climb out.
	if clean == "." || strings.HasPrefix(clean, "..") || path.IsAbs(clean) {
		return "", unknownExample(name)
	}
	data, err := treekillbot.Examples.ReadFile("examples/" + clean + Extension)
	if err != nil {
		return "", unknownExample(name)
	}
	return string(data), nil
}

// unknownExample builds the error for a name that does not resolve.
func unknownExample(name string) error {
	if suggestions := pulp.Suggest(name, Names()); len(suggestions) > 0 {
		return fmt.Errorf("unknown example %q. %s", name, pulp.FormatSuggestions("example", suggestions))
	}
	return fmt.Errorf("unknown example %q; run `treekillbot examples` for the list", name)
}

// describe returns the first line of a document's header comment, which is the
// convention every shipped document follows.
func describe(source string) string {
	for _, line := range strings.Split(source, "\n") {
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
