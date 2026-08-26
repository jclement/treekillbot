// Distributing space along one axis.
//
// Every sizing decision in treekillbot — column widths across a row, child
// heights down a section, cell sizes in a grid — is the same problem: given an
// available extent, a gap, and a list of children that each want a fixed
// length, a percentage, a share of the leftover, or just enough for their
// content, decide who gets what.
//
// Solving it once, here, is what makes the guarantee in DESIGN.md D2 hold
// everywhere rather than in most places. The post-condition of ResolveAxis is
// that the returned sizes plus the gaps sum to exactly the available extent
// whenever the children are flexible enough to absorb it — not to within a
// rounding error, exactly, because every step is integer arithmetic routed
// through geom.DistributeTicks.
package layout

import "github.com/jclement/treekillbot/internal/geom"

// AxisItem is one child's demand on an axis.
type AxisItem struct {
	Dim geom.Dimension
	// Natural is the child's intrinsic extent, already measured. It is what an
	// `auto` child receives, and the floor a shrinking child is measured
	// against.
	Natural geom.Tick
	Min     geom.Tick
	Max     geom.Tick // zero means unbounded
}

// AxisResult is the outcome of resolving an axis.
type AxisResult struct {
	Sizes []geom.Tick
	// Used is the total consumed, including gaps.
	Used geom.Tick
	// Leftover is the space no child claimed. It is positive only when nothing
	// on the axis was flexible, and the caller decides what to do with it
	// (justification).
	Leftover geom.Tick
	// Overflow is how much the children exceed the available extent, after
	// shrinking whatever was allowed to shrink. Non-zero means the document
	// does not fit, which is an error by default (DESIGN.md D9).
	Overflow geom.Tick
}

// ResolveAxis distributes avail among items separated by gap.
//
// The pipeline, in order, with the reasoning for the order:
//
//  1. Gaps come off the top. They are structural, not negotiable.
//  2. Fixed children take exactly what they asked for. An explicit length is
//     the author's statement and nothing else in the algorithm may override it.
//  3. Percentages resolve against the full content extent, not against what is
//     left after fixed children. That is what makes a percentage mean the same
//     thing regardless of what it sits beside, which is the entire point of
//     page-size-independent layout.
//  4. Auto children take their measured natural size.
//  5. Whatever remains goes to fill children, by weight.
//  6. Minimum and maximum bounds are applied by freezing violators and
//     redistributing to the rest, repeating until nothing else violates.
//
// Over-commitment shrinks only auto children, never fixed or percentage ones:
// those are explicit statements, and silently ignoring them would hide the
// document bug that a form which does not fit represents.
func ResolveAxis(avail, gap geom.Tick, items []AxisItem) AxisResult {
	n := len(items)
	if n == 0 {
		return AxisResult{Leftover: avail}
	}

	gaps := gap * geom.Tick(n-1)
	content := avail - gaps
	if content < 0 {
		content = 0
	}

	sizes := make([]geom.Tick, n)
	frozen := make([]bool, n)

	// Steps 2 and 4: fixed and auto children take their size directly.
	var claimed geom.Tick
	for i, item := range items {
		switch item.Dim.Mode {
		case geom.SizeFixed:
			// Clamped here, because a fixed child is frozen against later
			// adjustment and applyBounds would never see it — so `height: 120pt`
			// with `max-height: 44pt` silently stayed 120pt.
			sizes[i] = clampItem(item, item.Dim.Length)
			frozen[i] = true
			claimed += sizes[i]
		case geom.SizeAuto:
			sizes[i] = clampItem(item, item.Natural)
			claimed += sizes[i]
		}
	}

	// Step 3: percentages, apportioned exactly among themselves out of a budget
	// that is itself an exact share of the content extent.
	claimed += resolvePercentages(content, items, sizes, frozen)

	// Step 5: the leftover goes to fill children by weight.
	remaining := content - claimed
	fillWeights := make([]int32, n)
	anyFill := false
	for i, item := range items {
		if item.Dim.Mode == geom.SizeFill {
			fillWeights[i] = item.Dim.Weight
			anyFill = true
		}
	}
	if anyFill {
		share := remaining
		if share < 0 {
			share = 0
		}
		for i, s := range geom.DistributeTicks(share, fillWeights) {
			if fillWeights[i] > 0 {
				sizes[i] = s
			}
		}
		remaining -= share
	}

	// Step 6: honour min and max by freezing whoever violates and redistributing
	// to the rest. Bounded by n iterations because each pass freezes at least
	// one child, which is the same argument flexbox's resolution loop makes.
	applyBounds(content, items, sizes, frozen, fillWeights)

	// Over-commitment: take it back from auto children only, proportionally,
	// floored at each one's minimum.
	var total geom.Tick
	for _, s := range sizes {
		total += s
	}
	overflow := geom.Tick(0)
	if total > content {
		total = shrinkAutos(total-content, items, sizes)
		if total > content {
			overflow = total - content
		}
	}

	used := total + gaps
	leftover := geom.Tick(0)
	if !anyFill && content > total {
		leftover = content - total
	}
	return AxisResult{Sizes: sizes, Used: used, Leftover: leftover, Overflow: overflow}
}

