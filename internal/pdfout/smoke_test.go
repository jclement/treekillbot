package pdfout

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jclement/treekillbot/internal/fonts"
	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/paint"
	"github.com/jclement/treekillbot/internal/render"
)

// A day page, drawn the way the render layer will draw one: a titled rounded
// panel, ruled writing lines at an exact pitch, a dashed separator, a
// right-aligned date measured through the font, and vector checkboxes.
//
// This is the test that catches the failures no unit test sees — a font that
// will not embed, an ordering bug between text and paths, a trailer patch that
// breaks the cross-reference table. It leaves its output in testdata so a
// human can open it when something looks wrong.
func TestSmokeDayPage(t *testing.T) {
	doc := New(Options{
		Title:   "treekillbot smoke test",
		Author:  "treekillbot",
		Subject: "A day page exercising every drawing primitive",
		Creator: "pdfout smoke test",
		Created: time.Date(2026, 8, 26, 9, 0, 0, 0, time.UTC),
	})
	drawDayPage(t, doc)

	data, err := doc.Bytes()
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	if missing := doc.MissingGlyphs(); len(missing) != 0 {
		t.Errorf("faces could not draw %q; the sample must only use glyphs IBM Plex ships", string(missing))
	}
	assertValidPDF(t, data, 2)

	dir := "testdata"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating testdata: %v", err)
	}
	path := filepath.Join(dir, "smoke.pdf")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	t.Logf("wrote %s (%d bytes)", path, len(data))
}

// drawDayPage paints the sample. It is shared with the determinism test, which
// needs two runs of exactly the same drawing.
func drawDayPage(t *testing.T, doc *Document) {
	t.Helper()
	registry := fonts.NewRegistry()
	sansBold := mustFace(t, registry, "sans", fonts.Bold)
	sans := mustFace(t, registry, "sans", fonts.Regular)
	mono := mustFace(t, registry, "mono", fonts.Regular)
	serifItalic := mustFace(t, registry, "serif", fonts.Italic)

	const (
		titleSize = 44 // quarter-points, so 11pt
		bodySize  = 36 // 9pt
		dateSize  = 32 // 8pt
		linePitch = geom.TicksPerPt * 18
	)
	var (
		ink      = paint.GrayN(0.1)
		ruleGray = paint.GrayN(0.72)
		hairline = render.Stroke{Color: ruleGray, Width: geom.Pt(0.25)}
		border   = render.Stroke{Color: paint.GrayN(0.25), Width: geom.Pt(1)}
		dashed   = render.Stroke{
			Color: paint.GrayN(0.5),
			Width: geom.Pt(0.4),
			Dash:  []geom.Tick{geom.Pt(2), geom.Pt(2)},
		}
	)

	size, _ := NamedPageSize("letter")
	canvas := doc.AddPage(size)
	doc.AddOutline("Wednesday 26 August")

	panel := geom.Rect{X: geom.In(0.75), Y: geom.In(0.75), W: geom.In(4.5), H: geom.In(6)}
	FillPanel(canvas, panel, geom.Pt(6), paint.White, border)

	content := panel.InsetUniform(geom.Pt(10))
	baseline := content.Y + sansBold.Ascent(titleSize)
	canvas.DrawText(content.X, baseline, render.TextRun{
		Text: "WEDNESDAY", Face: sansBold, SizeQpt: titleSize, Color: ink, Tracking: geom.Pt(0.8),
	})

	// Right-aligned by measurement, which is the whole reason Face.Width and
	// the PDF writer have to agree about advance widths.
	date := render.TextRun{Text: "2026-08-26", Face: mono, SizeQpt: dateSize, Color: paint.GrayN(0.35)}
	canvas.DrawText(content.Right()-date.Width(), baseline, date)

	// Eight hairlines at an exact pitch. These go through the batch, so the
	// whole panel shares one pen setup no matter how many rules it has.
	ruleTop := content.Y + geom.Pt(34)
	for i := 0; i < 8; i++ {
		StrokeHorizontalRule(canvas, content, ruleTop+geom.Tick(i)*linePitch, hairline)
	}
	canvas.FlushLines()

	dashY := ruleTop + 8*linePitch + geom.Pt(6)
	StrokeHorizontalRule(canvas, content, dashY, dashed)
	canvas.FlushLines()

	// Vector checkboxes: squares drawn as paths, not glyphs, so they are the
	// same on every machine and cannot go missing from a font.
	boxPen := render.Stroke{Color: paint.GrayN(0.3), Width: geom.Pt(0.6)}
	const boxSide = geom.TicksPerPt * 8
	for i, label := range []string{"Water the ficus", "Ship the DSL parser", "Resume the naive cafe rewrite"} {
		top := dashY + geom.Pt(12) + geom.Tick(i)*geom.Pt(16)
		box := geom.Rect{X: content.X, Y: top, W: boxSide, H: boxSide}
		FillPanel(canvas, box, geom.Pt(1.5), paint.White, boxPen)
		canvas.DrawText(box.Right()+geom.Pt(5), top+sans.Ascent(bodySize), render.TextRun{
			Text: label, Face: sans, SizeQpt: bodySize, Color: ink,
		})
	}

	// A clipped note, to exercise the graphics state stack.
	canvas.Save()
	canvas.AddRect(geom.Rect{X: geom.In(5.5), Y: geom.In(0.75), W: geom.In(2), H: geom.In(1)}, 0)
	canvas.Clip()
	canvas.DrawText(geom.In(5.5), geom.In(1), render.TextRun{
		Text: "clipped marginalia that runs past the edge",
		Face: serifItalic, SizeQpt: bodySize, Color: paint.GrayN(0.45),
	})
	canvas.Restore()

	// A second page, landscape and a different size, to prove page setup and a
	// font first used after page one both work.
	a5, _ := NamedPageSize("a5")
	second := doc.AddPage(a5.Landscape())
	doc.AddOutline("Notes")
	second.DrawText(geom.In(0.5), geom.In(0.75), render.TextRun{
		Text: "A5 landscape, second page", Face: serifItalic, SizeQpt: titleSize, Color: ink,
	})
}

