package svgout

import (
	"strings"
	"testing"

	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/paint"
	"github.com/jclement/treekillbot/internal/render"
)

func newTestCanvas() *Canvas {
	return New(geom.In(8.5), geom.In(11), Options{Background: paint.White})
}

func TestViewBoxCarriesThePageSize(t *testing.T) {
	svg := newTestCanvas().String()
	// A viewBox in points plus width/height of 100% is what makes the preview
	// scale to any pane without distorting: the aspect ratio is carried by the
	// viewBox, not by whatever the container happens to be.
	if !strings.Contains(svg, `viewBox="0 0 612 792"`) {
		t.Fatalf("viewBox missing or wrong:\n%s", firstLine(svg))
	}
	if !strings.Contains(svg, `preserveAspectRatio="xMidYMid meet"`) {
		t.Fatal("preserveAspectRatio must keep the page's proportions")
	}
}

// Coordinates must quantise the same way the PDF backend's do, or the two
// outputs would disagree in the last decimal and the preview would stop being
// a faithful recreation.
func TestCoordinatesMatchThePDFQuantisation(t *testing.T) {
	tests := []struct {
		ticks geom.Tick
		want  string
	}{
		{geom.Pt(72), "72"},
		{geom.Pt(0.5), "0.5"},
		{geom.Pt(10.25), "10.25"},
		{1, "0.06"}, // one tick is 0.0625pt, and both backends emit 2 decimals
		{0, "0"},
		{-1, "-0.06"},
	}
	for _, tt := range tests {
		if got := num(tt.ticks); got != tt.want {
			t.Errorf("num(%d) = %q, want %q", tt.ticks, got, tt.want)
		}
	}
}

func TestRectPathIsClosedAndExact(t *testing.T) {
	c := newTestCanvas()
	c.SetFill(paint.Black)
	c.AddRect(geom.Rect{X: geom.Pt(10), Y: geom.Pt(20), W: geom.Pt(100), H: geom.Pt(50)}, 0)
	c.Fill()
	svg := c.String()
	if !strings.Contains(svg, `d="M10 20H110V70H10Z"`) {
		t.Fatalf("rect path is wrong:\n%s", svg)
	}
}

// A rounded rect's outer silhouette must match the given rect exactly, the same
// guarantee the PDF backend makes, so fills and borders agree at the corners.
func TestRoundedRectStaysInsideItsRect(t *testing.T) {
	c := newTestCanvas()
	c.SetFill(paint.Black)
	c.AddRect(geom.Rect{X: 0, Y: 0, W: geom.Pt(100), H: geom.Pt(60)}, geom.Pt(6))
	c.Fill()
	svg := c.String()
	// The path starts at x = radius on the top edge and uses arcs, never
	// straying outside 0..100 or 0..60.
	if !strings.Contains(svg, `M6 0H94A6 6 0 0 1 100 6`) {
		t.Fatalf("rounded rect does not begin on its own edge:\n%s", svg)
	}
}

func TestStrokeAttributes(t *testing.T) {
	c := newTestCanvas()
	c.SetStroke(render.Stroke{
		Color: paint.GrayN(0.5),
		Width: geom.Pt(0.5),
		Dash:  []geom.Tick{geom.Pt(2), geom.Pt(2)},
	})
	c.MoveTo(0, 0)
	c.LineTo(geom.Pt(10), 0)
	c.Stroke()
	svg := c.String()
	for _, want := range []string{
		`stroke="#808080"`,
		`stroke-width="0.5"`,
		`stroke-dasharray="2 2"`,
		// PDF's default cap is butt and the backend cannot change it, so the
		// preview must say so explicitly rather than take SVG's own default.
		`stroke-linecap="butt"`,
		`fill="none"`,
	} {
		if !strings.Contains(svg, want) {
			t.Errorf("missing %s in:\n%s", want, svg)
		}
	}
}

// A ruled panel emits hundreds of lines; they must become one path element.
func TestDrawLineBatchesIntoOnePath(t *testing.T) {
	c := newTestCanvas()
	c.SetStroke(render.Stroke{Color: paint.Black, Width: geom.Pt(0.5)})
	for i := 0; i < 40; i++ {
		y := geom.Pt(float64(i) * 15)
		c.DrawLine(0, y, geom.Pt(400), y)
	}
	c.FlushLines()
	svg := c.String()
	if n := strings.Count(svg, "<path"); n != 1 {
		t.Fatalf("40 rules produced %d path elements, want 1", n)
	}
	if n := strings.Count(svg, "M0 "); n != 40 {
		t.Fatalf("the batched path has %d moves, want 40", n)
	}
}

