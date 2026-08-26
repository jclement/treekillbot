// The variable-scope seam.
//
// The compiler needs to interpolate `{week.number}` into text and to iterate
// `for day in week.days`, but it does not need to know how dates work. This
// interface is the whole of its dependency on the variable system, which keeps
// the compiler testable with a stub and lets the date machinery evolve without
// touching tree construction.
package compile

import (
	"github.com/jclement/treekillbot/internal/pulp"
	"github.com/jclement/treekillbot/internal/vars"
)

// Scope resolves variables and manages lexical binding.
type Scope interface {
	// Push and Pop open and close a lexical scope. `let` bindings and loop
	// variables live in the innermost one and must not leak to siblings.
	Push()
	Pop()

	// Bind introduces a lexically scoped variable — a `let` or a loop binding.
	// These sit above every document-wide layer.
	Bind(name string, value vars.Value)

	// Define sets a document-wide variable at a given layer. A `vars` block
	// uses this rather than Bind so that --var still outranks it.
	Define(name string, value vars.Value, layer vars.Layer)

	// Declare registers a variable the document requires but does not define,
	// to be filled from --var or TKB_VAR_<NAME>. It also permits {env.<NAME>},
	// which is how a document opts in to reading one environment variable
	// without opening the door to all of them (DESIGN.md D11).
	Declare(name string, span pulp.Span, src *pulp.Source, diags *pulp.Diagnostics)

	// Interpolate substitutes {…} references in text. Offsets in span are used
	// to place diagnostics under the offending reference.
	Interpolate(text string, span pulp.Span, src *pulp.Source, diags *pulp.Diagnostics) string

	// List resolves a dotted path to an iterable, for `for x in <path>`.
	List(path string) ([]vars.Value, bool)
}

// nullScope is used when a document is compiled without variable support, as
// `fmt` and a bare `check` do. Text passes through untouched and no list
// resolves, so a loop over an unknown path expands to nothing rather than
// failing — which is what a syntax-only check wants.
type nullScope struct{}

// NullScope returns a scope that performs no substitution.
func NullScope() Scope { return nullScope{} }

func (nullScope) Push()                                 {}
func (nullScope) Pop()                                  {}
func (nullScope) Bind(string, vars.Value)               {}
func (nullScope) Define(string, vars.Value, vars.Layer) {}

func (nullScope) Declare(string, pulp.Span, *pulp.Source, *pulp.Diagnostics) {}
func (nullScope) List(string) ([]vars.Value, bool)                           { return nil, false }

func (nullScope) Interpolate(text string, _ pulp.Span, _ *pulp.Source, _ *pulp.Diagnostics) string {
	return text
}
