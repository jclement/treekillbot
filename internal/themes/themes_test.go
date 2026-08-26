package themes

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jclement/treekillbot/internal/compile"
	"github.com/jclement/treekillbot/internal/pipeline"
	"github.com/jclement/treekillbot/internal/pulp"
	"github.com/jclement/treekillbot/internal/schema"
)

// exercise is a document that touches all five semantic slots a theme has to
// define: body text, a framed panel, its title, a writing surface and the
// sheet. It is deliberately small and self-contained rather than an example
// from the repo, so that a theme test fails for a reason in this package.
const exercise = `page
  size: letter
  margin: 0.5in

section
  height: auto
  text "Heading"
    font-size: 14pt
    font-weight: bold

section
  height: fill
  gap: 8pt
  column
    panel "Ruled"
      height: fill
      border-width: 0.5pt
      padding: 6pt
      line-style: ruled
  column
    panel "Dots"
      height: fill
      border-width: 0.5pt
      padding: 6pt
      line-style: dotted
`

// TestBuiltinThemesRender is the acceptance test for the theme library: every
// embedded theme must load and then render a real document with no error
// diagnostics. A theme that parses but produces an unreadable page is not a
// theme, so the rendered PDF is required to be non-empty as well.
func TestBuiltinThemesRender(t *testing.T) {
	for _, name := range builtinNames(t) {
		t.Run(name, func(t *testing.T) {
			props, err := Load(name)
			if err != nil {
				t.Fatalf("Load(%q): %v", name, err)
			}
			if props == nil {
				t.Fatalf("Load(%q) returned no properties", name)
			}

			src := pulp.NewSource(name+"-exercise.pulp", exercise)
			result, err := pipeline.Build(src, pipeline.StageRender, pipeline.Options{Theme: props})
			if err != nil {
				t.Fatalf("building with theme %q: %v", name, err)
			}
			for _, d := range result.Diags {
				if d.Severity == pulp.SeverityError {
					t.Errorf("theme %q: %s", name, d.Plain())
				}
			}
			if len(result.PDF) == 0 {
				t.Errorf("theme %q produced an empty PDF", name)
			}
		})
	}
}

// TestBuiltinThemesDefineEveryInkSlot guards the promise that makes --theme
// worth having: a document swapped from one theme to another must not lose a
// colour it was relying on. Every theme therefore states all five slots, even
// where its answer is the same as the built-in default.
func TestBuiltinThemesDefineEveryInkSlot(t *testing.T) {
	slots := []struct {
		id   schema.PropID
		what string
	}{
		{schema.PColor, "body ink"},
		{schema.PLineColor, "the writing surface"},
		{schema.PBorderColor, "frames"},
		{schema.PTitleColor, "labels"},
		{schema.PBackground, "the sheet"},
	}

	for _, name := range builtinNames(t) {
		t.Run(name, func(t *testing.T) {
			props, err := Load(name)
			if err != nil {
				t.Fatalf("Load(%q): %v", name, err)
			}
			for _, slot := range slots {
				if !props.Global.Has(slot.id) {
					t.Errorf("theme %q does not set %s (%s)", name, schema.Name(slot.id), slot.what)
				}
			}
		})
	}
}

// TestBuiltinThemesAreDescribed checks the listing convention: `themes` prints
// each theme's first comment line, so a file without one lists as a blank row.
func TestBuiltinThemesAreDescribed(t *testing.T) {
	for _, theme := range Available() {
		if theme.Description == "" {
			t.Errorf("theme %q has no description; its first line should be a `# …` sentence", theme.Name)
		}
	}
}

// TestDefaultThemeHasNoFills is DESIGN.md D10 as a test. The `default` theme
// ships with no panel fills at all, because a tint that looks like a whisper on
// a monitor halftones two or three steps darker on plain stock and the first
// thing anyone does with a planner is write on it.
func TestDefaultThemeHasNoFills(t *testing.T) {
	props, err := Load("default")
	if err != nil {
		t.Fatalf("Load(default): %v", err)
	}
	if background, ok := props.Global.First(schema.PBackground); ok && !background.Color.IsInvisible() {
		t.Errorf("the default theme fills the sheet with %s; D10 says it ships with no fills", background.Raw)
	}
	if title, ok := props.Global.First(schema.PTitleBackground); ok && !title.Color.IsInvisible() {
		t.Errorf("the default theme fills title bars with %s; D10 says it ships with no fills", title.Raw)
	}
}

