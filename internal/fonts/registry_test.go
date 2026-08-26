package fonts

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/jclement/treekillbot/internal/fonts/assets"
)

// Every embedded file must parse and report metrics a layout engine can use.
// A truncated download stays invisible until someone asks for that one style,
// which is a bad day months later rather than a red test now.
func TestEmbeddedFacesParseWithSaneMetrics(t *testing.T) {
	registry := NewRegistry()
	for _, builtin := range assets.Builtins {
		style, ok := ParseStyle(builtin.Style)
		if !ok {
			t.Fatalf("%s: unparseable style %q", builtin.File, builtin.Style)
		}
		t.Run(builtin.File, func(t *testing.T) {
			face, got, err := registry.Resolve(builtin.Family, style)
			if err != nil {
				t.Fatalf("Resolve(%q, %v): %v", builtin.Family, style, err)
			}
			if got != style {
				t.Fatalf("Resolve(%q, %v) substituted %v; the family should ship it", builtin.Family, style, got)
			}
			if face.UnitsPerEm() <= 0 {
				t.Errorf("unitsPerEm = %d, want > 0", face.UnitsPerEm())
			}
			if face.AscentFU() <= 0 {
				t.Errorf("ascent = %d font units, want > 0", face.AscentFU())
			}
			if face.DescentFU() <= 0 {
				t.Errorf("descent = %d font units, want > 0 (stored positive)", face.DescentFU())
			}
			if face.CapHeightFU() <= 0 {
				t.Errorf("cap height = %d font units, want > 0", face.CapHeightFU())
			}
			// Every glyph the shipped themes can reach must exist, or the PDF
			// writer silently draws a space where the checkbox should be.
			for _, r := range "AZaz09 .,-—“”·" {
				if !face.HasGlyph(r) {
					t.Errorf("face lacks glyph %q", r)
				}
			}
			wantMono := builtin.Family == "IBM Plex Mono"
			if face.IsMonospace() != wantMono {
				t.Errorf("IsMonospace() = %v, want %v", face.IsMonospace(), wantMono)
			}
		})
	}
}

// Builtins is hand-maintained, so it can drift from what go:embed picked up in
// either direction: a file added without a table entry is dead weight, and an
// entry without a file is a runtime error nobody hits until that style is used.
func TestBuiltinsMatchEmbeddedFiles(t *testing.T) {
	onDisk := map[string]bool{}
	entries, err := fs.Glob(assets.FS(), "*.ttf")
	if err != nil {
		t.Fatalf("globbing embedded assets: %v", err)
	}
	for _, name := range entries {
		onDisk[name] = true
	}

	listed := map[string]bool{}
	for _, builtin := range assets.Builtins {
		if listed[builtin.File] {
			t.Errorf("%s is listed in Builtins twice", builtin.File)
		}
		listed[builtin.File] = true
		if !onDisk[builtin.File] {
			t.Errorf("Builtins names %s, which is not embedded", builtin.File)
		}
	}
	for name := range onDisk {
		if !listed[name] {
			t.Errorf("%s is embedded but missing from Builtins", name)
		}
	}
}

func TestResolveFamilyAliases(t *testing.T) {
	registry := NewRegistry()
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{"exact", "IBM Plex Mono", "IBM Plex Mono"},
		{"lowercase", "ibm plex mono", "IBM Plex Mono"},
		{"no spaces", "IBMPlexMono", "IBM Plex Mono"},
		{"hyphenated shorthand", "plex-mono", "IBM Plex Mono"},
		{"bare shorthand", "mono", "IBM Plex Mono"},
		{"shouty shorthand", "  MONO ", "IBM Plex Mono"},
		{"underscored", "ibm_plex_serif", "IBM Plex Serif"},
		{"sans shorthand", "sans", "IBM Plex Sans"},
		{"serif shorthand", "serif", "IBM Plex Serif"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			face, _, err := registry.Resolve(tc.query, Regular)
			if err != nil {
				t.Fatalf("Resolve(%q): %v", tc.query, err)
			}
			if face.Name != tc.want {
				t.Errorf("Resolve(%q) = %q, want %q", tc.query, face.Name, tc.want)
			}
		})
	}
}

