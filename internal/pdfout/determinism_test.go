package pdfout

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"testing"
	"time"

	"github.com/jclement/treekillbot/internal/fonts"
	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/paint"
	"github.com/jclement/treekillbot/internal/render"
)

// The claim DESIGN.md section 4 makes, checked directly: the same document
// rendered twice is the same bytes. Everything else in that section — no map
// iteration, glyph IDs by first use, a hashed /ID, a pinned compression level
// — exists to make this true, so this is the test that notices when one of
// them stops holding.
func TestOutputIsByteIdentical(t *testing.T) {
	for _, compressed := range []bool{true, false} {
		name := "compressed"
		if !compressed {
			name = "no-compress"
		}
		t.Run(name, func(t *testing.T) {
			first := renderSample(t, !compressed)
			second := renderSample(t, !compressed)
			if !bytes.Equal(first, second) {
				t.Fatalf("two runs differ: %d vs %d bytes, first difference at %d",
					len(first), len(second), firstDifference(first, second))
			}
			t.Logf("%d bytes, sha256 %s", len(first), hex.EncodeToString(sha256Of(first)))
		})
	}
}

// A document rendered a year later, with the clock left alone, must still be
// the same file. The zero Options.Created is the epoch, not time.Now, and this
// is what would catch someone "fixing" that.
func TestDefaultCreatedDateIsNotTheClock(t *testing.T) {
	build := func() []byte {
		doc := New(Options{Title: "no clock"})
		canvas := doc.AddPage(mustPageSize(t, "a5"))
		canvas.AddRect(geom.Rect{X: geom.Pt(10), Y: geom.Pt(10), W: geom.Pt(50), H: geom.Pt(50)}, 0)
		canvas.SetFill(paint.GrayN(0.5))
		canvas.Fill()
		data, err := doc.Bytes()
		if err != nil {
			t.Fatalf("rendering: %v", err)
		}
		return data
	}
	if !bytes.Equal(build(), build()) {
		t.Fatal("a document with no explicit date is not reproducible")
	}
	if !bytes.Contains(build(), []byte("/CreationDate(D:19700101000000+00'00')")) {
		t.Error("the default creation date is not the epoch")
	}
}

// The /ID has to be a function of the content: same document, same identifier;
// different document, different identifier. A random one — which is what most
// writers emit — would fail the first half.
func TestFileIDIsDerivedFromContent(t *testing.T) {
	idPattern := regexp.MustCompile(`/ID \[<([0-9A-F]{32})> <([0-9A-F]{32})>\]`)

	extract := func(data []byte) string {
		t.Helper()
		match := idPattern.FindSubmatch(data)
		if match == nil {
			t.Fatalf("no /ID in the trailer")
		}
		if !bytes.Equal(match[1], match[2]) {
			t.Errorf("the two /ID strings differ: %s and %s", match[1], match[2])
		}
		return string(match[1])
	}

	same := extract(renderSample(t, true))
	againSame := extract(renderSample(t, true))
	if same != againSame {
		t.Errorf("the same document produced two identifiers: %s and %s", same, againSame)
	}

	doc := New(Options{Title: "different"})
	canvas := doc.AddPage(mustPageSize(t, "a5"))
	canvas.AddRect(geom.Rect{W: geom.Pt(10), H: geom.Pt(10)}, 0)
	canvas.SetFill(paint.Black)
	canvas.Fill()
	other, err := doc.Bytes()
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	if extract(other) == same {
		t.Error("two different documents share an identifier")
	}
}