// TestDefaultThemeIsANoOp is the promise the default theme's own header makes:
// it is the schema's built-in defaults written out, so naming it must not change
// a single mark on the page. Without this, `default` drifts into being a second
// opinion layered over the first and `--theme default` silently restyles a
// document — the one behaviour nobody would think to check for.
//
// The comparison is the rendered PDF rather than the layout dump, because the
// difference a theme makes is colour and weight, which the rect tree does not
// record. Output is byte-identical for identical input (DESIGN.md section 4),
// so this is a fair test rather than a flaky one.
func TestDefaultThemeIsANoOp(t *testing.T) {
	props, err := Load("default")
	if err != nil {
		t.Fatalf("Load(default): %v", err)
	}

	render := func(theme *compile.ThemeLayer) []byte {
		t.Helper()
		src := pulp.NewSource("exercise.pulp", exercise)
		result, err := pipeline.Build(src, pipeline.StageRender, pipeline.Options{Theme: theme})
		if err != nil {
			t.Fatalf("building: %v", err)
		}
		if result.Diags.HasErrors() {
			t.Fatalf("building produced errors: %v", result.Diags)
		}
		return result.PDF
	}

	if !bytes.Equal(render(nil), render(props)) {
		t.Error("`--theme default` changed the output; it is meant to restate the built-in defaults exactly")
	}
}

// TestAvailableIsSorted pins the ordering promise. Map order reaching a listing
// is the kind of nondeterminism that only shows up in someone else's diff.
func TestAvailableIsSorted(t *testing.T) {
	names := Names()
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Fatalf("Available() is not sorted: %q came before %q", names[i-1], names[i])
		}
	}
}

// TestAvailableAgreesWithTheLoader is the property Available()'s doc comment
// claims: a theme that shows up in the listing must be one --theme can actually
// resolve. They were two separate directory walks once, and the working
// directory was in one of them and not the other.
func TestAvailableAgreesWithTheLoader(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	source := "# Mine, beside the document.\n\ndefaults\n  color: gray(0.4)\n"
	if err := os.WriteFile(filepath.Join(dir, "mine"+Extension), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	var listed *Theme
	for _, theme := range Available() {
		if theme.Name == "mine" {
			listed = &theme
		}
	}
	if listed == nil {
		t.Fatal("a theme in the working directory was not listed")
	}
	if listed.Description != "Mine, beside the document." {
		t.Errorf("description is %q", listed.Description)
	}
	if listed.Origin == BuiltinOrigin {
		t.Error("a theme read off disk is listed as built-in")
	}
	if _, err := Load("mine"); err != nil {
		t.Errorf("Load could not resolve a theme the listing offered: %v", err)
	}
}

func TestLoadEmptyNameIsNotATheme(t *testing.T) {
	props, err := Load("")
	if err != nil || props != nil {
		t.Fatalf("Load(\"\") = %v, %v; want nil, nil so an unset --theme passes straight through", props, err)
	}
}

func TestLoadUnknownThemeSuggests(t *testing.T) {
	_, err := Load("midnght")
	if err == nil {
		t.Fatal("Load(\"midnght\") succeeded; want an unknown-theme error")
	}
	if !strings.Contains(err.Error(), "midnight") {
		t.Errorf("error does not suggest `midnight`: %v", err)
	}
}

// TestLoadRejectsPathTraversal covers the one security-shaped decision in this
// package: a theme name becomes a path segment.
func TestLoadRejectsPathTraversal(t *testing.T) {
	for _, name := range []string{"../../etc/passwd", "a/b", "Mono", "~/x"} {
		if _, err := Load(name); err == nil {
			t.Errorf("Load(%q) succeeded; want a rejected name", name)
		}
	}
}

// ---- The theme contract ----

// TestThemeContractRefusals is the interesting half of this package: each of
// these is a theme that parses perfectly and would quietly wreck any document
// it was applied to. The message has to name the fix, so it is asserted too.
func TestThemeContractRefusals(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{"border width frames every section", "defaults\n  border-width: 0.5pt\n", "border-width"},
		{"border shorthand can carry a width", "defaults\n  border: 0.5pt solid gray(0.7)\n", "border-color"},
		{"padding pads every box", "defaults\n  padding: 6pt\n", "defaults panel"},
		{"line-style rules the whole page", "defaults\n  line-style: dotted\n", "defaults panel"},
		{"margin-rule is a decoration too", "defaults\n  margin-rule: true\n", "defaults panel"},
		{"page size belongs to the document", "defaults\n  size: a4\n", "Page setup"},
		{"defaults must narrow to a real element", "defaults pannel\n  border-color: gray(0.5)\n", "panel"},
		{"a style block needs a name", "style\n  color: gray(0.2)\n", "needs an argument"},
		{"a theme is not a document", "page\n  size: a4\n", "not one"},
	}

	dir := t.TempDir()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, "probe"+Extension)
			if err := os.WriteFile(path, []byte(tc.source), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := LoadFrom(dir, "probe")
			if err == nil {
				t.Fatalf("LoadFrom accepted:\n%s", tc.source)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error does not mention %q:\n%v", tc.want, err)
			}
		})
	}
}

