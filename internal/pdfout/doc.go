// Package pdfout renders a laid-out document to PDF using signintech/gopdf.
//
// It is one of several render.Canvas implementations (see DESIGN.md section 1)
// and the only one that knows what a PDF is. Everything above it works in
// ticks with a top-left origin; everything gopdf-shaped stops here.
//
// Three things this package is responsible for that are easy to miss:
//
//   - Determinism. Two runs over the same document must produce identical
//     bytes, so the clock is never read, /ID is a hash of the body rather than
//     random, and the compression level is pinned. See DESIGN.md section 4 and
//     trailer.go.
//   - Stroke alignment. PDF centres strokes on their path; stroke.go provides
//     the two rules from DESIGN.md D4 so callers cannot pick the wrong one.
//   - Telling the truth about what could not be drawn. gopdf substitutes a
//     space for a missing glyph and says nothing; Document collects them so
//     the CLI can warn.
//
// Because render.Canvas methods return nothing, a failure part-way through a
// page cannot be reported where it happens. Document latches the first error
// and returns it from Bytes; that is a deliberate concession to the interface,
// not a preference — see the note on Err.
package pdfout

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/signintech/gopdf"

	"github.com/jclement/treekillbot/internal/fonts"
	"github.com/jclement/treekillbot/internal/geom"
)

// pinnedCompressLevel is the flate level used for every content stream.
//
// It is pinned rather than left at zlib.DefaultCompression so that a change to
// Go's idea of "default" cannot silently move the byte hash. DESIGN.md section
// 4 takes the primary golden hash with compression off for the same reason, so
// a flate change fails one clearly-labelled test instead of the whole suite.
const pinnedCompressLevel = zlib.BestCompression

// defaultProducer identifies the tool in the document information dictionary.
// It carries no version: a version string would make every release's output
// differ from the last, which is exactly what the golden files exist to catch.
const defaultProducer = "treekillbot"

// Options configure a document. The zero value is a valid, deterministic,
// compressed, untitled document.
type Options struct {
	Title    string
	Author   string
	Subject  string
	Creator  string // the application that produced the source, if any
	Producer string // defaults to defaultProducer

	// Created fixes /CreationDate and /ModDate. The zero value means the Unix
	// epoch, NOT the current time: a PDF writer that reaches for the clock
	// unless told otherwise is one you have to remember to disable, and
	// forgetting shows up as a golden test that fails on a Tuesday. The CLI
	// fills this from --date or SOURCE_DATE_EPOCH.
	Created time.Time

	// NoCompress leaves content streams as plain text. Slower and larger, but
	// it makes the output readable, which is what you want when a golden hash
	// changes and you need to know why.
	NoCompress bool

	// Grayscale converts every colour to its DeviceGray equivalent on the way
	// out. It is a property of a print run rather than of the document, which
	// is why it lives here and not in the theme (DESIGN.md D10).
	Grayscale bool
}

// Document is a PDF under construction. It is not safe for concurrent use;
// painting is ordered, and so is the object numbering that depends on it.
type Document struct {
	opts Options
	pdf  *gopdf.GoPdf

	pages   []*Canvas
	current *Canvas

	// registered maps a face key to the family name gopdf knows it by. Faces
	// are registered on first use, so the subset objects — and therefore the
	// PDF object numbers — follow the drawing order, which is itself fixed by
	// the layout traversal.
	registered map[string]string

	// missing collects runes no face could render. gopdf calls
	// OnGlyphNotFound from inside its writer, so the mutex is not decoration.
	missingMu sync.Mutex
	missing   map[rune]bool

	err    error
	output []byte
}

// New starts a document. No page exists yet; call AddPage.
func New(opts Options) *Document {
	if opts.Producer == "" {
		opts.Producer = defaultProducer
	}
	if opts.Created.IsZero() {
		opts.Created = time.Unix(0, 0).UTC()
	}

	pdf := &gopdf.GoPdf{}
	// A nominal page size is required at Start even though every page carries
	// its own; gopdf uses config.PageSize only as the default for AddPage.
	pdf.Start(gopdf.Config{Unit: gopdf.UnitPT, PageSize: gopdf.Rect{W: 612, H: 792}})
	if opts.NoCompress {
		pdf.SetNoCompression()
	} else {
		pdf.SetCompressLevel(pinnedCompressLevel)
	}
	pdf.SetInfo(gopdf.PdfInfo{
		Title:        opts.Title,
		Author:       opts.Author,
		Subject:      opts.Subject,
		Creator:      opts.Creator,
		Producer:     opts.Producer,
		CreationDate: opts.Created,
	})

	return &Document{
		opts:       opts,
		pdf:        pdf,
		registered: make(map[string]string, 4),
		missing:    make(map[rune]bool),
	}
}

