// Named page sizes.
//
// Sizes are stored in ticks, computed from their defining unit — inches for the
// North American sizes, millimetres for ISO — rather than from a table of
// rounded point values. A4 lands on 595.25pt rather than the nominal 595.276,
// and the 0.009mm difference is invisible on paper while an exactly divisible
// grid is not.
package compile

import (
	"sort"
	"strings"

	"github.com/jclement/treekillbot/internal/geom"
)

var pageSizes = map[string]PageSize{
	"letter":     {Width: geom.In(8.5), Height: geom.In(11), Name: "letter"},
	"legal":      {Width: geom.In(8.5), Height: geom.In(14), Name: "legal"},
	"tabloid":    {Width: geom.In(11), Height: geom.In(17), Name: "tabloid"},
	"executive":  {Width: geom.In(7.25), Height: geom.In(10.5), Name: "executive"},
	"halfletter": {Width: geom.In(5.5), Height: geom.In(8.5), Name: "halfletter"},
	"index3x5":   {Width: geom.In(3), Height: geom.In(5), Name: "index3x5"},
	"index4x6":   {Width: geom.In(4), Height: geom.In(6), Name: "index4x6"},
	"index5x8":   {Width: geom.In(5), Height: geom.In(8), Name: "index5x8"},
	"a3":         {Width: geom.Mm(297), Height: geom.Mm(420), Name: "a3"},
	"a4":         {Width: geom.Mm(210), Height: geom.Mm(297), Name: "a4"},
	"a5":         {Width: geom.Mm(148), Height: geom.Mm(210), Name: "a5"},
	"a6":         {Width: geom.Mm(105), Height: geom.Mm(148), Name: "a6"},
	"b5":         {Width: geom.Mm(176), Height: geom.Mm(250), Name: "b5"},
	// The two pocket-notebook sizes worth having by name, because a planner
	// page for one is a thing people actually print.
	"a5slim":     {Width: geom.Mm(105), Height: geom.Mm(210), Name: "a5slim"},
	"pocket":     {Width: geom.Mm(90), Height: geom.Mm(140), Name: "pocket"},
	"travellers": {Width: geom.Mm(110), Height: geom.Mm(210), Name: "travellers"},
}

// aliases map common alternative spellings onto canonical names.
var pageSizeAliases = map[string]string{
	"us-letter": "letter",
	"usletter":  "letter",
	"ansi-a":    "letter",
	"ledger":    "tabloid",
	"half":      "halfletter",
	"3x5":       "index3x5",
	"4x6":       "index4x6",
	"5x8":       "index5x8",
}

// lookupPageSize resolves a name, applying aliases and ignoring case, spaces
// and hyphens so that "US Letter", "us-letter" and "usletter" all work.
func lookupPageSize(name string) (PageSize, bool) {
	key := strings.ToLower(strings.TrimSpace(name))
	key = strings.NewReplacer(" ", "", "_", "").Replace(key)
	if canonical, ok := pageSizeAliases[key]; ok {
		key = canonical
	}
	if size, ok := pageSizes[key]; ok {
		return size, true
	}
	// Try again with hyphens removed, so "a-4" resolves.
	if size, ok := pageSizes[strings.ReplaceAll(key, "-", "")]; ok {
		return size, true
	}
	return PageSize{}, false
}

// NamedPageSize resolves a name, falling back to US Letter. The fallback is
// safe because an unknown name is reported separately by the compiler; this
// keeps rendering going so one bad property does not cascade.
func NamedPageSize(name string) PageSize {
	if size, ok := lookupPageSize(name); ok {
		return size
	}
	return pageSizes["letter"]
}

// PageSizeNames returns every canonical name, sorted, for suggestions and for
// `treekillbot docs sizes`.
func PageSizeNames() []string {
	out := make([]string, 0, len(pageSizes))
	for name := range pageSizes {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Rect returns the page as a rectangle at the origin.
func (p PageSize) Rect() geom.Rect {
	return geom.Rect{W: p.Width, H: p.Height}
}
