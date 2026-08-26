// `time-grid` — the day planner: an hour slot per row, half-hour marks, and a
// label gutter down the left.
//
// This is the one decoration that does not floor its count. Time is continuous
// and bounded: a grid that says 7am–9pm must END at 9pm, on the bottom edge of
// the panel. Leaving a 7pt stub of blank below 9pm because fourteen slots did
// not divide evenly into the height is simply wrong, so the rows are an exact
// partition of the height rather than a multiple of a pitch.
package decor

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/render"
)

// Label metrics the spec fixes and the property table does not expose.
const (
	timeLabelGap             = geom.TicksPerPt * 6 // between the label column and the writing area
	timeLabelPad             = geom.TicksPerPt * 2 // between a slot's top rule and its label
	labelSizeNum, labelSizeD = 7, 10               // hour labels at 0.7 * font-size
	minutesPerDay            = 24 * 60
)

// timeGrid draws a labelled hour grid.
type timeGrid struct {
	params
	startMinutes int
	slots        int // labelled slots between start and end
}

// newTimeGrid validates the clock properties. A span that does not divide into
// whole slots is an authoring error we can name precisely, which is worth more
// than silently rounding to something the author did not ask for.
func newTimeGrid(p params) (Decoration, error) {
	start, err := parseClock(p.timeStart)
	if err != nil {
		return nil, fmt.Errorf("time-start: %w", err)
	}
	end, err := parseClock(p.timeEnd)
	if err != nil {
		return nil, fmt.Errorf("time-end: %w", err)
	}
	if end <= start {
		return nil, fmt.Errorf("time-end %q is not after time-start %q", p.timeEnd, p.timeStart)
	}
	if p.timeStep <= 0 {
		return nil, fmt.Errorf("time-step must be a positive number of minutes, got %d", p.timeStep)
	}
	span := end - start
	if span%p.timeStep != 0 {
		return nil, fmt.Errorf("time-start %s to time-end %s is %d minutes, which is not a whole number of %d-minute slots",
			p.timeStart, p.timeEnd, span, p.timeStep)
	}
	return &timeGrid{params: p, startMinutes: start, slots: span / p.timeStep}, nil
}

// rowOffsets returns the cumulative y of every slot boundary, from the top of
// the band to its bottom edge — slots+1 values, the last exactly band.Bottom().
//
// DistributeTicks is what makes that last claim true rather than approximately
// true: it is the only divider in the engine and it guarantees the parts sum to
// the whole (DESIGN.md D2).
func (t *timeGrid) rowOffsets(band geom.Rect) (offsets []geom.Tick, rows []geom.Tick) {
	if t.slots <= 0 || band.H <= 0 {
		return nil, nil
	}
	rows = geom.DistributeEqual(band.H, t.slots)
	offsets = make([]geom.Tick, 0, t.slots+1)
	y := band.Y
	offsets = append(offsets, y)
	for _, h := range rows {
		y += h
		offsets = append(offsets, y)
	}
	return offsets, rows
}

// writingLeft is where the writing area starts: past the label gutter and its
// gap. Rules span only the writing area, not the labels.
func (t *timeGrid) writingLeft(band geom.Rect) geom.Tick {
	return band.X + t.timeGutter + timeLabelGap
}

// Draw paints the sub-slot marks, the slot rules, the gutter rule and the hour
// labels, in that order so the heavier lines land on top of the lighter ones.
//
// Rule B for every line here. Slot rules include both the top and the bottom
// edge — unlike a ruled panel, a time grid's first and last lines are real: they
// are the start of the first hour and the end of the last.
func (t *timeGrid) Draw(content geom.Rect, _ Grid, dst render.Canvas) {
	band := content.Inset(t.inset)
	offsets, rows := t.rowOffsets(band)
	if len(offsets) == 0 {
		return
	}
	left, right := t.writingLeft(band), band.Right()
	if right <= left {
		return
	}

	t.drawSubMarks(dst, offsets, rows, left, right)

	slotPen := t.rulePen()
	if slotPen.IsVisible() {
		dst.SetStroke(slotPen)
		for _, y := range offsets {
			dst.DrawLine(left, y, right, y)
		}
		// The gutter rule separates the labels from the writing area. It sits
		// halfway across the gap so it is optically between the two rather than
		// crowding either.
		gutterX := band.X + t.timeGutter + timeLabelGap/2
		dst.DrawLine(gutterX, offsets[0], gutterX, offsets[len(offsets)-1])
		dst.FlushLines()
	}

	t.drawLabels(dst, band, offsets)
}

// drawSubMarks strokes the unlabelled marks inside each slot.
//
// Each slot's marks are partitioned from THAT slot's exact tick count, not from
// a global half-pitch, so a half-hour mark can never drift away from the middle
// of its hour even when the slot heights differ by a tick.
func (t *timeGrid) drawSubMarks(dst render.Canvas, offsets, rows []geom.Tick, left, right geom.Tick) {
	if t.timeSubdivide <= 1 {
		return
	}
	subPen := pen(t.color, t.width.Scale(subRuleWeightNum, subRuleWeightDen))
	if !subPen.IsVisible() {
		return
	}
	dst.SetStroke(subPen)
	for i, height := range rows {
		y := offsets[i]
		parts := geom.DistributeEqual(height, t.timeSubdivide)
		// The last part ends on the next slot boundary, which the slot rule
		// already draws, so it is not a mark of its own.
		for _, part := range parts[:len(parts)-1] {
			y += part
			dst.DrawLine(left, y, right, y)
		}
	}
	dst.FlushLines()
}

