// Exact partition of a length among weighted shares.
//
// This file contains the only division in the layout engine. Every place a
// container splits space among children — column widths, fill heights, ruled
// line spacing, justified word gaps — routes through DistributeTicks, and gets
// the guarantee that the parts sum to the whole exactly.
//
// The method is largest-remainder (Hamilton) apportionment, the same rule used
// to allocate legislative seats, and it is the reason "30% + 70%" lands on the
// content width to the tick rather than to within a rounding error.
package geom

import "sort"

// maxDistributeWeightSum bounds the weight sum so that total*weight cannot
// overflow int64. Real weight sums are tiny — 10000 for percentages expressed
// in basis points, 16 per "fill" unit, one per ruled line — so this ceiling is
// four orders of magnitude above anything a document can produce, and tripping
// it means a caller has passed something that is not a weight.
const maxDistributeWeightSum = int64(1) << 20

// DistributeTicks splits total among len(weights) shares in proportion to the
// weights, and guarantees that the returned slice sums to total EXACTLY.
//
// Each share receives floor(total*weight/sumWeights); the leftover ticks — of
// which there are always fewer than the number of shares — go one apiece to the
// shares with the largest truncated remainders. Ties are broken by lower index,
// so the result depends only on the inputs and never on map order, float
// rounding, or the order the caller happened to build the slice in.
//
// Non-positive weights receive nothing and are excluded from the apportionment,
// which lets callers pass a weight of zero for a collapsed child rather than
// filtering the slice first. If no weight is positive, every share is zero and
// total is dropped; the caller is responsible for not asking to distribute
// space among nothing.
//
// A negative total distributes negative shares, still summing exactly — this
// happens when an over-committed axis needs to take space back.
func DistributeTicks(total Tick, weights []int32) []Tick {
	out := make([]Tick, len(weights))
	if len(weights) == 0 {
		return out
	}

	var sumWeights int64
	positive := 0
	for _, w := range weights {
		if w > 0 {
			sumWeights += int64(w)
			positive++
		}
	}
	if sumWeights == 0 {
		return out
	}
	if sumWeights > maxDistributeWeightSum {
		panic("geom: DistributeTicks weight sum out of range; these are not weights")
	}
	if total.Abs() > MaxTick {
		panic("geom: DistributeTicks total out of range")
	}

	// Floor division with a non-negative remainder. Go's integer division
	// truncates toward zero, which for a negative total would bias shares
	// upward and leave a negative leftover; correcting to a true floor keeps
	// the leftover in [0, positive) for either sign.
	remainders := make([]int64, len(weights))
	var assigned Tick
	for i, w := range weights {
		if w <= 0 {
			continue
		}
		numerator := int64(total) * int64(w)
		quotient := numerator / sumWeights
		remainder := numerator % sumWeights
		if remainder < 0 {
			quotient--
			remainder += sumWeights
		}
		out[i] = Tick(quotient)
		remainders[i] = remainder
		assigned += Tick(quotient)
	}

	leftover := int(total - assigned)
	if leftover <= 0 {
		return out
	}

	// Rank the shares by remainder descending, index ascending. sort.SliceStable
	// is not enough on its own — the tie-break must be explicit, because a
	// stable sort only preserves the input order, and the input order is what
	// we are deliberately making load-bearing.
	order := make([]int, 0, positive)
	for i, w := range weights {
		if w > 0 {
			order = append(order, i)
		}
	}
	sort.Slice(order, func(a, b int) bool {
		ia, ib := order[a], order[b]
		if remainders[ia] != remainders[ib] {
			return remainders[ia] > remainders[ib]
		}
		return ia < ib
	})

	for k := 0; k < leftover && k < len(order); k++ {
		out[order[k]]++
	}
	return out
}

// DistributeEqual splits total into n equal shares that sum to total exactly,
// with the remainder ticks going to the earliest shares. This is the common
// case — evenly spaced rules, an unweighted row of day cells — and avoids
// building a slice of ones at every call site.
func DistributeEqual(total Tick, n int) []Tick {
	if n <= 0 {
		return nil
	}
	weights := make([]int32, n)
	for i := range weights {
		weights[i] = 1
	}
	return DistributeTicks(total, weights)
}

// CumulativeOffsets converts a slice of sizes plus a gap between each into the
// running start offset of every item, and the total consumed. Keeping this in
// one place means the "did I add the gap after the last item" question is
// answered once rather than at every container.
func CumulativeOffsets(sizes []Tick, gap Tick) (offsets []Tick, total Tick) {
	offsets = make([]Tick, len(sizes))
	for i, s := range sizes {
		offsets[i] = total
		total += s
		if i < len(sizes)-1 {
			total += gap
		}
	}
	return offsets, total
}
