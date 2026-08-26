// The row arithmetic shared by every decoration built out of horizontal rules:
// `ruled`, `checkbox`, and both bands of `cornell`.
//
// It lives in one function because the requirement is that a checkbox panel and
// a ruled panel of the same height and pitch put their lines in exactly the same
// places, so that two columns of a spread line up. Two implementations of that
// arithmetic would agree on the day they were written and not much longer.
package decor

import "github.com/jclement/treekillbot/internal/geom"

// rules returns the y of every writing rule inside band, top-down.
//
// Rule B (DESIGN.md D4): the returned y is the CENTRE of the stroke, so a rule
// covers [y-width/2, y+width/2] and changing line-width does not move it.
//
// The count is floored — n = floor(H / pitch) — and the leftover becomes
// padding rather than a stub of a line at the bottom. Ruled notebooks that end
// in a half-row look cheap, and this is the single highest-leverage aesthetic
// default in the tool. `line-partial: true` restores the legal pad that
// genuinely does run off the edge.
//
// Rule k is the baseline of writing row k, and row k occupies the band
// [y_(k-1), y_k]; the first rule therefore sits a full pitch below the top of
// the block, because there must be room to write above it.
func (p *params) rules(band geom.Rect) []geom.Tick {
	if p.pitch <= 0 || band.H <= 0 {
		return nil
	}

	n := int(band.H / p.pitch)
	leftover := band.H - geom.Tick(n)*p.pitch
	stub := p.partial && leftover > 0
	if p.count > 0 && p.count < n {
		n = p.count
		leftover = band.H - geom.Tick(n)*p.pitch
		stub = false
	}
	if n <= 0 && !stub {
		return nil
	}

	// `grow` gives up the declared pitch in exchange for the last rule landing
	// EXACTLY on the bottom edge — not 0.03pt above it — which is what an
	// address block whose rules must meet the panel border needs. It is the one
	// distribution that goes through DistributeTicks, because it is the one that
	// has to sum to the height.
	if p.distribute == "grow" && n > 0 {
		out := make([]geom.Tick, n)
		y := band.Y
		for i, share := range geom.DistributeEqual(band.H, n) {
			y += share
			out[i] = y
		}
		return out
	}

	offset := leftoverOffset(p.distribute, leftover)
	if stub {
		// The stub IS the leftover, so there is nothing left to pad with.
		offset = 0
	}

	out := make([]geom.Tick, 0, n+1)
	for k := 1; k <= n; k++ {
		out = append(out, band.Y+offset+geom.Tick(k)*p.pitch)
	}
	if stub {
		// The partial row is a real row and needs a rule to write on; it lands
		// on the bottom edge, which is where the paper runs out.
		out = append(out, band.Bottom())
	}
	return out
}

// leftoverOffset places the block of rules within its band.
//
// `center` is the default and it is what makes a panel look designed rather
// than truncated: the pitch the author tuned is preserved exactly and the
// remainder is split floor(leftover/2) above, the odd tick below.
func leftoverOffset(distribute string, leftover geom.Tick) geom.Tick {
	switch distribute {
	case "start":
		return 0
	case "end":
		return leftover
	default: // center, and anything the validator let through
		return leftover / 2
	}
}

// baselinesFrom converts a rule set into text baselines, dropping each one by
// the descent when `baseline-on-rule` is off.
func (p *params) baselinesFrom(rules []geom.Tick) []geom.Tick {
	if p.textDrop == 0 {
		return rules
	}
	out := make([]geom.Tick, len(rules))
	for i, y := range rules {
		out[i] = y - p.textDrop
	}
	return out
}

// rowNaturalHeight is the height a row-based decoration wants when its box is
// `auto`: the space its requested number of rules occupies, or 0 when no count
// was given and it should fill whatever it is handed.
func (p *params) rowNaturalHeight() geom.Tick {
	if p.count <= 0 || p.pitch <= 0 {
		return 0
	}
	return p.pitch * geom.Tick(p.count)
}
