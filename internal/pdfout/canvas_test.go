package pdfout

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/jclement/treekillbot/internal/fonts"
	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/paint"
	"github.com/jclement/treekillbot/internal/render"
)

// DESIGN.md D10's whole claim: the tint the author writes is the tint the
// printer receives. gray(0.85) must emit "0.85 g", not an RGB triple a RIP has
// to convert back, and cmyk() must pass through as "k".
func TestColorSpacesReachTheContentStream(t *testing.T) {
	tests := []struct {
		name    string
		color   paint.Color
		wantOps []string
		absent  []string
	}{
		{
			name:    "grey fill is DeviceGray",
			color:   paint.GrayN(0.85),
			wantOps: []string{"0.85 g"},
			absent:  []string{"0.850 0.850 0.850 rg"},
		},
		{
			name:    "cmyk fill is DeviceCMYK",
			color:   paint.CMYK(0.1, 0.2, 0.3, 0.4),
			wantOps: []string{"0.10 0.20 0.30 0.40 k"},
		},
		{
			name:    "rgb fill is DeviceRGB",
			color:   paint.RGB8(0x33, 0x66, 0x99),
			wantOps: []string{"0.200 0.400 0.600 rg"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stream := contentStream(t, func(canvas *Canvas) {
				canvas.SetFill(tc.color)
				canvas.AddRect(geom.Rect{X: geom.Pt(10), Y: geom.Pt(10), W: geom.Pt(50), H: geom.Pt(50)}, 0)
				canvas.Fill()
			})
			for _, want := range tc.wantOps {
				if !strings.Contains(stream, want) {
					t.Errorf("content stream lacks %q:\n%s", want, stream)
				}
			}
			for _, unwanted := range tc.absent {
				if strings.Contains(stream, unwanted) {
					t.Errorf("content stream contains %q, which means the space was converted", unwanted)
				}
			}
		})
	}
}

// Grey text is the case that nearly does not work: gopdf writes a colour
// operator inside every text object unless it is in grey mode, so a grey tint
// would otherwise be silently promoted to RGB on its way onto the page.
func TestGrayTextStaysDeviceGray(t *testing.T) {
	registry := fonts.NewRegistry()
	face := mustFace(t, registry, "sans", fonts.Regular)

	stream := contentStream(t, func(canvas *Canvas) {
		canvas.DrawText(geom.Pt(72), geom.Pt(72), render.TextRun{
			Text: "grey", Face: face, SizeQpt: 40, Color: paint.GrayN(0.2),
		})
	})
	if !strings.Contains(stream, "0.20 g") {
		t.Errorf("grey text did not emit a DeviceGray operator:\n%s", stream)
	}
	if regexp.MustCompile(`0\.200 0\.200 0\.200 rg`).MatchString(stream) {
		t.Errorf("grey text was converted to RGB:\n%s", stream)
	}
}

// PDF's "0 w" means "whatever one device pixel is", which makes the line's
// weight a property of the printer. DESIGN.md D10 refuses it.
func TestStrokeWidthIsClampedToAQuarterPoint(t *testing.T) {
	tests := []struct {
		name  string
		width geom.Tick
		want  string
	}{
		{"below the floor", geom.Pt(0.1), "0.25 w"},
		{"one tick", 1, "0.25 w"},
		{"at the floor", geom.Pt(0.25), "0.25 w"},
		{"above the floor", geom.Pt(0.5), "0.50 w"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stream := contentStream(t, func(canvas *Canvas) {
				canvas.SetStroke(render.Stroke{Color: paint.Black, Width: tc.width})
				canvas.DrawLine(geom.Pt(10), geom.Pt(10), geom.Pt(100), geom.Pt(10))
				canvas.FlushLines()
			})
			if !strings.Contains(stream, tc.want) {
				t.Errorf("want %q in:\n%s", tc.want, stream)
			}
			if strings.Contains(stream, "0.00 w") {
				t.Errorf("emitted a zero-width stroke:\n%s", stream)
			}
		})
	}
}

