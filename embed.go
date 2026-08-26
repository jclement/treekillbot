// Package treekillbot exists for one reason: to embed the example documents.
//
// `go:embed` patterns are resolved relative to the directory of the file that
// declares them and cannot reach upward, so a package under internal/ cannot
// embed examples/ where it sits. Rather than duplicate the documents into
// internal/, or turn examples/ into build output that nobody can browse on
// GitHub, the directive lives here at the module root and internal/examples
// reads through it.
//
// The examples are the finished, designed documents: the ones worth printing
// and reading. The starting points `treekillbot new` scaffolds live in
// internal/templates and are deliberately different — see that package.
package treekillbot

import "embed"

// Examples holds every shipped .pulp document, under its repository path:
// "examples/weekly.pulp", "examples/stress/01-units.pulp".
//
// Only .pulp files are embedded. The stress set's README is documentation for a
// reader of the repository, not something the binary has any use for.
//
//go:embed examples/*.pulp examples/stress/*.pulp
var Examples embed.FS
