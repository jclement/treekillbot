// Family and style resolution: the lookup table between what a .pulp document
// asks for ("mono", bold) and the parsed Face that answers metric questions.
//
// Two behaviours here are deliberate and worth knowing before changing
// anything:
//
//   - Faces are parsed on first use, never at startup. A weekly planner uses
//     one or two of the twelve embedded files, and parsing the other ten costs
//     time and memory for nothing.
//   - Every listing is returned in sorted order. Nothing in this package ever
//     ranges over a map to produce output, because `treekillbot fonts` is
//     golden-tested and map order would make it flap. See DESIGN.md section 4.
package fonts

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/jclement/treekillbot/internal/fonts/assets"
)

// ErrUnknownFamily is returned by Resolve when no family, alias or user font
// matches the requested name. Callers turn it into a source diagnostic, so it
// is a sentinel rather than a formatted string.
var ErrUnknownFamily = errors.New("unknown font family")

// styleCount is the number of concrete styles we ship per family. It tracks
// the Style enum in face.go; there are no intermediate weights because we
// embed static instances (DESIGN.md D8).
const styleCount = 4

// Registry resolves a family name and style to a Face.
//
// It is safe for concurrent use. The mutex covers the family tables and the
// lazily-filled face slots; parsing happens while holding it, which serialises
// the first use of each file but costs nothing afterwards and keeps the "parse
// exactly once, even on error" guarantee trivial to see.
type Registry struct {
	mu sync.Mutex
	// families is keyed by the normalised primary name, e.g. "ibmplexmono".
	families map[string]*familyEntry
	// aliases maps a normalised shorthand ("mono") to a primary key. Kept
	// separate so a family's own name always beats another family's alias.
	aliases map[string]string
}

type familyEntry struct {
	name  string // display name, e.g. "IBM Plex Mono"
	slots [styleCount]*faceSlot
}

// faceSlot is one (family, style) pair: where to get the bytes, and the parse
// result once someone has asked for it. A failed parse is cached too — a
// truncated user font should report the same error every time rather than
// re-reading the file on every text run.
type faceSlot struct {
	style  Style
	source string // "embedded:IBMPlexMono-Bold.ttf" or an absolute path
	read   func() ([]byte, error)
	face   *Face
	err    error
}

// NewRegistry returns a registry holding the embedded IBM Plex faces. No font
// is parsed yet; the first Resolve for a given face does that.
func NewRegistry() *Registry {
	r := &Registry{
		families: make(map[string]*familyEntry, 3),
		aliases:  make(map[string]string, 8),
	}
	for _, b := range assets.Builtins {
		style, ok := ParseStyle(b.Style)
		if !ok {
			// Builtins is a hand-maintained table compiled into the binary; a
			// bad entry is a programming error, not a runtime condition.
			panic(fmt.Sprintf("fonts: embedded asset %q has unparseable style %q", b.File, b.Style))
		}
		file := b.File
		r.register(b.Family, style, "embedded:"+file, func() ([]byte, error) {
			return assets.Read(file)
		})
	}
	return r
}

// LoadDir registers every .ttf and .otf file in dir, shadowing the built-ins.
//
// Family and style come from the filename, which is the convention every font
// distributor already follows: "Family-Style.ttf", so "Iosevka-BoldItalic.ttf"
// is Iosevka bold-italic and "Comic Neue.ttf" is Comic Neue regular. The
// alternative — reading the font's own name table — would mean parsing every
// file in the directory just to build the index, which is the cost this
// package is otherwise arranged to avoid.
//
// A missing directory is an error: --font-dir naming somewhere that does not
// exist is a mistake worth reporting rather than silently ignoring.
func (r *Registry) LoadDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("reading font directory %q: %w", dir, err)
	}
	// os.ReadDir already sorts by filename, so registration order — and
	// therefore which file wins an alias — depends only on the directory
	// contents.
	loaded := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".ttf" && ext != ".otf" {
			continue
		}
		family, style := splitFontFilename(entry.Name())
		path := filepath.Join(dir, entry.Name())
		r.register(family, style, path, func() ([]byte, error) {
			return os.ReadFile(path)
		})
		loaded++
	}
	if loaded == 0 {
		return fmt.Errorf("font directory %q contains no .ttf or .otf files", dir)
	}
	return nil
}

// register installs one face, replacing any face already occupying that
// (family, style) slot. Replacement is what makes a user font shadow a
// built-in of the same name.
func (r *Registry) register(family string, style Style, source string, read func() ([]byte, error)) {
	key := normalizeFamily(family)
	if key == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	entry := r.families[key]
	if entry == nil {
		entry = &familyEntry{name: family}
		r.families[key] = entry
		// A primary name that was previously somebody's alias takes the key
		// back: your own name always beats another family's shorthand.
		delete(r.aliases, key)
	}
	entry.slots[style] = &faceSlot{style: style, source: source, read: read}

	for _, alias := range aliasKeys(family) {
		if alias == key {
			continue
		}
		if _, taken := r.families[alias]; taken {
			continue
		}
		if _, taken := r.aliases[alias]; taken {
			continue
		}
		r.aliases[alias] = key
	}
}