func mustFace(t *testing.T, registry *fonts.Registry, family string, style fonts.Style) *fonts.Face {
	t.Helper()
	face, got, err := registry.Resolve(family, style)
	if err != nil {
		t.Fatalf("resolving %s %v: %v", family, style, err)
	}
	if got != style {
		t.Fatalf("resolving %s %v substituted %v", family, style, got)
	}
	return face
}

// ---- Structural checks ----

// assertValidPDF checks the parts of the file structure that this package is
// responsible for and that a broken trailer patch would silently corrupt:
// the header, the trailer's own fields, and — the one that matters — that
// every cross-reference offset still lands on the object it claims to.
func assertValidPDF(t *testing.T, data []byte, wantPages int) {
	t.Helper()
	if len(data) == 0 {
		t.Fatal("empty output")
	}
	if !bytes.HasPrefix(data, []byte("%PDF-1.7")) {
		t.Errorf("missing PDF header, starts with %q", data[:min(16, len(data))])
	}
	if !bytes.HasSuffix(data, []byte("%%EOF\n")) {
		t.Error("missing end-of-file marker")
	}
	for _, want := range []string{"/ID [<", "/ModDate(D:", "/CreationDate(D:", "/Root 1 0 R"} {
		if !bytes.Contains(data, []byte(want)) {
			t.Errorf("trailer is missing %q", want)
		}
	}
	if got := bytes.Count(data, []byte("/Type /Page\n")); got != wantPages {
		t.Errorf("found %d page objects, want %d", got, wantPages)
	}
	assertXrefOffsets(t, data)
}

// assertXrefOffsets walks the cross-reference table and checks that each entry
// points at the start of the object with that number. If the trailer patch in
// trailer.go ever shifted a byte that something else refers to, this is what
// would catch it.
func assertXrefOffsets(t *testing.T, data []byte) {
	t.Helper()
	startAt := bytes.LastIndex(data, []byte("\nstartxref\n"))
	if startAt < 0 {
		t.Fatal("no startxref")
	}
	tail := string(data[startAt+len("\nstartxref\n"):])
	offsetText, _, _ := strings.Cut(tail, "\n")
	xrefOffset, err := strconv.Atoi(strings.TrimSpace(offsetText))
	if err != nil {
		t.Fatalf("unreadable startxref %q: %v", offsetText, err)
	}
	if xrefOffset <= 0 || xrefOffset >= len(data) {
		t.Fatalf("startxref %d is outside the file (%d bytes)", xrefOffset, len(data))
	}
	if !bytes.HasPrefix(data[xrefOffset:], []byte("xref\n")) {
		t.Fatalf("startxref %d does not point at the xref table", xrefOffset)
	}

	lines := strings.Split(string(data[xrefOffset:]), "\n")
	// lines[0] is "xref", lines[1] is "0 N", lines[2] is the free entry.
	for i := 3; i < len(lines); i++ {
		if !strings.HasSuffix(lines[i], " 00000 n ") {
			break
		}
		offset, err := strconv.Atoi(strings.TrimSpace(strings.Fields(lines[i])[0]))
		if err != nil {
			t.Fatalf("unreadable xref entry %q: %v", lines[i], err)
		}
		objectNumber := i - 2
		want := fmt.Sprintf("%d 0 obj\n", objectNumber)
		if !bytes.HasPrefix(data[offset:], []byte(want)) {
			t.Fatalf("xref entry for object %d points at %d, which reads %q",
				objectNumber, offset, data[offset:min(offset+24, len(data))])
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