// A pen change mid-batch must flush first, or the earlier lines would be drawn
// with the later pen.
func TestPenChangeFlushesTheBatch(t *testing.T) {
	c := newTestCanvas()
	c.SetStroke(render.Stroke{Color: paint.Black, Width: geom.Pt(0.5)})
	c.DrawLine(0, 0, geom.Pt(10), 0)
	c.SetStroke(render.Stroke{Color: paint.GrayN(0.5), Width: geom.Pt(1)})
	c.DrawLine(0, geom.Pt(10), geom.Pt(10), geom.Pt(10))
	c.FlushLines()
	if n := strings.Count(c.String(), "<path"); n != 2 {
		t.Fatalf("got %d paths, want 2 — the pen change must have flushed the first batch", n)
	}
}

// A zero-length segment draws nothing in PDF because the backend has butt caps
// and no way to change them. The preview must match, or it would show dots the
// print does not have.
func TestZeroLengthLineDrawsNothing(t *testing.T) {
	c := newTestCanvas()
	c.SetStroke(render.Stroke{Color: paint.Black, Width: geom.Pt(1)})
	c.DrawLine(geom.Pt(5), geom.Pt(5), geom.Pt(5), geom.Pt(5))
	c.FlushLines()
	if strings.Contains(c.String(), "<path") {
		t.Fatal("a zero-length segment must draw nothing, matching the PDF backend")
	}
}

func TestGrayIsEmittedAsAnExplicitTriple(t *testing.T) {
	c := newTestCanvas()
	c.SetFill(paint.GrayN(0.85))
	c.AddRect(geom.Rect{W: geom.Pt(10), H: geom.Pt(10)}, 0)
	c.Fill()
	// gray(0.85) is 217 in 8-bit, so #d9d9d9. Emitting a triple rather than a
	// keyword keeps the browser showing the same tint the PDF carries.
	if !strings.Contains(c.String(), `fill="#d9d9d9"`) {
		t.Fatalf("grey should render as an explicit triple:\n%s", c.String())
	}
}

func TestClipNestsAndCloses(t *testing.T) {
	c := newTestCanvas()
	c.Save()
	c.AddRect(geom.Rect{W: geom.Pt(10), H: geom.Pt(10)}, 0)
	c.Clip()
	c.SetFill(paint.Black)
	c.AddRect(geom.Rect{W: geom.Pt(100), H: geom.Pt(100)}, 0)
	c.Fill()
	c.Restore()

	svg := c.String()
	if strings.Count(svg, "<clipPath") != 1 {
		t.Fatal("expected exactly one clipPath")
	}
	if strings.Count(svg, "<g clip-path") != strings.Count(svg, "</g>") {
		t.Fatalf("unbalanced clip groups:\n%s", svg)
	}
}

// String must be safe to call without a matching Restore, because a paint pass
// that errors part-way still has to produce a well-formed document.
func TestStringClosesDanglingGroups(t *testing.T) {
	c := newTestCanvas()
	c.Save()
	c.AddRect(geom.Rect{W: geom.Pt(10), H: geom.Pt(10)}, 0)
	c.Clip()
	svg := c.String()
	if strings.Count(svg, "<g clip-path") != strings.Count(svg, "</g>") {
		t.Fatalf("unbalanced groups when Restore was never called:\n%s", svg)
	}
	if !strings.HasSuffix(svg, "</svg>") {
		t.Fatal("document must still close")
	}
}

func TestTextEscapesMarkup(t *testing.T) {
	// A title containing markup characters must not be able to break the
	// document, which is the whole risk of generating XML by hand.
	if got := escapeText(`a < b & "c"`); got != `a &lt; b &amp; "c"` {
		t.Fatalf("escapeText = %q", got)
	}
	if got := escapeAttr(`say "hi" & <go>`); got != `say &quot;hi&quot; &amp; &lt;go&gt;` {
		t.Fatalf("escapeAttr = %q", got)
	}
}

func TestOutputIsDeterministic(t *testing.T) {
	build := func() string {
		c := newTestCanvas()
		c.SetFill(paint.GrayN(0.9))
		c.AddRect(geom.Rect{W: geom.Pt(100), H: geom.Pt(50)}, geom.Pt(3))
		c.Fill()
		c.SetStroke(render.Stroke{Color: paint.Black, Width: geom.Pt(0.5)})
		for i := 0; i < 20; i++ {
			c.DrawLine(0, geom.Pt(float64(i)*10), geom.Pt(100), geom.Pt(float64(i)*10))
		}
		c.FlushLines()
		return c.String()
	}
	first := build()
	for i := 0; i < 50; i++ {
		if again := build(); again != first {
			t.Fatalf("run %d differs", i)
		}
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '>'); i >= 0 {
		return s[:i+1]
	}
	return s
}
