// "Did you mean…?" suggestions for misspelled names.
//
// Every place the language rejects a name — a property, an element, an enum
// value, a theme, a template, a subcommand — routes through here, so the
// behaviour is identical everywhere and there is one threshold to tune rather
// than six.
//
// Two decisions worth knowing about:
//
// Normalisation runs before distance. If a name matches a candidate exactly
// once case, underscores and camelCase are folded away, that is not a typo at
// all — it is someone writing `lineStyle` or `line_style` in a language that
// spells things `line-style`. That deserves a different, more certain message
// than a guess.
//
// The distance metric is Damerau-Levenshtein rather than plain Levenshtein,
// because adjacent transposition is the single most common typing error and
// plain Levenshtein charges it two edits. `stlye` and `witdh` should be one
// edit away from `style` and `width`, and under plain Levenshtein they are two,
// which pushes them past the threshold on short names exactly when the
// suggestion would have been most useful.
package pulp

import (
	"sort"
	"strings"
	"unicode"
)

// Suggestion is one candidate replacement for an unrecognised name.
type Suggestion struct {
	Name     string
	Distance int
	// Exact is true when the name matches after normalisation — the author used
	// the wrong convention rather than misspelling the word.
	Exact bool
}

// maxSuggestions caps how many alternates are offered. One confident primary
// plus two alternates is enough to be useful; a longer list reads as the tool
// having no idea, which is worse than saying so.
const maxSuggestions = 3

// Suggest ranks candidates against an unrecognised name.
//
// It returns nil when nothing is close enough, and callers should say "no
// similar property" rather than offering their least-bad guess: a wrong
// suggestion costs more than no suggestion, because the reader tries it.
func Suggest(name string, candidates []string) []Suggestion {
	if name == "" || len(candidates) == 0 {
		return nil
	}
	target := normalize(name)
	threshold := suggestThreshold(name)

	var out []Suggestion
	for _, c := range candidates {
		if c == name {
			continue
		}
		norm := normalize(c)
		if norm == target {
			out = append(out, Suggestion{Name: c, Distance: 0, Exact: true})
			continue
		}
		d := damerauLevenshtein(target, norm)
		if d <= threshold {
			out = append(out, Suggestion{Name: c, Distance: d})
		}
	}
	if len(out) == 0 {
		return nil
	}

	// Rank: convention matches first, then edit distance, then a shared prefix
	// (people get the start of a word right far more often than the end), then
	// alphabetically so the output is deterministic.
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Exact != b.Exact {
			return a.Exact
		}
		if a.Distance != b.Distance {
			return a.Distance < b.Distance
		}
		pa, pb := commonPrefix(target, normalize(a.Name)), commonPrefix(target, normalize(b.Name))
		if pa != pb {
			return pa > pb
		}
		return a.Name < b.Name
	})

	// A convention match is certain; never dilute it with guesses.
	if out[0].Exact {
		return out[:1]
	}
	if len(out) > maxSuggestions {
		out = out[:maxSuggestions]
	}
	return out
}

// suggestThreshold scales the allowed edit distance with the length of the
// name. One edit on a three-letter word is a different word; three edits on a
// twenty-letter word is still recognisably the same one.
func suggestThreshold(name string) int {
	t := 1 + len(name)/5
	if t > 3 {
		t = 3
	}
	return t
}

// normalize folds away the differences that are convention rather than
// spelling: case, separators, and camelCase word boundaries.
func normalize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '-' || r == '_' || r == ' ' {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

func commonPrefix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

// damerauLevenshtein returns the optimal-string-alignment distance between two
// strings, counting an adjacent transposition as a single edit.
//
// This is the restricted variant: it does not allow a substring to be edited
// after being transposed, which makes it O(nm) with two rows of state instead
// of the full algorithm's alphabet-sized table. For catching typos in
// identifiers the difference never shows up.
func damerauLevenshtein(a, b string) int {
	if a == b {
		return 0
	}
	if a == "" {
		return len(b)
	}
	if b == "" {
		return len(a)
	}

	prev2 := make([]int, len(b)+1)
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(
				curr[j-1]+1,    // insertion
				prev[j]+1,      // deletion
				prev[j-1]+cost, // substitution
			)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				if t := prev2[j-2] + 1; t < curr[j] {
					curr[j] = t
				}
			}
		}
		prev2, prev, curr = prev, curr, prev2
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

// FormatSuggestions renders suggestions as a help line. It returns the empty
// string when there is nothing worth saying, so callers can assign it to Help
// unconditionally.
func FormatSuggestions(kind string, ss []Suggestion) string {
	if len(ss) == 0 {
		return ""
	}
	if ss[0].Exact {
		return "Pulp spells " + kind + "s in kebab-case: write `" + ss[0].Name + "`."
	}
	help := "Did you mean `" + ss[0].Name + "`?"
	if len(ss) > 1 {
		alts := make([]string, 0, len(ss)-1)
		for _, s := range ss[1:] {
			alts = append(alts, "`"+s.Name+"`")
		}
		help += " Or " + strings.Join(alts, ", ") + "."
	}
	return help
}