// Resolve returns the face for a family and style, together with the style
// actually used.
//
// The returned style differs from the requested one when the family does not
// ship it — asking for bold-italic in a family that has only bold gets bold.
// The substitution is returned rather than logged so that the CLI can decide
// whether it is a warning, and so that this package stays silent.
//
// Family matching ignores case, spaces and punctuation, and accepts the
// obvious shorthands: "mono", "IBM Plex Mono" and "plex-mono" are one family.
func (r *Registry) Resolve(family string, style Style) (*Face, Style, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry := r.lookupLocked(family)
	if entry == nil {
		return nil, style, fmt.Errorf("%w: %q", ErrUnknownFamily, family)
	}
	for _, candidate := range styleFallback(style) {
		slot := entry.slots[candidate]
		if slot == nil {
			continue
		}
		face, err := r.faceLocked(entry.name, slot)
		if err != nil {
			return nil, style, err
		}
		return face, candidate, nil
	}
	return nil, style, fmt.Errorf("font family %q has no usable face", entry.name)
}

// HasFamily reports whether a name resolves to a known family, for the "did
// you mean" path in the schema layer.
func (r *Registry) HasFamily(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lookupLocked(name) != nil
}

func (r *Registry) lookupLocked(name string) *familyEntry {
	key := normalizeFamily(name)
	if entry, ok := r.families[key]; ok {
		return entry
	}
	if primary, ok := r.aliases[key]; ok {
		return r.families[primary]
	}
	return nil
}

// faceLocked parses a slot's bytes on first use and caches the result, success
// or failure. The caller must hold the mutex.
func (r *Registry) faceLocked(family string, slot *faceSlot) (*Face, error) {
	if slot.face != nil || slot.err != nil {
		return slot.face, slot.err
	}
	data, err := slot.read()
	if err != nil {
		slot.err = err
		return nil, err
	}
	slot.face, slot.err = Load(family, slot.style, data, slot.source)
	return slot.face, slot.err
}

// ---- Listing ----

// StyleInfo is one available style within a family.
type StyleInfo struct {
	Style  Style
	Source string // "embedded:<file>" or the path the face was loaded from
}

// FamilyInfo describes one family for `treekillbot fonts`.
type FamilyInfo struct {
	Name    string
	Aliases []string // sorted shorthands that also resolve to this family
	Styles  []StyleInfo
}

// Available lists every registered family, sorted by name, with each family's
// styles sorted by style and aliases sorted alphabetically. Nothing about the
// order depends on map iteration; see the package comment.
func (r *Registry) Available() []FamilyInfo {
	r.mu.Lock()
	defer r.mu.Unlock()

	aliasesByKey := make(map[string][]string, len(r.aliases))
	for alias, primary := range r.aliases {
		aliasesByKey[primary] = append(aliasesByKey[primary], alias)
	}

	out := make([]FamilyInfo, 0, len(r.families))
	for key, entry := range r.families {
		info := FamilyInfo{Name: entry.name, Aliases: aliasesByKey[key]}
		sort.Strings(info.Aliases)
		for style := Style(0); style < styleCount; style++ {
			if slot := entry.slots[style]; slot != nil {
				info.Styles = append(info.Styles, StyleInfo{Style: style, Source: slot.source})
			}
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ---- Name and style parsing ----

// ParseStyle maps a style spelling onto one of the four concrete styles.
// It accepts the DSL spellings ("bold-italic"), the filename spellings
// ("BoldItalic") and the common synonyms ("oblique", "book"), all
// case-insensitively.
func ParseStyle(s string) (Style, bool) {
	switch normalizeFamily(s) {
	case "regular", "book", "roman", "normal", "":
		return Regular, s != ""
	case "bold", "semibold", "demibold":
		return Bold, true
	case "italic", "oblique":
		return Italic, true
	case "bolditalic", "italicbold", "boldoblique", "semibolditalic":
		return BoldItalic, true
	}
	return Regular, false
}

// styleFallback returns the styles to try, best first, when a family does not
// ship the requested one. The ladder always ends at Regular, and prefers
// keeping weight over keeping slant — a bold heading that loses its italic
// still reads as a heading, whereas one that loses its weight does not.
func styleFallback(want Style) [styleCount]Style {
	switch want {
	case Bold:
		return [styleCount]Style{Bold, Regular, BoldItalic, Italic}
	case Italic:
		return [styleCount]Style{Italic, Regular, BoldItalic, Bold}
	case BoldItalic:
		return [styleCount]Style{BoldItalic, Bold, Italic, Regular}
	default:
		return [styleCount]Style{Regular, Bold, Italic, BoldItalic}
	}
}

// normalizeFamily reduces a name to its comparison key: lowercase, with every
// character that is not a letter or a digit removed. "IBM Plex Mono",
// "ibm_plex_mono" and "plex mono " all differ only in the noise a user is
// likely to get wrong, so none of it participates in matching.
func normalizeFamily(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// aliasKeys returns the shorthands a family answers to, longest first: every
// suffix of its words. "IBM Plex Mono" yields "ibmplexmono", "plexmono" and
// "mono", which is exactly the set a user is likely to type. Single-word
// families yield only their own key.
func aliasKeys(family string) []string {
	words := strings.FieldsFunc(family, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'))
	})
	keys := make([]string, 0, len(words))
	for i := range words {
		keys = append(keys, normalizeFamily(strings.Join(words[i:], "")))
	}
	return keys
}

// splitFontFilename derives a family and style from a font file's name,
// following the "Family-Style.ttf" convention. A stem whose trailing segment
// is not a style name is taken whole, so "Comic Neue.ttf" is the Comic Neue
// family in regular rather than a family called "Comic" in a style called
// "Neue".
func splitFontFilename(name string) (family string, style Style) {
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	cut := strings.LastIndexAny(stem, "-_")
	if cut <= 0 {
		return stem, Regular
	}
	parsed, ok := ParseStyle(stem[cut+1:])
	if !ok {
		return stem, Regular
	}
	return stem[:cut], parsed
}
