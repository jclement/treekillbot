// The build pipeline: source text in, PDF bytes out.
//
// Every entry point — the `build` command, `check`, the watch loop, the HTML
// preview server — runs the same stages in the same order, so none of them can
// drift from the others. The stages are separable because each one's output is
// a value rather than a mutation, which is what lets `check` stop after
// validation and the preview server stop before the PDF writer.
package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jclement/treekillbot/internal/compile"
	"github.com/jclement/treekillbot/internal/decor"
	"github.com/jclement/treekillbot/internal/draw"
	"github.com/jclement/treekillbot/internal/fonts"
	"github.com/jclement/treekillbot/internal/geom"
	"github.com/jclement/treekillbot/internal/layout"
	"github.com/jclement/treekillbot/internal/pulp"
	"github.com/jclement/treekillbot/internal/render"
	"github.com/jclement/treekillbot/internal/schema"
	"github.com/jclement/treekillbot/internal/vars"
)

// Options configure a build.
type Options struct {
	// Scope resolves variables. When nil, one is built from the fields below,
	// which is what every caller except a test wants.
	Scope compile.Scope

	// Anchor is the date every date built-in derives from. The zero value means
	// now. Setting it — from --date or SOURCE_DATE_EPOCH — makes the whole
	// document reproducible, which is the same flag that prints next week's page
	// today.
	Anchor time.Time
	// WeekStart rotates the displayed week without moving the ISO week number.
	WeekStart string
	// Vars are the --var and --vars-file assignments, highest priority.
	Vars map[string]string
	// AllowUndefined downgrades an unresolved reference to a warning.
	AllowUndefined bool
	// UnsafeEnv permits undeclared env.* lookups.
	UnsafeEnv bool
	// Theme is applied beneath the document's own defaults.
	Theme *compile.ThemeLayer
	// PageSize overrides the document's declared size.
	PageSize *compile.PageSize
	// FontDir adds user fonts that shadow the built-in families.
	FontDir string
	// ThemeDir is where a `theme` directive in the document looks first,
	// normally the document's own directory.
	ThemeDir string
	// ResolveTheme loads a theme by name for a `theme` directive in the
	// document. It is injected rather than imported so the pipeline does not
	// depend on the theme registry — which also keeps the registry free to
	// depend on the pipeline in its own tests.
	ResolveTheme func(dir, name string) (*compile.ThemeLayer, error)

	Created       time.Time
	Grayscale     bool
	NoCompress    bool
	DebugLayout   bool
	AllowOverflow bool

	// Repeat renders several pages from one document, advancing the date anchor
	// by Step each time. This is what turns a weekly template into a quarter's
	// worth of spreads in one job, which is how anyone actually uses a planner
	// generator.
	Repeat int
	// Step is how far the anchor moves between pages: 1d, 2w, 1m, 1y.
	Step string

	Title  string
	Author string
	// Creator names the application that produced the document, which shows up
	// in a PDF viewer's properties panel.
	Creator string
	// Orientation overrides the document's own, for --orientation.
	Orientation string
}

// Result is everything a build produced.
type Result struct {
	Document *compile.Result
	PDF      []byte
	Diags    pulp.Diagnostics

	PageCount     int
	PageSize      compile.PageSize
	MissingGlyphs []rune
	// FontsUsed counts the distinct faces embedded, for the build summary.
	FontsUsed int
	// LayoutDump is the rectangle tree, the primary golden-file format.
	LayoutDump string

	Timings Timings

	// resolver and grid are retained so a second Canvas — the SVG preview, the
	// op recorder — can be painted from an already-built result without
	// recompiling. They are unexported because nothing outside this package
	// should be choosing its own font registry for a document that has already
	// been laid out with a different one.
	resolver *fontResolver
	grid     decor.Grid
}

// RenderTo paints an already-built result onto another canvas.
//
// This is what makes the browser preview faithful: it is the same tree, the
// same computed rectangles and the same painting code that produced the PDF,
// with only the Canvas swapped. Rebuilding for each output would leave room for
// the two to disagree.
func RenderTo(result *Result, canvas render.Canvas, opts Options) {
	if result == nil || result.Document == nil || result.resolver == nil {
		return
	}
	Render(result.Document.Root, canvas, result.resolver, result.grid, opts)
}