// Bytes mutates gopdf's object graph as it compiles, so calling it twice must
// return the cached result rather than a second, differently-broken file.
func TestBytesIsIdempotent(t *testing.T) {
	doc := New(Options{})
	canvas := doc.AddPage(mustPageSize(t, "letter"))
	canvas.AddRect(geom.Rect{W: geom.Pt(10), H: geom.Pt(10)}, 0)
	canvas.SetFill(paint.Black)
	canvas.Fill()

	first, err := doc.Bytes()
	if err != nil {
		t.Fatalf("first render: %v", err)
	}
	second, err := doc.Bytes()
	if err != nil {
		t.Fatalf("second render: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("second call returned %d bytes, first returned %d", len(second), len(first))
	}
}

// finalizeTrailer splices bytes into a finished file. If gopdf ever changes
// what it writes, the splice must refuse rather than corrupt.
func TestFinalizeTrailerRejectsAnUnknownShape(t *testing.T) {
	tests := []struct {
		name string
		doc  string
	}{
		{"no trailer", "%PDF-1.7\nxref\n0 1\nstartxref\n9\n%%EOF\n"},
		{"no info dictionary", "%PDF-1.7\n\ntrailer\n<<\n/Size 1\n>>\nstartxref\n9\n%%EOF\n"},
		{"empty", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := finalizeTrailer([]byte(tc.doc), time.Unix(0, 0)); err == nil {
				t.Error("patched a document it should not recognise")
			}
		})
	}
}

// renderSample draws a page using every primitive whose emission order could
// plausibly depend on something other than the input: several fonts, several
// colour spaces, a batch of rules, a clip, and a curve.
func renderSample(t *testing.T, noCompress bool) []byte {
	t.Helper()
	registry := fonts.NewRegistry()
	doc := New(Options{
		Title:      "determinism",
		Author:     "treekillbot",
		Created:    time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		NoCompress: noCompress,
	})
	canvas := doc.AddPage(mustPageSize(t, "letter"))

	FillPanel(canvas, geom.Rect{X: geom.Pt(36), Y: geom.Pt(36), W: geom.Pt(300), H: geom.Pt(200)},
		geom.Pt(6), paint.GrayN(0.96), render.Stroke{Color: paint.GrayN(0.3), Width: geom.Pt(1)})

	pen := render.Stroke{Color: paint.GrayN(0.75), Width: geom.Pt(0.25)}
	for i := 0; i < 12; i++ {
		y := geom.Pt(60) + geom.Tick(i)*geom.Pt(14)
		StrokeRule(canvas, geom.Pt(48), y, geom.Pt(324), y, pen)
	}
	canvas.FlushLines()

	// Three colour spaces and an alpha, each of which reaches a different
	// corner of the emission code.
	canvas.SetFill(paint.CMYK(0.1, 0, 0.2, 0.05))
	canvas.AddRect(geom.Rect{X: geom.Pt(360), Y: geom.Pt(36), W: geom.Pt(60), H: geom.Pt(60)}, 0)
	canvas.Fill()
	canvas.SetFill(paint.RGB8(0x33, 0x66, 0x99).WithAlpha(0.5))
	canvas.AddRect(geom.Rect{X: geom.Pt(430), Y: geom.Pt(36), W: geom.Pt(60), H: geom.Pt(60)}, geom.Pt(4))
	canvas.Fill()

	canvas.Save()
	canvas.AddRect(geom.Rect{X: geom.Pt(36), Y: geom.Pt(260), W: geom.Pt(300), H: geom.Pt(80)}, 0)
	canvas.Clip()
	for i, family := range []string{"sans", "mono", "serif"} {
		face := mustFace(t, registry, family, fonts.Style(i%3))
		canvas.DrawText(geom.Pt(48), geom.Pt(280)+geom.Tick(i)*geom.Pt(20), render.TextRun{
			Text: "Sphinx of black quartz, judge my vow",
			Face: face, SizeQpt: 40, Color: paint.GrayN(0.15),
		})
	}
	canvas.Restore()

	data, err := doc.Bytes()
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	return data
}

func mustPageSize(t *testing.T, name string) PageSize {
	t.Helper()
	size, ok := NamedPageSize(name)
	if !ok {
		t.Fatalf("unknown page size %q", name)
	}
	return size
}

func sha256Of(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}

func firstDifference(a, b []byte) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}
