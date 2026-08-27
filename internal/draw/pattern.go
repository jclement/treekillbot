// Patterned fills.
//
// A dither is drawn as one dashed line per row, never as a field of little
// squares. A 12pt heading band at a 1pt cell is twelve stroke operations; the
// same band as individual cells would be six thousand rectangles, and a page of
// headings would be a PDF nobody wants to open.
//
// The trick is that a dash array can encode any repeating on/off sequence. A
// row of an ordered-dither matrix is exactly that: a short pattern of inked and
// blank cells that repeats. Stroke a line whose width equals the cell size, with
// a dash built from the row's pattern, and the result is a row of perfect
// squares.
//
// Why a dither at all, rather than a light grey: a pattern is made of one solid
// ink, so a printer reproduces it exactly. A flat 15% grey goes through a
// halftone screen and dot-gains two or three steps darker than it looked on
// screen — the concern DESIGN.md D10 is built around. A dither is the tint you
// can predict.
package draw

import (
	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/paint"
	"github.com/jclement/treekillbot/internal/render"
)

// The dither masks, four cells square, one per shipped density.
//
// These are hand-authored rather than thresholded out of a Bayer matrix. A
// general matrix is the right tool when you need every level; at these four
// specific densities it degenerates. At 25% the standard 4×4 matrix inks the
// same two columns on rows 0 and 2, so the dots line up vertically and the fill
// reads as stripes rather than as texture. Choosing the four patterns by eye is
// both simpler and better, which is why the classic system patterns were drawn
// this way too.
//
// Each row is offset from the one above so the ink disperses.
var ditherMasks = map[string][4][4]bool{
	// 2 of 16.
	"dither-12": {
		{true, false, false, false},
		{false, false, false, false},
		{false, false, true, false},
		{false, false, false, false},
	},
	// 4 of 16.
	"dither-25": {
		{true, false, true, false},
		{false, false, false, false},
		{false, true, false, true},
		{false, false, false, false},
	},
	// 8 of 16: the checkerboard.
	"dither-50": {
		{true, false, true, false},
		{false, true, false, true},
		{true, false, true, false},
		{false, true, false, true},
	},
	// 12 of 16: the inverse of 25%.
	"dither-75": {
		{false, true, false, true},
		{true, true, true, true},
		{true, false, true, false},
		{true, true, true, true},
	},
}

// patternFill paints a pattern into a region, clipped to it.
//
// The lattice is anchored to the page rather than to the region, so dithers on
// adjacent headings line up instead of each starting its own grid — the same
// reasoning as the page-global dot lattice in the line decorations.
func patternFill(name string, region geom.Rect, radius geom.Tick, color paint.Color, pitch geom.Tick, origin geom.Rect, canvas render.Canvas) {
	if name == "" || name == "none" || region.IsEmpty() || color.IsInvisible() {
		return
	}
	if pitch < minStroke {
		pitch = minStroke
	}

	canvas.Save()
	defer canvas.Restore()
	canvas.AddRect(region, radius)
	canvas.Clip()

	switch {
	case name == "scanline":
		paintScanlines(region, color, pitch, origin, canvas)
	case name == "hatch":
		paintHatch(region, color, pitch, origin, canvas)
	default:
		if mask, ok := ditherMasks[name]; ok {
			paintDither(region, color, pitch, origin, mask, canvas)
		}
	}
	canvas.FlushLines()
}

// paintDither strokes one dashed line per row of the lattice.
func paintDither(region geom.Rect, color paint.Color, pitch geom.Tick, origin geom.Rect, mask [4][4]bool, canvas render.Canvas) {
	firstRow := latticeIndex(region.Y, origin.Y, pitch)
	lastRow := latticeIndex(region.Bottom(), origin.Y, pitch)

	for row := firstRow; row <= lastRow; row++ {
		start, dash, ok := rowDash(mask[((row%4)+4)%4], pitch)
		if !ok {
			continue // no ink in this row
		}
		// The line begins at the lattice, left of the region, and the clip
		// trims it. That keeps the dash phase at zero and the pattern aligned
		// across every region on the page.
		y := origin.Y + geom.Tick(row)*pitch + pitch/2
		x := origin.X + geom.Tick(start)*pitch
		canvas.SetStroke(render.Stroke{Color: color, Width: pitch, Dash: dash})
		canvas.MoveTo(x, y)
		canvas.LineTo(region.Right(), y)
		canvas.Stroke()
	}
}