// resolvePercentages assigns every percentage child its share and returns the
// total assigned.
//
// The two-stage apportionment is what keeps `30% + 70%` exact. The percentages
// first claim a budget that is an exact share of the content extent, then split
// that budget exactly among themselves. When they sum to 100%, the budget is
// the whole content extent and no tick can go missing between the two stages.
func resolvePercentages(content geom.Tick, items []AxisItem, sizes []geom.Tick, frozen []bool) geom.Tick {
	weights := make([]int32, len(items))
	var sumPct int32
	any := false
	for i, item := range items {
		if item.Dim.Mode != geom.SizePercent {
			continue
		}
		weights[i] = item.Dim.Pct
		sumPct += item.Dim.Pct
		any = true
	}
	if !any {
		return 0
	}

	budget := content
	if sumPct < 10000 {
		budget = geom.DistributeTicks(content, []int32{sumPct, 10000 - sumPct})[0]
	} else if sumPct > 10000 {
		// Over 100% is legal and means overlapping demands; each child still
		// gets its literal share of the content extent, and the overflow is
		// reported rather than quietly rescaled.
		budget = content.Scale(int64(sumPct), 10000)
	}

	var assigned geom.Tick
	for i, s := range geom.DistributeTicks(budget, weights) {
		if weights[i] == 0 {
			continue
		}
		sizes[i] = clampItem(items[i], s)
		frozen[i] = true
		assigned += sizes[i]
	}
	return assigned
}

// applyBounds freezes children that violate their min or max and redistributes
// the difference among the fill children that are still free.
func applyBounds(content geom.Tick, items []AxisItem, sizes []geom.Tick, frozen []bool, fillWeights []int32) {
	for pass := 0; pass < len(items); pass++ {
		violated := false
		var recovered geom.Tick
		for i, item := range items {
			if frozen[i] {
				continue
			}
			clamped := clampItem(item, sizes[i])
			if clamped == sizes[i] {
				continue
			}
			recovered += sizes[i] - clamped
			sizes[i] = clamped
			frozen[i] = true
			violated = true
		}
		if !violated || recovered == 0 {
			return
		}

		free := make([]int32, len(items))
		anyFree := false
		for i := range items {
			if !frozen[i] && fillWeights[i] > 0 {
				free[i] = fillWeights[i]
				anyFree = true
			}
		}
		if !anyFree {
			return
		}
		for i, extra := range geom.DistributeTicks(recovered, free) {
			if free[i] > 0 {
				sizes[i] += extra
			}
		}
	}
}

// shrinkAutos reclaims excess from auto children in proportion to how much each
// has above its minimum, and returns the new total.
//
// Only auto children shrink. A fixed length and a percentage are the author
// saying what they want; quietly overriding either would turn a document bug
// into a silently wrong page, which is the outcome DESIGN.md D9 exists to
// prevent.
func shrinkAutos(excess geom.Tick, items []AxisItem, sizes []geom.Tick) geom.Tick {
	slack := make([]int32, len(items))
	var totalSlack geom.Tick
	for i, item := range items {
		if item.Dim.Mode != geom.SizeAuto {
			continue
		}
		if s := sizes[i] - item.Min; s > 0 {
			// Slack is expressed in ticks but used as a weight, so it is capped
			// to keep DistributeTicks inside its documented range.
			slack[i] = int32(geom.MinTick(s, 1<<19))
			totalSlack += s
		}
	}
	if totalSlack > 0 {
		take := geom.MinTick(excess, totalSlack)
		for i, amount := range geom.DistributeTicks(take, slack) {
			if slack[i] > 0 {
				sizes[i] -= amount
			}
		}
	}

	var total geom.Tick
	for _, s := range sizes {
		total += s
	}
	return total
}

// clampItem applies a child's min and max bounds.
func clampItem(item AxisItem, size geom.Tick) geom.Tick {
	return geom.Clamp(size, item.Min, item.Max)
}

// Offsets converts resolved sizes into start positions along the axis, given a
// starting coordinate and the gap between children.
func Offsets(start geom.Tick, sizes []geom.Tick, gap geom.Tick) []geom.Tick {
	out := make([]geom.Tick, len(sizes))
	cursor := start
	for i, s := range sizes {
		out[i] = cursor
		cursor += s
		if i < len(sizes)-1 {
			cursor += gap
		}
	}
	return out
}