// Timings records how long each stage took, for the build summary.
type Timings struct {
	Parse, Validate, Compile, Layout, Render time.Duration
}

// Total returns the sum of every stage.
func (t Timings) Total() time.Duration {
	return t.Parse + t.Validate + t.Compile + t.Layout + t.Render
}

// Stage names how far a build got, so a caller can stop early.
type Stage uint8

const (
	StageValidate Stage = iota // parse and check, no layout
	StageLayout                // through layout, no PDF
	// StageRender goes all the way to PDF bytes. Build delegates it to
	// BuildDocument rather than carrying a second rendering path, so there is
	// exactly one place that drives the PDF writer.
	StageRender
)

// BuildFile reads a file and runs it through the pipeline as far as stage.
// To produce a PDF, use BuildDocumentFile.
func BuildFile(path string, stage Stage, opts Options) (*Result, error) {
	text, err := readFile(path)
	if err != nil {
		return nil, err
	}
	return Build(pulp.NewSource(path, text), stage, opts)
}

// readFile loads a document, wrapping the error with the path so a missing file
// says which one.
func readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	return string(data), nil
}

// Build runs the pipeline over an already-loaded source.
//
// It returns a Result even when diagnostics contain errors, so that a caller
// can report several problems at once and so the preview server can show a
// best-effort page while the author is mid-edit. Check Result.Diags.HasErrors
// before trusting the output.
func Build(src *pulp.Source, stage Stage, opts Options) (*Result, error) {
	if stage == StageRender {
		return BuildDocument(src, opts)
	}
	pages := opts.Repeat
	if pages < 1 {
		pages = 1
	}
	return buildPage(src, stage, opts, pageContext{number: 1, count: pages})
}

// pageContext is which page of a repeated run is being built.
type pageContext struct {
	number, count int
	// anchorShift moves this page's date anchor away from the run's, which is
	// how a repeated weekly ends up being a different week each time.
	anchorShift stepOffset
}

func buildPage(src *pulp.Source, stage Stage, opts Options, page pageContext) (*Result, error) {
	result := &Result{}

	start := time.Now()
	doc, diags := pulp.Parse(src)
	result.Timings.Parse = time.Since(start)
	result.Diags = append(result.Diags, diags...)

	start = time.Now()
	result.Diags = append(result.Diags, schema.Validate(doc)...)
	result.Timings.Validate = time.Since(start)

	// A document that failed to parse has nothing worth compiling, and pressing
	// on would bury the real error under a hundred consequences of it.
	if result.Diags.HasErrors() {
		result.Diags.Sort()
		return result, nil
	}
	if stage == StageValidate {
		result.Diags.Sort()
		return result, nil
	}

	// A `theme` directive in the document is resolved here rather than in the
	// compiler, because the themes package imports the compiler and the reverse
	// would be a cycle. Doing it here also puts the precedence in the obvious
	// place: --theme on the command line beats what the document asked for,
	// since the person running the command is the more immediate authority.
	if opts.Theme == nil {
		theme, themeDiags := documentTheme(src, doc, opts.ThemeDir, opts.ResolveTheme)
		result.Diags = append(result.Diags, themeDiags...)
		if themeDiags.HasErrors() {
			result.Diags.Sort()
			return result, nil
		}
		opts.Theme = theme
	}

	scope := opts.Scope
	if scope == nil {
		scope = newScope(src, opts, page)
	}

	start = time.Now()
	compiled, compileDiags := compile.Compile(doc, compile.Options{
		Scope:    scope,
		Theme:    opts.Theme,
		PageSize: opts.PageSize,
	})
	result.Timings.Compile = time.Since(start)
	result.Diags = append(result.Diags, compileDiags...)
	result.Document = compiled
	result.PageSize = compiled.Page

	applyOrientation(compiled, opts.Orientation)
	result.PageSize = compiled.Page

	registry, err := loadFonts(opts.FontDir)
	if err != nil {
		return result, err
	}
	resolver := &fontResolver{registry: registry}

	start = time.Now()
	env := &layout.Env{
		Fonts:         resolver,
		Diags:         &result.Diags,
		AllowOverflow: opts.AllowOverflow,
		PageGrid: layout.PageGrid{
			Origin: compiled.Page.Rect(),
			Pitch:  geom.Mm(5),
		},
	}
	layout.Layout(compiled.Root, compiled.Page.Rect(), env)
	result.Timings.Layout = time.Since(start)
	result.resolver = resolver
	result.grid = pageGrid(compiled)
	result.LayoutDump = draw.DumpLayout(compiled.Root)

	result.Diags.Sort()
	return result, nil
}

