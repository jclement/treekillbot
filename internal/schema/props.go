// Resolved property sets.
//
// A Props is the fully cascaded answer to "what are this node's properties?".
// It is a fixed-size array indexed by PropID plus a bitmask of which entries
// were explicitly set, rather than a map, for two reasons: the cascade becomes
// a loop over integers, and no map iteration order can reach the output. The
// second reason is the load-bearing one — see DESIGN.md section 4.
package schema

import (
	"strings"

	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/paint"
	"github.com/jclement/treekillbot/internal/pulp"
)

// setMaskWords is how many uint64s are needed to hold one bit per property.
const setMaskWords = (NumProps + 63) / 64

// Props holds one node's resolved properties.
type Props struct {
	values [NumProps][]pulp.Value
	set    [setMaskWords]uint64
	// explicit marks the properties an author stated directly on this node or
	// through a style bundle, as opposed to ones that arrived from a `defaults`
	// block. The distinction exists to fix a real CSS gotcha — see
	// OverrideInherited.
	explicit [setMaskWords]uint64
}

// NewProps returns an empty property set.
func NewProps() *Props { return &Props{} }

// Has reports whether a property was explicitly set.
func (p *Props) Has(id PropID) bool {
	if id == PInvalid || int(id) >= NumProps {
		return false
	}
	return p.set[id/64]&(1<<(id%64)) != 0
}

// Set records a property's values as a cascaded default: present, but not an
// explicit statement by the author about this node.
func (p *Props) Set(id PropID, values []pulp.Value) {
	if id == PInvalid || int(id) >= NumProps {
		return
	}
	p.values[id] = values
	p.set[id/64] |= 1 << (id % 64)
}

// SetExplicit records a property the author stated directly on this node or via
// a style bundle. Explicit values are the ones that propagate down the tree in
// preference to `defaults` blocks.
func (p *Props) SetExplicit(id PropID, values []pulp.Value) {
	p.Set(id, values)
	if id != PInvalid && int(id) < NumProps {
		p.explicit[id/64] |= 1 << (id % 64)
	}
}

// IsExplicit reports whether a property was stated directly rather than
// inherited from a defaults block.
func (p *Props) IsExplicit(id PropID) bool {
	if id == PInvalid || int(id) >= NumProps {
		return false
	}
	return p.explicit[id/64]&(1<<(id%64)) != 0
}

// Values returns a property's raw values, or nil when unset.
func (p *Props) Values(id PropID) []pulp.Value {
	if !p.Has(id) {
		return nil
	}
	return p.values[id]
}

// First returns a property's first value and whether it was set.
func (p *Props) First(id PropID) (pulp.Value, bool) {
	vs := p.Values(id)
	if len(vs) == 0 {
		return pulp.Value{}, false
	}
	return vs[0], true
}

// Clone returns an independent copy, used when a subtree needs to layer its own
// defaults over an inherited set without disturbing its siblings.
func (p *Props) Clone() *Props {
	out := &Props{set: p.set, explicit: p.explicit}
	for id := PropID(1); id < numProps; id++ {
		if p.Has(id) {
			out.values[id] = p.values[id]
		}
	}
	return out
}

// MergeFrom copies every property set in other into p, overwriting. This is one
// step of the cascade, and the caller applies the steps in DESIGN.md section 3's
// order: defaults, then typed defaults, then styles, then direct properties.
func (p *Props) MergeFrom(other *Props) {
	if other == nil {
		return
	}
	for id := PropID(1); id < numProps; id++ {
		if !other.Has(id) {
			continue
		}
		if other.IsExplicit(id) {
			p.SetExplicit(id, other.values[id])
			continue
		}
		p.Set(id, other.values[id])
	}
}

// MergeAsExplicit copies every property set in other into p, marking them all
// explicit. This is how a `style` bundle applies: naming a style is a
// deliberate statement about this node, so its properties carry the same weight
// as ones written on the node directly.
func (p *Props) MergeAsExplicit(other *Props) {
	if other == nil {
		return
	}
	for id := PropID(1); id < numProps; id++ {
		if other.Has(id) {
			p.SetExplicit(id, other.values[id])
		}
	}
}

// OverrideInherited copies a parent's EXPLICIT inheritable properties into p,
// overwriting values that arrived from a defaults block.
//
// This is where treekillbot deliberately parts company with CSS. In CSS a
// universal rule beats inheritance, so `* { font-size: 8pt }` silently defeats
// a size set on an ancestor — a gotcha people have complained about for
// twenty-five years. Here, a document-level `defaults` block is a baseline,
// and a property written on an ancestor is a deliberate statement about that
// whole subtree, so the ancestor wins.
//
// A value that reached the ancestor from a defaults block is NOT explicit, so
// it does not override a more nested defaults block further down. That keeps
// `defaults` composable while removing the surprise.
func (p *Props) OverrideInherited(parent *Props) {
	if parent == nil {
		return
	}
	for id := PropID(1); id < numProps; id++ {
		if !props[id].Inherited || !parent.Has(id) || !parent.IsExplicit(id) {
			continue
		}
		if p.IsExplicit(id) {
			continue // this node said something of its own; it wins
		}
		p.SetExplicit(id, parent.values[id])
	}
}

// InheritFrom copies the inheritable properties of a parent into p, but only
// where p has not set them itself. Non-inheritable properties are skipped
// entirely, which is what stops a section's height from becoming its panels'
// height.
func (p *Props) InheritFrom(parent *Props) {
	if parent == nil {
		return
	}
	for id := PropID(1); id < numProps; id++ {
		if !props[id].Inherited || p.Has(id) || !parent.Has(id) {
			continue
		}
		p.Set(id, parent.values[id])
	}
}

