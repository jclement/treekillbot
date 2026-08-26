// Page sizes, in ticks, with the named ones the CLI accepts.
//
// The ISO sizes are computed from their millimetre definitions through
// geom.Mm rather than copied from a table of rounded points. A4 lands on 9524
// ticks (595.25pt), 0.009mm off the ISO nominal — invisible on paper, and the
// price of it is that a page width is an exact number of ticks that a column
// grid can divide cleanly. See DESIGN.md D1.
package pdfout

import (
	"sort"
	"strings"

	"github.com/jclement/treekillbot/internal/geom"
)

// PageSize is a page's media box. Name is carried along for diagnostics and
// for the build summary; it is empty for a custom size.
type PageSize struct {
	Name string
	W, H geom.Tick
}

// The named sizes. US sizes are exact in points by definition, so they are
// written that way; the A series comes from millimetres.
var namedPageSizes = map[string]PageSize{
	"letter":  {Name: "letter", W: geom.Pt(612), H: geom.Pt(792)},
	"legal":   {Name: "legal", W: geom.Pt(612), H: geom.Pt(1008)},
	"tabloid": {Name: "tabloid", W: geom.Pt(792), H: geom.Pt(1224)},
	"a3":      {Name: "a3", W: geom.Mm(297), H: geom.Mm(420)},
	"a4":      {Name: "a4", W: geom.Mm(210), H: geom.Mm(297)},
	"a5":      {Name: "a5", W: geom.Mm(148), H: geom.Mm(210)},
	"a6":      {Name: "a6", W: geom.Mm(105), H: geom.Mm(148)},
}

// NamedPageSize looks a size up by name, case-insensitively. The second return
// is false for an unknown name, which the caller turns into a source error
// listing PageSizeNames.
func NamedPageSize(name string) (PageSize, bool) {
	size, ok := namedPageSizes[strings.ToLower(strings.TrimSpace(name))]
	return size, ok
}

// PageSizeNames returns the accepted names in sorted order, for the "unknown
// page size, try one of ..." diagnostic.
func PageSizeNames() []string {
	names := make([]string, 0, len(namedPageSizes))
	for name := range namedPageSizes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// CustomPageSize is an arbitrary media box, for the sizes nobody named —
// a 5.5x8.5in planner insert, an A5 slip in a Filofax.
func CustomPageSize(w, h geom.Tick) PageSize {
	return PageSize{W: w, H: h}
}

// Landscape returns the size with its long edge horizontal, and Portrait with
// it vertical. Both are idempotent, so a document can ask for landscape
// without knowing which way the size was written.
func (p PageSize) Landscape() PageSize {
	if p.W >= p.H {
		return p
	}
	p.W, p.H = p.H, p.W
	return p
}

// Portrait returns the size with its long edge vertical.
func (p PageSize) Portrait() PageSize {
	if p.H >= p.W {
		return p
	}
	p.W, p.H = p.H, p.W
	return p
}

// IsValid reports whether the size encloses any area. A zero page is an
// authoring error worth catching before it reaches the PDF writer, where it
// becomes an unopenable file rather than a message.
func (p PageSize) IsValid() bool { return p.W > 0 && p.H > 0 }