// Render paints a laid-out tree onto any canvas. It is exported so the SVG
// preview and the op recorder can drive exactly the same painting code the PDF
// writer does — which is what makes the preview faithful rather than merely
// similar.
func Render(root *layout.Node, canvas render.Canvas, resolver layout.FontResolver, grid decor.Grid, opts Options) {
	draw.Paint(root, canvas, &draw.Env{
		Fonts:       resolver,
		Decor:       decorFactory(resolver, grid),
		Grayscale:   opts.Grayscale,
		DebugLayout: opts.DebugLayout,
	})
}

// documentTheme resolves a `theme <name>` directive in the document.
//
// Returning nil with no diagnostics when there is no directive is the common
// case and means "no theme", which is different from "the theme failed to
// load" — the second has to be an error, because rendering unthemed after
// someone asked for a theme is the kind of wrong that looks right.
func documentTheme(src *pulp.Source, doc *pulp.Document, themeDir string,
	resolve func(dir, name string) (*compile.ThemeLayer, error)) (*compile.ThemeLayer, pulp.Diagnostics) {

	var diags pulp.Diagnostics
	var directive *pulp.Node
	for _, node := range doc.TopLevel() {
		if node.Name == "theme" {
			directive = node // the last one wins, matching how defaults layer
		}
	}
	if directive == nil {
		return nil, nil
	}
	if !directive.HasArg {
		diags.Errorf(src, directive.NameSpan, "E153", "`theme` needs a name").
			WithLabel("no theme named").
			WithHelp("Write `theme mono`. Run `treekillbot themes` to see what is available.")
		return nil, diags
	}

	if resolve == nil {
		// No resolver was supplied, so nothing can load a theme. Saying so is
		// better than rendering unthemed and leaving the author to wonder.
		diags.Errorf(src, directive.NameSpan, "E154",
			"this build cannot load themes").
			WithLabel("no theme registry").
			WithHelp("Use `treekillbot build`, which supplies one.")
		return nil, diags
	}

	name := strings.Trim(directive.Arg, `"'`)
	if themeDir == "" {
		themeDir = filepath.Dir(src.Name)
	}
	layer, err := resolve(themeDir, name)
	if err != nil {
		diags.Errorf(src, directive.ArgSpan, "E154", "%v", err).
			WithLabel("theme not loaded")
		return nil, diags
	}
	return layer, nil
}

// newScope builds the variable scope from a build's options.
//
// The precedence ladder lives in internal/vars; all this does is feed each
// layer in. --var wins over everything because it is the most immediate thing
// the person running the command said.
func newScope(src *pulp.Source, opts Options, page pageContext) compile.Scope {
	anchor := opts.Anchor
	if anchor.IsZero() {
		anchor = time.Now()
	}
	anchor = page.anchorShift.apply(anchor)
	scopeOpts := vars.Options{
		AllowUndefined: opts.AllowUndefined,
		UnsafeEnv:      opts.UnsafeEnv,
		Doc:            vars.DocInfo{Path: src.Name, Name: documentName(src.Name)},
	}
	if weekday, ok := weekdayNamed(opts.WeekStart); ok {
		scopeOpts = scopeOpts.WithWeekStart(weekday)
	}

	scope := vars.NewScope(anchor, scopeOpts)
	// Theme constants sit just above the built-ins, so a document's own `vars`
	// block, --vars-file and --var all still outrank them.
	if opts.Theme != nil {
		for _, name := range sortedNames(opts.Theme.Constants) {
			scope.Define(name, vars.NewString(opts.Theme.Constants[name]), vars.LayerTheme)
		}
	}
	for name, value := range opts.Vars {
		scope.Define(name, vars.NewString(value), vars.LayerCLI)
	}
	// Page numbers are known before compiling here, because each page is a
	// separate compilation. The deferred-binding path in internal/vars exists
	// for callers that cannot do that; this one can.
	scope.BindPage(page.number, page.count)
	return scope
}