// SetIDs returns every property explicitly set, in table order, for
// --explain-property and for the debug dump.
func (p *Props) SetIDs() []PropID {
	var out []PropID
	for id := PropID(1); id < numProps; id++ {
		if p.Has(id) {
			out = append(out, id)
		}
	}
	return out
}

// ---- Typed accessors ----
//
// Each accessor takes the fallback the caller wants when the property is unset
// or holds the wrong kind of value. Validation has already reported anything
// malformed by the time these run, so they never report errors themselves;
// returning the fallback keeps rendering going so that one bad property does
// not cascade into a hundred follow-on complaints.

// Tick returns an absolute length.
func (p *Props) Tick(id PropID, fallback geom.Tick) geom.Tick {
	v, ok := p.First(id)
	if !ok || v.Kind != pulp.KindLength {
		return fallback
	}
	return v.Length
}

// Dimension returns a size property as a Dimension.
func (p *Props) Dimension(id PropID, fallback geom.Dimension) geom.Dimension {
	v, ok := p.First(id)
	if !ok {
		return fallback
	}
	switch v.Kind {
	case pulp.KindLength:
		return geom.Fixed(v.Length)
	case pulp.KindPercent:
		return geom.Dimension{Mode: geom.SizePercent, Pct: v.Pct}
	case pulp.KindFill:
		return geom.Dimension{Mode: geom.SizeFill, Weight: v.Weight}
	case pulp.KindAuto:
		return geom.Auto
	}
	return fallback
}

// Color returns a colour property.
func (p *Props) Color(id PropID, fallback paint.Color) paint.Color {
	v, ok := p.First(id)
	if !ok || v.Kind != pulp.KindColor {
		return fallback
	}
	return v.Color
}

// Str returns a string or keyword property.
func (p *Props) Str(id PropID, fallback string) string {
	v, ok := p.First(id)
	if !ok {
		return fallback
	}
	switch v.Kind {
	case pulp.KindString, pulp.KindKeyword, pulp.KindInterp:
		return v.Str
	case pulp.KindNone:
		return "none"
	case pulp.KindAuto:
		return "auto"
	}
	return v.Raw
}

// Enum returns a keyword property, normalising the spellings that mean the same
// thing so that callers switch on one form. British and American spellings of
// centre are both accepted because refusing one is a pointless papercut.
func (p *Props) Enum(id PropID, fallback string) string {
	s := p.Str(id, fallback)
	if canonical, isAlias := CanonicalEnum(id, strings.ToLower(s)); isAlias {
		return canonical
	}
	switch s {
	case "centre":
		return "center"
	case "normal":
		if id == PFontWeight {
			return "regular"
		}
	}
	return s
}

// Bool returns a boolean property.
func (p *Props) Bool(id PropID, fallback bool) bool {
	v, ok := p.First(id)
	if !ok || v.Kind != pulp.KindBool {
		return fallback
	}
	return v.Bool
}

// Num returns a unitless number property.
func (p *Props) Num(id PropID, fallback float64) float64 {
	v, ok := p.First(id)
	if !ok || (v.Kind != pulp.KindNumber && v.Kind != pulp.KindPercent) {
		return fallback
	}
	if v.Kind == pulp.KindPercent {
		return float64(v.Pct) / 10000
	}
	return v.Num
}

// Int returns a whole-number property.
func (p *Props) Int(id PropID, fallback int) int {
	v, ok := p.First(id)
	if !ok || v.Kind != pulp.KindNumber {
		return fallback
	}
	return int(v.Num + 0.5)
}

// Edges returns a per-side length property, applying the CSS shorthand arity:
// one value sets all four sides, two set vertical then horizontal, three set
// top, horizontal, bottom, and four set top, right, bottom, left.
func (p *Props) Edges(id PropID, fallback geom.Edges) geom.Edges {
	vs := p.Values(id)
	lengths := make([]geom.Tick, 0, 4)
	for _, v := range vs {
		if v.Kind != pulp.KindLength {
			continue
		}
		lengths = append(lengths, v.Length)
	}
	edges := fallback
	switch len(lengths) {
	case 1:
		edges = geom.EdgesAll(lengths[0])
	case 2:
		edges = geom.EdgesVH(lengths[0], lengths[1])
	case 3:
		edges = geom.Edges{Top: lengths[0], Right: lengths[1], Bottom: lengths[2], Left: lengths[1]}
	case 4:
		edges = geom.Edges{Top: lengths[0], Right: lengths[1], Bottom: lengths[2], Left: lengths[3]}
	}
	return p.applySideOverrides(id, edges)
}

// applySideOverrides lets `padding-top` refine `padding` on one edge.
//
// A per-side property carries no default, so an unset side leaves the shorthand
// alone. Giving them defaults would mean every shorthand was silently overridden
// by four zeroes, which is the sort of bug that only shows up once someone
// finally uses the shorthand.
func (p *Props) applySideOverrides(id PropID, edges geom.Edges) geom.Edges {
	sides, ok := SideOverrides(id)
	if !ok {
		return edges
	}
	targets := [4]*geom.Tick{&edges.Top, &edges.Right, &edges.Bottom, &edges.Left}
	for i, side := range sides {
		if !p.Has(side) {
			continue
		}
		if v, ok := p.First(side); ok && v.Kind == pulp.KindLength {
			*targets[i] = v.Length
		}
	}
	return edges
}