// drawLabels places one label per slot, hugging the TOP of its slot and
// right-aligned in the gutter.
//
// Top, because the label names the hour that begins there — that is what every
// calendar app does and what the eye expects. Right-aligned, because it gives
// the labels a clean edge against the gutter rule.
func (t *timeGrid) drawLabels(dst render.Canvas, band geom.Rect, offsets []geom.Tick) {
	if t.face == nil || t.timeGutter <= 0 {
		return
	}
	size := labelSizeQpt(t.sizeQpt)
	ascent := t.face.Ascent(size)
	labelRight := band.X + t.timeGutter
	for i := 0; i < t.slots; i++ {
		run := render.TextRun{
			Text:    clockLabel(t.startMinutes + i*t.timeStep),
			Face:    t.face,
			SizeQpt: size,
			Color:   t.textColor,
		}
		dst.DrawText(labelRight-run.Width(), offsets[i]+ascent+timeLabelPad, run)
	}
}

// labelSizeQpt scales the body size down for the hour labels, never below the
// smallest size worth setting.
func labelSizeQpt(bodyQpt int32) int32 {
	size := (bodyQpt*labelSizeNum + labelSizeD/2) / labelSizeD
	if size < 4 {
		return 4
	}
	return size
}

// Baselines returns the top rule of each writable slot.
//
// A time grid inverts a ruled panel's relationship between rule and text: you
// write BELOW an hour line, not on it, so these are the slot boundaries text
// hangs from. The closing rule at the bottom edge is not one of them — nothing
// is written below the end of the day.
func (t *timeGrid) Baselines(content geom.Rect) []geom.Tick {
	offsets, _ := t.rowOffsets(content.Inset(t.inset))
	if len(offsets) == 0 {
		return nil
	}
	return offsets[:len(offsets)-1]
}

// NaturalHeight is one line-pitch per slot.
//
// A time grid fills the height it is given, but unlike the other filling
// decorations it knows how many rows it has, so `height: auto` can mean
// something better than nothing: the whole day at the author's writing pitch.
func (t *timeGrid) NaturalHeight() geom.Tick {
	if t.slots <= 0 || t.pitch <= 0 {
		return 0
	}
	return t.pitch * geom.Tick(t.slots)
}

// ---- Clock parsing and formatting ----

// parseClock reads "7", "7:30", "7am", "7:30 pm" or "19:00" into minutes since
// midnight. 24:00 is accepted as an end time meaning the end of the day.
func parseClock(s string) (int, error) {
	text := strings.ToLower(strings.TrimSpace(s))
	if text == "" {
		return 0, fmt.Errorf("no time given")
	}

	pm := false
	meridiem := false
	for _, suffix := range []string{"am", "pm", "a", "p"} {
		if !strings.HasSuffix(text, suffix) {
			continue
		}
		pm = strings.HasPrefix(suffix, "p")
		meridiem = true
		text = strings.TrimSpace(strings.TrimSuffix(text, suffix))
		break
	}

	hourText, minuteText, hasMinutes := strings.Cut(text, ":")
	hour, err := strconv.Atoi(strings.TrimSpace(hourText))
	if err != nil {
		return 0, fmt.Errorf("%q is not a time like 7:00, 7am or 19:00", s)
	}
	minute := 0
	if hasMinutes {
		if minute, err = strconv.Atoi(strings.TrimSpace(minuteText)); err != nil {
			return 0, fmt.Errorf("%q is not a time like 7:00, 7am or 19:00", s)
		}
	}

	if meridiem {
		if hour < 1 || hour > 12 {
			return 0, fmt.Errorf("%q: a 12-hour time needs an hour from 1 to 12", s)
		}
		hour %= 12
		if pm {
			hour += 12
		}
	}
	if hour < 0 || hour > 24 || minute < 0 || minute > 59 {
		return 0, fmt.Errorf("%q is not a valid time of day", s)
	}
	total := hour*60 + minute
	if total > minutesPerDay {
		return 0, fmt.Errorf("%q is past the end of the day", s)
	}
	return total, nil
}

// clockLabel renders an hour label in the compact 12-hour form a planner wants:
// 7, 8, …, 11, 12p, 1, 2. The meridiem appears only at noon and midnight, which
// is the one place a bare number would be ambiguous, and leaving it off
// everywhere else keeps the gutter quiet.
func clockLabel(minutes int) string {
	minutes %= minutesPerDay
	hour, minute := minutes/60, minutes%60

	label := strconv.Itoa(hourIn12(hour))
	if minute != 0 {
		label += fmt.Sprintf(":%02d", minute)
	}
	switch hour {
	case 0:
		return label + "a"
	case 12:
		return label + "p"
	}
	return label
}

// hourIn12 maps a 24-hour hour onto a clock face, where midnight and noon are
// both 12 rather than 0.
func hourIn12(hour int) int {
	hour %= 12
	if hour == 0 {
		return 12
	}
	return hour
}