// sortedNames returns a map's keys in a fixed order, because map order must
// never reach the output (DESIGN.md section 4).
func sortedNames(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for name := range m {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// weekdayNamed maps the three week-start spellings the CLI accepts.
func weekdayNamed(name string) (time.Weekday, bool) {
	switch strings.ToLower(name) {
	case "monday":
		return time.Monday, true
	case "sunday":
		return time.Sunday, true
	case "saturday":
		return time.Saturday, true
	}
	return time.Monday, false
}

// documentName is the source's base name without its extension, exposed to
// documents as {doc.name} so a footer can identify the page it is on.
func documentName(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// applyOrientation forces a page into portrait or landscape, overriding the
// document. It swaps rather than reassigns so a custom size keeps its
// proportions.
func applyOrientation(compiled *compile.Result, orientation string) {
	switch orientation {
	case "portrait":
		if compiled.Page.Width > compiled.Page.Height {
			compiled.Page.Width, compiled.Page.Height = compiled.Page.Height, compiled.Page.Width
		}
	case "landscape":
		if compiled.Page.Height > compiled.Page.Width {
			compiled.Page.Width, compiled.Page.Height = compiled.Page.Height, compiled.Page.Width
		}
	}
}

// countFaces reports how many distinct faces a document actually drew with.
func countFaces(root *layout.Node, resolver *fontResolver) int {
	seen := map[string]bool{}
	root.Walk(func(n *layout.Node) bool {
		if wrapped := n.TextLayout(); wrapped != nil && wrapped.Face != nil {
			seen[wrapped.Face.Name+"/"+wrapped.Face.Style.String()] = true
		}
		return true
	})
	return len(seen)
}

// pageGrid returns the page-global lattice dot and graph decorations anchor to.
func pageGrid(compiled *compile.Result) decor.Grid {
	return decor.Grid{
		Origin: compiled.Page.Rect().Inset(compiled.Margin),
		Pitch:  geom.Mm(5),
	}
}

// loadFonts builds a registry, layering a user font directory over the built-in
// families so someone can substitute their own face without rebuilding.
func loadFonts(dir string) (*fonts.Registry, error) {
	registry := fonts.NewRegistry()
	if dir == "" {
		return registry, nil
	}
	if err := registry.LoadDir(dir); err != nil {
		return nil, fmt.Errorf("loading fonts from %s: %w", dir, err)
	}
	return registry, nil
}

// fontResolver adapts the registry to the single-return interface layout and
// painting use. The style substitution the registry reports is dropped here
// deliberately: it is a document-level observation the CLI reports once, not
// something to rediscover at every text run.
type fontResolver struct {
	registry *fonts.Registry
}

func (f *fontResolver) Resolve(family string, style fonts.Style) *fonts.Face {
	face, _, err := f.registry.Resolve(family, style)
	if err != nil {
		return nil
	}
	return face
}

// decorFactory bridges the decoration package to the painter's expectation.
//
// The two interfaces differ by one argument: decorations take the page-global
// lattice, because a dot grid must line up across adjacent panels rather than
// restarting inside each one, while the painter does not want to know that
// lattices exist. Binding the grid here is the whole of the adaptation.
func decorFactory(resolver layout.FontResolver, grid decor.Grid) draw.DecorFactory {
	return func(props *schema.Props, content geom.Rect) draw.Decorator {
		d, err := decor.New(props, resolver)
		if err != nil || d == nil {
			return nil
		}
		return boundDecoration{decoration: d, grid: grid}
	}
}

// boundDecoration pairs a decoration with the page lattice it draws against.
type boundDecoration struct {
	decoration decor.Decoration
	grid       decor.Grid
}

func (b boundDecoration) Draw(content geom.Rect, canvas render.Canvas) {
	b.decoration.Draw(content, b.grid, canvas)
}

func (b boundDecoration) Baselines(content geom.Rect) []geom.Tick {
	return b.decoration.Baselines(content)
}
