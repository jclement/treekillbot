// Themes: the ink a document is printed in, written in Pulp itself.
//
// A theme is a `.pulptheme` file of `defaults` blocks, embedded with go:embed
// (DESIGN.md D13). One language for the whole toolchain buys three things:
// `check` and `fmt` work on a theme, `treekillbot themes --show mono` prints a
// file you can copy into ~/.config/treekillbot/themes/ and edit, and there is
// no second colour or length parser to drift out of sync with the real one.
// Nothing in this package parses a property value; the loader runs the theme
// through the ordinary pulp → schema → compile path and reads the answer off
// the far end.
//
// User themes shadow built-ins by name, `default` included. That is deliberate:
// the shipped themes are opinions, and someone who prefers a different one
// should be able to state it once rather than pass a flag forever.
package themes

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jclement/treekillbot/internal/compile"
	"github.com/jclement/treekillbot/internal/pulp"
	"github.com/jclement/treekillbot/internal/schema"
)

// Extension is the file suffix a theme carries. It is distinct from `.pulp` so
// that a directory of documents and a directory of themes can be the same
// directory without `treekillbot build *.pulp` picking up the themes.
const Extension = ".pulptheme"

// UserDir is where a person's own themes live, relative to the config home.
// The path is spelled out rather than taken from os.UserConfigDir because that
// function answers "~/Library/Application Support" on macOS, and this tool's
// config lives in ~/.config on every platform it runs on.
const UserDir = "treekillbot/themes"

//go:embed builtin/*.pulptheme
var builtinFS embed.FS

const builtinDir = "builtin"

// Theme is one theme the tool can apply.
type Theme struct {
	// Name is what `--theme` takes: the file's base name without its extension.
	Name string
	// Description is the theme file's first comment line, which is where each
	// one states in a sentence what it is for.
	Description string
	// Origin is BuiltinOrigin, or the path of the file a user theme was read
	// from.
	// It is what makes a shadowed built-in visible in `treekillbot themes`
	// rather than a mystery.
	Origin string
}

// BuiltinOrigin is the Origin of a theme that ships with the binary. It is
// exported so a caller can tell a shipped theme from a user's own without
// matching on a string it had to guess.
const BuiltinOrigin = "built-in"

// themeFile is a theme's source, located but not yet compiled.
type themeFile struct {
	name   string
	origin string
	text   string
}

// Load resolves a theme by name and returns the property set to hand to
// pipeline.Options.Theme.
//
// An empty name returns (nil, nil), so a caller can pass an unset --theme flag
// straight through without guarding it. An unknown name is an error carrying a
// did-you-mean.
//
// Themes are searched for in the working directory first, then in
// ~/.config/treekillbot/themes, then among the built-ins. LoadFrom takes the
// document's own directory instead, which is what a caller that knows where the
// source file lives should use.
func Load(name string) (*compile.ThemeLayer, error) { return LoadFrom(".", name) }

// LoadFrom resolves a theme, searching documentDir before the user's theme
// directory and the built-ins.
func LoadFrom(documentDir, name string) (*compile.ThemeLayer, error) {
	if name == "" {
		return nil, nil
	}
	file, err := find(documentDir, name)
	if err != nil {
		return nil, err
	}
	return file.layer()
}

// Source returns a theme's Pulp source, for `themes --show`. It is the whole
// file, comments included, because the point of showing it is that someone can
// paste it into their own themes directory and edit it.
func Source(name string) (string, error) {
	file, err := find(".", name)
	if err != nil {
		return "", err
	}
	return file.text, nil
}

// Available returns every theme that can be named from the working directory,
// sorted by name.
//
// A user theme with the same name as a built-in replaces it in the list rather
// than appearing twice, and the directories are walked in the same order Load
// searches them, so the listing and the loader cannot disagree — a listing that
// promised a theme the loader would not pick is worse than no listing. The
// consequence is that `treekillbot themes` depends on where you are standing,
// which is honest: so does `--theme`.
func Available() []Theme {
	byName := map[string]Theme{}

	entries, err := builtinFS.ReadDir(builtinDir)
	if err == nil {
		for _, entry := range entries {
			name := strings.TrimSuffix(entry.Name(), Extension)
			text, err := builtinFS.ReadFile(builtinDir + "/" + entry.Name())
			if err != nil {
				continue
			}
			byName[name] = Theme{Name: name, Description: describe(string(text)), Origin: BuiltinOrigin}
		}
	}

	// Lowest priority first, so a nearer directory overwrites what a further
	// one contributed. searchDirs is nearest-first, hence the reverse walk.
	dirs := searchDirs(".")
	for i := len(dirs) - 1; i >= 0; i-- {
		collectDir(dirs[i], byName)
	}

	out := make([]Theme, 0, len(byName))
	for _, theme := range byName {
		out = append(out, theme)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// collectDir adds every theme file in a directory to the listing, overwriting
// entries of the same name. A directory that does not exist is not an error:
// most people have no theme directory, and that is the normal case rather than
// a misconfiguration worth reporting.
func collectDir(dir string, byName map[string]Theme) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), Extension) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		text, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), Extension)
		byName[name] = Theme{Name: name, Description: describe(string(text)), Origin: path}
	}
}