func TestResolveUnknownFamily(t *testing.T) {
	registry := NewRegistry()
	_, _, err := registry.Resolve("Comic Sans MS", Regular)
	if !errors.Is(err, ErrUnknownFamily) {
		t.Fatalf("Resolve of an absent family returned %v, want ErrUnknownFamily", err)
	}
}

// The substitution has to be visible to the caller — the CLI warns on it, and
// a face silently downgraded from bold-italic to regular is exactly the kind
// of thing that ships to a printer unnoticed.
func TestResolveStyleFallback(t *testing.T) {
	tests := []struct {
		name    string
		present []Style
		want    Style
		expect  Style
	}{
		{"exact match wins", []Style{Regular, Bold, Italic, BoldItalic}, BoldItalic, BoldItalic},
		{"bold-italic falls back to bold", []Style{Regular, Bold}, BoldItalic, Bold},
		{"bold-italic prefers bold over italic", []Style{Regular, Bold, Italic}, BoldItalic, Bold},
		{"bold-italic settles for italic", []Style{Regular, Italic}, BoldItalic, Italic},
		{"bold falls back to regular", []Style{Regular, Italic}, Bold, Regular},
		{"italic falls back to regular", []Style{Regular, Bold}, Italic, Regular},
		{"regular falls back to whatever exists", []Style{BoldItalic}, Regular, BoldItalic},
	}
	source, err := assets.Read("IBMPlexSans-Regular.ttf")
	if err != nil {
		t.Fatalf("reading a face to clone: %v", err)
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			registry := &Registry{
				families: map[string]*familyEntry{},
				aliases:  map[string]string{},
			}
			for _, style := range tc.present {
				registry.register("Partial Family", style, "test", func() ([]byte, error) { return source, nil })
			}
			_, got, err := registry.Resolve("Partial Family", tc.want)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if got != tc.expect {
				t.Errorf("Resolve(want %v) used %v, want %v", tc.want, got, tc.expect)
			}
		})
	}
}

func TestLoadDirShadowsBuiltins(t *testing.T) {
	dir := t.TempDir()
	// The serif file stands in for "the user's own Mono": what matters is that
	// the bytes resolved under the built-in name are the user's, not ours.
	source, err := assets.Read("IBMPlexSerif-Regular.ttf")
	if err != nil {
		t.Fatalf("reading source face: %v", err)
	}
	path := filepath.Join(dir, "IBM Plex Mono-Regular.ttf")
	if err := os.WriteFile(path, source, 0o644); err != nil {
		t.Fatalf("writing user font: %v", err)
	}

	registry := NewRegistry()
	builtin, _, err := registry.Resolve("mono", Regular)
	if err != nil {
		t.Fatalf("resolving the built-in first: %v", err)
	}
	if builtin.IsMonospace() != true {
		t.Fatalf("precondition failed: the built-in mono is not monospaced")
	}

	if err := registry.LoadDir(dir); err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	shadowed, style, err := registry.Resolve("mono", Regular)
	if err != nil {
		t.Fatalf("Resolve after LoadDir: %v", err)
	}
	if style != Regular {
		t.Errorf("style = %v, want Regular", style)
	}
	if shadowed.Source != path {
		t.Errorf("Source = %q, want the user file %q", shadowed.Source, path)
	}
	if shadowed.IsMonospace() {
		t.Error("still resolving the embedded monospaced face; the user font did not shadow it")
	}
	// Styles the user did not supply keep coming from the built-in family.
	bold, _, err := registry.Resolve("mono", Bold)
	if err != nil {
		t.Fatalf("Resolve(mono, bold): %v", err)
	}
	if bold.Source != "embedded:IBMPlexMono-Bold.ttf" {
		t.Errorf("bold Source = %q, want the embedded bold", bold.Source)
	}
}

func TestLoadDirErrors(t *testing.T) {
	registry := NewRegistry()
	if err := registry.LoadDir(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("LoadDir on a missing directory returned nil; --font-dir typos must be reported")
	}
	if err := registry.LoadDir(t.TempDir()); err == nil {
		t.Error("LoadDir on a directory with no fonts returned nil")
	}
}