// rowDash converts a row's cell mask into a starting cell and a dash array.
//
// The scan starts at an inked cell whose predecessor is blank — a run boundary —
// so the runs alternate cleanly and the array comes out with an even number of
// entries. An odd-length dash array flips its meaning every period, which turns
// a checkerboard into a mess.
func rowDash(mask [4]bool, pitch geom.Tick) (start int, dash []geom.Tick, ok bool) {
	inked := 0
	for _, cell := range mask {
		if cell {
			inked++
		}
	}
	switch inked {
	case 0:
		return 0, nil, false
	case len(mask):
		return 0, nil, true // solid: no dash at all
	}

	start = -1
	for i := 0; i < 4; i++ {
		if mask[i] && !mask[(i+3)%4] {
			start = i
			break
		}
	}
	if start < 0 {
		return 0, nil, false
	}

	on := true
	run := 0
	for offset := 0; offset < 4; offset++ {
		cell := mask[(start+offset)%4]
		if cell == on {
			run++
			continue
		}
		dash = append(dash, geom.Tick(run)*pitch)
		on = cell
		run = 1
	}
	dash = append(dash, geom.Tick(run)*pitch)
	return start, dash, true
}

// paintScanlines draws solid rows on every second lattice row: the coarse,
// unmistakably mechanical texture of a CRT or a cheap fax.
func paintScanlines(region geom.Rect, color paint.Color, pitch geom.Tick, origin geom.Rect, canvas render.Canvas) {
	canvas.SetStroke(render.Stroke{Color: color, Width: pitch})
	firstRow := latticeIndex(region.Y, origin.Y, pitch)
	lastRow := latticeIndex(region.Bottom(), origin.Y, pitch)
	for row := firstRow; row <= lastRow; row++ {
		if ((row%2)+2)%2 != 0 {
			continue
		}
		y := origin.Y + geom.Tick(row)*pitch + pitch/2
		canvas.DrawLine(region.X, y, region.Right(), y)
	}
	canvas.FlushLines()
}

// paintHatch draws 45-degree lines across the region.
//
// The spacing is four cells rather than one: a hatch at the dither's own pitch
// would be a solid fill, and the point of a hatch is that you can see through it.
func paintHatch(region geom.Rect, color paint.Color, pitch geom.Tick, origin geom.Rect, canvas render.Canvas) {
	spacing := pitch * 4
	if spacing <= 0 {
		return
	}
	width := pitch / 2
	if width < minStroke {
		width = minStroke
	}
	canvas.SetStroke(render.Stroke{Color: color, Width: width})

	// A 45-degree line through the region is indexed by x - y, so sweeping that
	// difference across the region's extent covers it exactly once.
	from := region.X - region.Bottom()
	to := region.Right() - region.Y
	first := latticeIndex(from, origin.X-origin.Y, spacing)
	last := latticeIndex(to, origin.X-origin.Y, spacing)
	base := origin.X - origin.Y
	for index := first; index <= last; index++ {
		offset := base + geom.Tick(index)*spacing
		// Extend well past the region on both ends; the clip trims it.
		canvas.DrawLine(offset+region.Y, region.Y, offset+region.Bottom(), region.Bottom())
	}
	canvas.FlushLines()
}

// latticeIndex returns which lattice cell a coordinate falls in, flooring so
// that negative offsets behave.
func latticeIndex(value, origin, pitch geom.Tick) int {
	if pitch <= 0 {
		return 0
	}
	delta := value - origin
	if delta < 0 {
		return int((delta - pitch + 1) / pitch)
	}
	return int(delta / pitch)
}
