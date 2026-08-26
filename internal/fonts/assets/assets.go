// Package assets holds the TrueType files treekillbot ships inside its binary.
//
// It exists as a package of its own for one mechanical reason: go:embed cannot
// reference a path containing "..", so the directive has to live in the same
// directory as the .ttf files. Keeping it separate from the parent fonts
// package also keeps the dependency one-way — assets knows nothing about
// faces, registries or geometry, so it can be described entirely in strings.
//
// The fonts are IBM Plex static instances under the SIL Open Font License 1.1;
// OFL.txt is the licence and SOURCES.md records where every byte came from and
// its SHA-256. Both are shipped in the repository, not embedded, because the
// obligation is to distribute the licence with the font files, and the source
// tree is what we distribute.
package assets

import (
	"embed"
	"fmt"
	"io/fs"
)

//go:embed *.ttf
var files embed.FS

// Builtin describes one embedded face.
//
// Style is spelled the way fonts.Style.String() spells it — "regular", "bold",
// "italic", "bold-italic" — rather than being a fonts.Style value, because
// importing the parent package here would be an import cycle. The parent
// parses these back into its own enum.
type Builtin struct {
	Family string // display name as a document would write it, e.g. "IBM Plex Mono"
	Style  string
	File   string
}

// Builtins lists every embedded face, sorted by family then style, so that any
// caller iterating it produces the same order on every run. Nothing here is
// ever discovered by walking the embedded FS: a table that has to be edited
// alongside the files is the thing that makes an accidentally-missing font a
// compile-time problem rather than a silent one.
var Builtins = []Builtin{
	{Family: "IBM Plex Mono", Style: "bold", File: "IBMPlexMono-Bold.ttf"},
	{Family: "IBM Plex Mono", Style: "bold-italic", File: "IBMPlexMono-BoldItalic.ttf"},
	{Family: "IBM Plex Mono", Style: "italic", File: "IBMPlexMono-Italic.ttf"},
	{Family: "IBM Plex Mono", Style: "regular", File: "IBMPlexMono-Regular.ttf"},
	{Family: "IBM Plex Sans", Style: "bold", File: "IBMPlexSans-Bold.ttf"},
	{Family: "IBM Plex Sans", Style: "bold-italic", File: "IBMPlexSans-BoldItalic.ttf"},
	{Family: "IBM Plex Sans", Style: "italic", File: "IBMPlexSans-Italic.ttf"},
	{Family: "IBM Plex Sans", Style: "regular", File: "IBMPlexSans-Regular.ttf"},
	{Family: "IBM Plex Serif", Style: "bold", File: "IBMPlexSerif-Bold.ttf"},
	{Family: "IBM Plex Serif", Style: "bold-italic", File: "IBMPlexSerif-BoldItalic.ttf"},
	{Family: "IBM Plex Serif", Style: "italic", File: "IBMPlexSerif-Italic.ttf"},
	{Family: "IBM Plex Serif", Style: "regular", File: "IBMPlexSerif-Regular.ttf"},
}

// Read returns the bytes of one embedded font file.
//
// embed.FS copies on every call — the data is stored as a string and ReadFile
// converts it — so a 200KB face costs 200KB each time. That is why the
// registry parses a face once and holds the result, rather than reading the
// bytes again for every text run.
func Read(name string) ([]byte, error) {
	data, err := files.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("reading embedded font %q: %w", name, err)
	}
	return data, nil
}

// FS exposes the embedded files for tests that want to walk them and check
// that every file on disk is also listed in Builtins.
func FS() fs.FS { return files }