func TestAvailableIsSortedAndComplete(t *testing.T) {
	registry := NewRegistry()
	got := registry.Available()
	if len(got) != 3 {
		t.Fatalf("Available() listed %d families, want 3", len(got))
	}
	wantNames := []string{"IBM Plex Mono", "IBM Plex Sans", "IBM Plex Serif"}
	for i, family := range got {
		if family.Name != wantNames[i] {
			t.Errorf("family %d = %q, want %q", i, family.Name, wantNames[i])
		}
		if len(family.Styles) != styleCount {
			t.Errorf("%s ships %d styles, want %d", family.Name, len(family.Styles), styleCount)
		}
		for j, style := range family.Styles {
			if int(style.Style) != j {
				t.Errorf("%s styles are out of order at %d: %v", family.Name, j, style.Style)
			}
		}
	}
	if len(got[0].Aliases) == 0 {
		t.Error("IBM Plex Mono reports no aliases; the shorthands are part of the listing")
	}
	// Repeat listings must agree exactly; map order would show up here.
	for i := 0; i < 5; i++ {
		again := registry.Available()
		for j := range got {
			if again[j].Name != got[j].Name {
				t.Fatalf("Available() is not deterministic: %q then %q at %d", got[j].Name, again[j].Name, j)
			}
		}
	}
}

// The registry is shared by the layout engine and the renderer, which may run
// on different goroutines; the first-use parse is the part that would race.
func TestRegistryConcurrentResolve(t *testing.T) {
	registry := NewRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			style := Style(i % styleCount)
			family := []string{"mono", "sans", "serif"}[i%3]
			face, _, err := registry.Resolve(family, style)
			if err != nil {
				t.Errorf("Resolve(%q, %v): %v", family, style, err)
				return
			}
			face.Width("the quick brown fox", 40, 0)
			registry.Available()
		}(i)
	}
	wg.Wait()
}

func TestParseStyle(t *testing.T) {
	tests := []struct {
		in    string
		want  Style
		valid bool
	}{
		{"regular", Regular, true},
		{"Regular", Regular, true},
		{"Book", Regular, true},
		{"bold", Bold, true},
		{"Bold", Bold, true},
		{"italic", Italic, true},
		{"Oblique", Italic, true},
		{"bold-italic", BoldItalic, true},
		{"BoldItalic", BoldItalic, true},
		{"BoldOblique", BoldItalic, true},
		{"", Regular, false},
		{"Condensed", Regular, false},
		{"Thin", Regular, false},
	}
	for _, tc := range tests {
		got, ok := ParseStyle(tc.in)
		if got != tc.want || ok != tc.valid {
			t.Errorf("ParseStyle(%q) = %v, %v; want %v, %v", tc.in, got, ok, tc.want, tc.valid)
		}
	}
}

func TestSplitFontFilename(t *testing.T) {
	tests := []struct {
		file       string
		wantFamily string
		wantStyle  Style
	}{
		{"IBMPlexMono-Bold.ttf", "IBMPlexMono", Bold},
		{"IBM Plex Mono-BoldItalic.ttf", "IBM Plex Mono", BoldItalic},
		{"Iosevka_Italic.otf", "Iosevka", Italic},
		// "Neue" is not a style, so the whole stem is the family: splitting it
		// would invent a family called "Comic".
		{"Comic Neue.ttf", "Comic Neue", Regular},
		{"Inter-Condensed.ttf", "Inter-Condensed", Regular},
		{"Whatever.ttf", "Whatever", Regular},
	}
	for _, tc := range tests {
		family, style := splitFontFilename(tc.file)
		if family != tc.wantFamily || style != tc.wantStyle {
			t.Errorf("splitFontFilename(%q) = %q, %v; want %q, %v", tc.file, family, style, tc.wantFamily, tc.wantStyle)
		}
	}
}

func TestAliasKeys(t *testing.T) {
	tests := []struct {
		family string
		want   []string
	}{
		{"IBM Plex Mono", []string{"ibmplexmono", "plexmono", "mono"}},
		{"Iosevka", []string{"iosevka"}},
		{"", nil},
	}
	for _, tc := range tests {
		got := aliasKeys(tc.family)
		if len(got) != len(tc.want) {
			t.Fatalf("aliasKeys(%q) = %v, want %v", tc.family, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("aliasKeys(%q)[%d] = %q, want %q", tc.family, i, got[i], tc.want[i])
			}
		}
	}
}