// Names returns every available theme name, sorted. It is what the did-you-mean
// for an unknown theme ranks against.
func Names() []string {
	available := Available()
	out := make([]string, 0, len(available))
	for _, theme := range available {
		out = append(out, theme.Name)
	}
	return out
}

// describe extracts a theme or template's one-line description: the text of the
// file's first comment line.
//
// The convention is that the first line of every shipped file is a sentence
// saying what it is for, with the reasoning in the paragraphs beneath. That
// keeps the listing and the file itself from being two places to update.
func describe(text string) string {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "#") {
			return ""
		}
		return strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
	}
	return ""
}

// ---- Discovery ----

// find locates a theme's source, in the order documented on Load.
func find(documentDir, name string) (*themeFile, error) {
	if err := validName(name); err != nil {
		return nil, err
	}

	for _, dir := range searchDirs(documentDir) {
		path := filepath.Join(dir, name+Extension)
		text, err := os.ReadFile(path)
		if err == nil {
			return &themeFile{name: name, origin: path, text: string(text)}, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("reading theme %s: %w", path, err)
		}
	}

	text, err := builtinFS.ReadFile(builtinDir + "/" + name + Extension)
	if err == nil {
		return &themeFile{name: name, origin: BuiltinOrigin, text: string(text)}, nil
	}
	return nil, unknownTheme(name)
}

// searchDirs returns the on-disk directories a theme is looked for in, nearest
// first. The document's own directory comes first so that a theme shipped
// alongside a document travels with it.
func searchDirs(documentDir string) []string {
	var dirs []string
	if documentDir != "" {
		dirs = append(dirs, documentDir)
	}
	if user := userThemeDir(); user != "" {
		dirs = append(dirs, user)
	}
	return dirs
}

// userThemeDir returns ~/.config/treekillbot/themes, honouring XDG_CONFIG_HOME,
// or "" when there is no home directory to speak of.
func userThemeDir() string {
	if base := os.Getenv("XDG_CONFIG_HOME"); base != "" {
		return filepath.Join(base, UserDir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", UserDir)
}

// validName rejects anything that is not a bare theme name.
//
// A theme name becomes a path segment, so `--theme ../../etc/passwd` has to be
// refused here rather than trusted to filepath.Join. Restricting it to the
// shape the built-ins already use costs nothing: a theme in another directory
// is one `include` away in the document.
func validName(name string) error {
	if name == "" {
		return errors.New("a theme name cannot be empty")
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return fmt.Errorf("%q is not a theme name; theme names are lowercase words such as `mono` or `dot-grid`", name)
		}
	}
	return nil
}

// unknownTheme builds the error for a name nothing matched, with a suggestion
// drawn from the same engine every other unknown name in the tool uses.
func unknownTheme(name string) error {
	names := Names()
	if suggestions := pulp.Suggest(name, names); len(suggestions) > 0 {
		return fmt.Errorf("unknown theme %q. %s", name, pulp.FormatSuggestions("theme", suggestions))
	}
	return fmt.Errorf("unknown theme %q; available themes are %s", name, strings.Join(names, ", "))
}

// ---- Compilation ----

// probe is appended to a theme's source so that the real cascade can be asked
// what the theme's `defaults` blocks resolve to.
//
// This is the whole trick that keeps a theme from needing its own parser. A
// theme is semantically one global `defaults` block, and the only machinery
// that knows how to turn that into a Props — including the arity of `padding`,
// the units on a length and the spelling of a colour function — is
// internal/compile. So the file is compiled as a one-panel document and the
// panel's resolved properties are read back. `panel` is the probe because it is
// the element every property a theme may set applies to.
const probe = "\n\npanel\n"

// layer compiles a theme file into the cascade contribution that sits beneath
// a document's own defaults.
//
// It goes through compile.CompileTheme rather than reading properties itself,
// so a theme and a document cannot end up disagreeing about what `defaults
// panel` means — there is one implementation of the cascade, and this uses it.
func (f *themeFile) layer() (*compile.ThemeLayer, error) {
	src := pulp.NewSource(f.origin, f.text)
	doc, diags := pulp.Parse(src)
	diags = append(diags, schema.Validate(doc)...)
	if err := firstError(diags); err != nil {
		return nil, err
	}
	if err := validateTheme(src, doc); err != nil {
		return nil, err
	}

	layer, compileDiags := compile.CompileTheme(doc)
	if err := firstError(compileDiags); err != nil {
		return nil, err
	}

	// A theme that contributes nothing is almost always a truncated file rather
	// than an intention: `themes --show mono > mono.pulptheme` in the document's
	// own directory truncates the target before the command runs, and discovery
	// then prefers the empty file. Rendering unthemed from then on, silently, is
	// the worst outcome available.
	if layer.IsEmpty() {
		return nil, fmt.Errorf("theme %s (%s) sets no properties; if you meant to write it with "+
			"`themes --show`, write to a different name and move it into place", f.name, f.origin)
	}
	return layer, nil
}

// firstError returns the first error diagnostic as an error, or nil.
func firstError(diags pulp.Diagnostics) error {
	for _, d := range diags {
		if d.Severity != pulp.SeverityError {
			continue
		}
		if d.Help == "" {
			return errors.New(d.Plain())
		}
		return fmt.Errorf("%s\n  help: %s", d.Plain(), d.Help)
	}
	return nil
}