// A per-element block is what makes a theme able to say the most ordinary thing
// a theme wants to say — "panels have a hairline border" — without framing every
// section and column on the page. It used to be refused because the theme layer
// was a single flat property bag with nowhere for it to go.
func TestThemeCanNarrowToAnElement(t *testing.T) {
	dir := t.TempDir()
	source := "defaults\n  color: gray(0.1)\n\ndefaults panel\n  border-width: 0.5pt\n  padding: 6pt\n"
	if err := os.WriteFile(filepath.Join(dir, "narrow"+Extension), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	layer, err := LoadFrom(dir, "narrow")
	if err != nil {
		t.Fatalf("a theme must be able to narrow to an element: %v", err)
	}
	panel, ok := layer.ByType["panel"]
	if !ok {
		t.Fatal("the `defaults panel` block did not reach the theme layer")
	}
	if !panel.Has(schema.PBorderWidth) || !panel.Has(schema.PPadding) {
		t.Fatal("the per-element block lost its properties")
	}
	// And the box metrics must NOT have leaked into the global block, or they
	// would frame every section on the page.
	if layer.Global.Has(schema.PBorderWidth) {
		t.Fatal("a per-element border-width leaked into the global block")
	}
}

// A style bundle in a theme is applied only when a document names it, so the
// global-block restrictions do not apply: being ruled is what `style: ruled`
// asked for.
func TestThemeCanDefineStyles(t *testing.T) {
	dir := t.TempDir()
	source := "defaults\n  color: gray(0.1)\n\nstyle ruled\n  line-style: ruled\n  line-pitch: 15pt\n"
	if err := os.WriteFile(filepath.Join(dir, "styled"+Extension), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	layer, err := LoadFrom(dir, "styled")
	if err != nil {
		t.Fatalf("a theme must be able to define a style: %v", err)
	}
	bundle, ok := layer.Styles["ruled"]
	if !ok {
		t.Fatalf("the style did not reach the theme layer; styles are %v", layer.Styles)
	}
	if !bundle.Has(schema.PLineStyle) {
		t.Fatal("the style bundle lost its properties")
	}
}

// TestLoadRefusesAnEmptyTheme covers the shell footgun described in layer():
// a redirect truncates the file before the command that would fill it runs, and
// a silently-empty theme means every build afterwards is unthemed with nothing
// anywhere saying so.
func TestLoadRefusesAnEmptyTheme(t *testing.T) {
	dir := t.TempDir()
	for name, source := range map[string]string{
		"empty":    "",
		"comments": "# All talk.\n#\n# No defaults.\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name+Extension), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := LoadFrom(dir, name)
		if err == nil {
			t.Errorf("LoadFrom(%q) accepted a theme that sets nothing", name)
			continue
		}
		if !strings.Contains(err.Error(), "sets no properties") {
			t.Errorf("the error should say the theme is empty; got: %v", err)
		}
	}
}

// TestUserThemeShadowsBuiltin covers the deliberate override in DESIGN.md D13:
// a file called default.pulptheme beside the document wins over the built-in.
func TestUserThemeShadowsBuiltin(t *testing.T) {
	dir := t.TempDir()
	source := "# Mine.\n\ndefaults\n  color: gray(0.5)\n"
	if err := os.WriteFile(filepath.Join(dir, "default"+Extension), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	props, err := LoadFrom(dir, "default")
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if props.Global.Has(schema.PLineColor) {
		t.Error("the built-in default theme leaked through; a user theme replaces it wholesale")
	}
	value, ok := props.Global.First(schema.PColor)
	if !ok || value.Color.IsInvisible() {
		t.Fatal("the user theme's colour was not loaded")
	}
}

// TestLoadOnlyKeepsWhatTheThemeSaid guards the boundary between what a theme
// said and what the schema would have defaulted to anyway. Passing the built-in
// defaults on would make the theme look responsible for values it never
// mentioned, and `--explain-property` would blame it for them.
func TestLoadOnlyKeepsWhatTheThemeSaid(t *testing.T) {
	dir := t.TempDir()
	source := "# Minimal.\n\ndefaults\n  color: gray(0.4)\n"
	if err := os.WriteFile(filepath.Join(dir, "minimal"+Extension), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	props, err := LoadFrom(dir, "minimal")
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if got := props.Global.SetIDs(); len(got) != 1 || got[0] != schema.PColor {
		names := make([]string, 0, len(got))
		for _, id := range got {
			names = append(names, schema.Name(id))
		}
		t.Errorf("theme carries %v; want only color", names)
	}
}

func TestSourceReturnsTheWholeFile(t *testing.T) {
	text, err := Source("mono")
	if err != nil {
		t.Fatalf("Source(mono): %v", err)
	}
	if !strings.HasPrefix(text, "#") {
		t.Error("Source did not return the file's comments; --show exists to be copied and edited")
	}
	if !strings.Contains(text, "defaults") {
		t.Error("Source returned no defaults block")
	}
}

// builtinNames lists the embedded themes, so a new .pulptheme file is covered
// by every test above the moment it is added.
func builtinNames(t *testing.T) []string {
	t.Helper()
	entries, err := builtinFS.ReadDir(builtinDir)
	if err != nil {
		t.Fatalf("reading embedded themes: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, strings.TrimSuffix(entry.Name(), Extension))
	}
	if len(names) == 0 {
		t.Fatal("no embedded themes")
	}
	return names
}