// gopdf's dash state is sticky (DESIGN.md D7): a solid rule drawn after a
// dashed one inherits the dash unless the pattern is reset explicitly.
func TestDashIsResetBetweenPens(t *testing.T) {
	stream := contentStream(t, func(canvas *Canvas) {
		canvas.SetStroke(render.Stroke{
			Color: paint.Black, Width: geom.Pt(0.5),
			Dash: []geom.Tick{geom.Pt(2), geom.Pt(2)}, Phase: geom.Pt(1),
		})
		canvas.DrawLine(geom.Pt(10), geom.Pt(10), geom.Pt(100), geom.Pt(10))
		canvas.SetStroke(render.Stroke{Color: paint.Black, Width: geom.Pt(0.5)})
		canvas.DrawLine(geom.Pt(10), geom.Pt(20), geom.Pt(100), geom.Pt(20))
		canvas.FlushLines()
	})
	dashAt := strings.Index(stream, "[2.00 2.00] 1.00 d")
	resetAt := strings.Index(stream, "[] 0 d")
	if dashAt < 0 {
		t.Fatalf("dash pattern was never set:\n%s", stream)
	}
	if resetAt < dashAt {
		t.Fatalf("dash was never reset after being set:\n%s", stream)
	}
}

// gopdf has no SetLineCap, so a zero-length line has butt caps at both ends
// and paints nothing. Emitting one would put an operator in the file that
// provably deposits no ink; dots are filled shapes instead.
func TestZeroLengthLineIsDropped(t *testing.T) {
	stream := contentStream(t, func(canvas *Canvas) {
		canvas.SetStroke(render.Stroke{Color: paint.Black, Width: geom.Pt(1)})
		canvas.DrawLine(geom.Pt(50), geom.Pt(50), geom.Pt(50), geom.Pt(50))
		canvas.FlushLines()
	})
	if strings.Contains(stream, " l S") {
		t.Errorf("a degenerate line reached the content stream:\n%s", stream)
	}
	// A page whose only instruction was a degenerate line has no content at
	// all, which is the strongest form of "drew nothing".
	if stream != "" {
		t.Errorf("want an empty content stream, got:\n%s", stream)
	}
}

// The point of overriding OnGlyphNotFound: gopdf substitutes a space and says
// nothing, so a checkbox character the face lacks prints as a blank.
func TestMissingGlyphsAreCollected(t *testing.T) {
	registry := fonts.NewRegistry()
	face := mustFace(t, registry, "mono", fonts.Regular)

	doc := New(Options{})
	canvas := doc.AddPage(mustPageSize(t, "a5"))
	// Linear B syllables and an Egyptian hieroglyph: nothing IBM Plex ships.
	canvas.DrawText(geom.Pt(20), geom.Pt(40), render.TextRun{
		Text: "ok \U00010000\U00010001\U00013000", Face: face, SizeQpt: 40, Color: paint.Black,
	})
	if _, err := doc.Bytes(); err != nil {
		t.Fatalf("rendering: %v", err)
	}

	missing := doc.MissingGlyphs()
	want := []rune{0x10000, 0x10001, 0x13000}
	if len(missing) != len(want) {
		t.Fatalf("collected %v, want %v", missing, want)
	}
	for i := range want {
		if missing[i] != want[i] {
			t.Fatalf("collected %v, want %v (sorted by code point)", missing, want)
		}
	}
}

// A Restore with no Save unbalances the graphics state stack, which corrupts
// every clip after it. Silently ignoring it would produce a page that is wrong
// in a way nobody can trace.
func TestUnbalancedRestoreIsAnError(t *testing.T) {
	doc := New(Options{})
	canvas := doc.AddPage(mustPageSize(t, "a5"))
	canvas.Restore()
	if _, err := doc.Bytes(); err == nil {
		t.Fatal("rendering succeeded after an unbalanced Restore")
	}
}