// AddPage starts a new page and returns the canvas that paints it.
//
// Any lines still batched on the previous page are flushed first, so a caller
// that forgets FlushLines loses nothing. The returned canvas stays usable
// after the next AddPage only in the sense that gopdf's cursor has moved on:
// paint the pages in order.
func (d *Document) AddPage(size PageSize) *Canvas {
	if d.current != nil {
		d.current.FlushLines()
	}
	if !size.IsValid() {
		d.fail(fmt.Errorf("page %d has an empty media box (%s)", len(d.pages)+1, size.Name))
		size = PageSize{W: geom.Pt(612), H: geom.Pt(792)}
	}
	d.pdf.AddPageWithOption(gopdf.PageOption{
		PageSize: &gopdf.Rect{W: size.W.Points(), H: size.H.Points()},
	})
	canvas := newCanvas(d, size)
	d.pages = append(d.pages, canvas)
	d.current = canvas
	return canvas
}

// AddOutline adds a bookmark pointing at the page most recently added, which
// is how a fifty-page planner stays navigable in a viewer.
func (d *Document) AddOutline(title string) {
	if len(d.pages) == 0 {
		d.fail(fmt.Errorf("outline %q added before any page", title))
		return
	}
	d.pdf.AddOutline(title)
}

// PageCount returns the number of pages added so far.
func (d *Document) PageCount() int { return len(d.pages) }

// MissingGlyphs returns every rune the document asked for and no face could
// draw, sorted by code point.
//
// This exists because gopdf's default behaviour is to substitute a space and
// carry on, so a form whose checkbox character is absent from the chosen face
// prints as a blank with nothing anywhere saying so. The CLI turns this into a
// warning; call it after Bytes, since some substitutions are only discovered
// while the content stream is written.
func (d *Document) MissingGlyphs() []rune {
	d.missingMu.Lock()
	defer d.missingMu.Unlock()
	out := make([]rune, 0, len(d.missing))
	for r := range d.missing {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Err returns the first error that occurred while painting.
//
// render.Canvas methods return nothing, so an error raised mid-page — an
// unregisterable font, an impossible rectangle — has nowhere to go at the
// point it happens. Latching one is the fpdf behaviour DESIGN.md D7 rejects,
// so it is contained as tightly as possible: the FIRST error is kept, later
// painting still happens, and Bytes refuses to produce a file.
func (d *Document) Err() error { return d.err }

func (d *Document) fail(err error) {
	if d.err == nil {
		d.err = err
	}
}

// Bytes renders the document. It is idempotent: gopdf's compile step mutates
// the object graph, so the result is produced once and cached.
func (d *Document) Bytes() ([]byte, error) {
	if d.output != nil {
		return d.output, d.err
	}
	if d.current != nil {
		d.current.FlushLines()
	}
	if len(d.pages) == 0 {
		return nil, fmt.Errorf("document has no pages")
	}
	if d.err != nil {
		return nil, d.err
	}

	var buf bytes.Buffer
	if _, err := d.pdf.WriteTo(&buf); err != nil {
		d.fail(fmt.Errorf("writing pdf: %w", err))
		return nil, d.err
	}
	out, err := finalizeTrailer(buf.Bytes(), d.opts.Created)
	if err != nil {
		d.fail(err)
		return nil, d.err
	}
	d.output = out
	return d.output, nil
}

// WriteTo renders the document to w, satisfying io.WriterTo.
func (d *Document) WriteTo(w io.Writer) (int64, error) {
	data, err := d.Bytes()
	if err != nil {
		return 0, err
	}
	n, err := w.Write(data)
	return int64(n), err
}

// useFont registers a face with gopdf on first use and returns the family name
// to pass to SetFontWithStyle.
//
// Registration order fixes the order of the font objects in the file, and
// therefore the object numbers. That is deterministic because it follows the
// drawing order, which follows the layout traversal — DESIGN.md section 4's
// "assigned by first use in a canonical traversal".
func (d *Document) useFont(face *fonts.Face) (string, error) {
	key := face.Name + "\x00" + face.Style.String()
	if family, ok := d.registered[key]; ok {
		return family, nil
	}
	option := gopdf.TtfOption{
		Style:           gopdfStyle(face.Style),
		OnGlyphNotFound: d.noteMissingGlyph,
	}
	if err := d.pdf.AddTTFFontDataWithOption(face.Name, face.Data, option); err != nil {
		return "", fmt.Errorf("embedding font %s %s: %w", face.Name, face.Style, err)
	}
	d.registered[key] = face.Name
	return face.Name, nil
}

func (d *Document) noteMissingGlyph(r rune) {
	d.missingMu.Lock()
	d.missing[r] = true
	d.missingMu.Unlock()
}

// gopdfStyle maps our four static styles onto gopdf's style bits. gopdf treats
// bold and italic as selectors for a separately registered file rather than as
// synthetic effects, which is what we want: there is no faux-bold here.
func gopdfStyle(style fonts.Style) int {
	switch style {
	case fonts.Bold:
		return gopdf.Bold
	case fonts.Italic:
		return gopdf.Italic
	case fonts.BoldItalic:
		return gopdf.Bold | gopdf.Italic
	default:
		return gopdf.Regular
	}
}