func TestDocumentWithNoPagesIsAnError(t *testing.T) {
	if _, err := New(Options{}).Bytes(); err == nil {
		t.Fatal("rendered a document with no pages")
	}
}

// --grayscale is a property of a print run, so it converts on the way out
// rather than by swapping the theme.
func TestGrayscaleOptionDesaturates(t *testing.T) {
	stream := contentStreamWith(t, Options{NoCompress: true, Grayscale: true}, func(canvas *Canvas) {
		canvas.SetFill(paint.RGB8(0xFF, 0x00, 0x00))
		canvas.AddRect(geom.Rect{X: geom.Pt(10), Y: geom.Pt(10), W: geom.Pt(20), H: geom.Pt(20)}, 0)
		canvas.Fill()
	})
	if strings.Contains(stream, " rg") {
		t.Errorf("--grayscale left an RGB operator in the stream:\n%s", stream)
	}
	if !strings.Contains(stream, "0.21 g") {
		t.Errorf("want the Rec. 709 luminance of pure red as DeviceGray:\n%s", stream)
	}
}

// A font first used on the second page must still embed correctly: gopdf
// appends font objects to the same list as pages and content streams, and
// getting that order wrong produces a page with no glyphs.
func TestFontFirstUsedOnALaterPage(t *testing.T) {
	registry := fonts.NewRegistry()
	doc := New(Options{})
	first := doc.AddPage(mustPageSize(t, "a6"))
	first.DrawText(geom.Pt(20), geom.Pt(40), render.TextRun{
		Text: "page one", Face: mustFace(t, registry, "sans", fonts.Regular), SizeQpt: 40, Color: paint.Black,
	})
	second := doc.AddPage(mustPageSize(t, "a6"))
	second.DrawText(geom.Pt(20), geom.Pt(40), render.TextRun{
		Text: "page two", Face: mustFace(t, registry, "serif", fonts.BoldItalic), SizeQpt: 40, Color: paint.Black,
	})

	data, err := doc.Bytes()
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	assertValidPDF(t, data, 2)
	if got := bytes.Count(data, []byte("/FontFile2")); got != 2 {
		t.Errorf("embedded %d font files, want 2", got)
	}
}

// ---- helpers ----

// contentStream renders a one-page uncompressed document and returns that
// page's content stream, which is the only way to assert on the operators
// actually emitted. An empty string means the page drew nothing at all.
func contentStream(t *testing.T, draw func(*Canvas)) string {
	t.Helper()
	return contentStreamWith(t, Options{}, draw)
}

func contentStreamWith(t *testing.T, opts Options, draw func(*Canvas)) string {
	t.Helper()
	opts.NoCompress = true
	doc := New(opts)
	canvas := doc.AddPage(mustPageSize(t, "letter"))
	draw(canvas)
	data, err := doc.Bytes()
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	return pageContentStream(t, data)
}

// pageContentStream follows the first page's /Contents reference to its object
// and returns the stream body. Following the reference rather than grabbing the
// first "stream" in the file matters: the ToUnicode CMap and the embedded font
// are streams too, and they come first.
func pageContentStream(t *testing.T, data []byte) string {
	t.Helper()
	reference := regexp.MustCompile(`/Contents\s+(\d+) 0 R`).FindSubmatch(data)
	if reference == nil {
		return "" // the page has no content object, so nothing was drawn
	}
	objectAt := bytes.Index(data, append([]byte("\n"), append(reference[1], []byte(" 0 obj\n")...)...))
	if objectAt < 0 {
		t.Fatalf("page references object %s, which is not in the file", reference[1])
	}
	start := bytes.Index(data[objectAt:], []byte("stream\n"))
	end := bytes.Index(data[objectAt:], []byte("endstream"))
	if start < 0 || end < start {
		t.Fatalf("content object %s has no stream", reference[1])
	}
	return string(data[objectAt+start+len("stream\n") : objectAt+end])
}
