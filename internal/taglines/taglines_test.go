package taglines

import (
	"math/rand/v2"
	"testing"
	"unicode/utf8"
)

// The count is asserted so that nobody quietly trims the list: the set is
// meant to be large enough that you rarely see the same one twice, and a
// silent drop to forty would never be noticed from the output alone.
func TestCountIsExact(t *testing.T) {
	if len(taglines) != Count {
		t.Errorf("have %d taglines, want exactly %d", len(taglines), Count)
	}
	if got := len(All()); got != Count {
		t.Errorf("All() returned %d taglines, want %d", got, Count)
	}
}

func TestTaglinesAreUnique(t *testing.T) {
	seen := make(map[string]int, len(taglines))
	for i, tagline := range taglines {
		if first, dup := seen[tagline]; dup {
			t.Errorf("tagline %d duplicates %d: %q", i, first, tagline)
			continue
		}
		seen[tagline] = i
	}
}

// Length is measured in runes, not bytes, because the cap exists to stop the
// status line wrapping and the status line is laid out in columns.
func TestTaglinesFitTheStatusLine(t *testing.T) {
	for i, tagline := range taglines {
		if tagline == "" {
			t.Errorf("tagline %d is empty", i)
			continue
		}
		if n := utf8.RuneCountInString(tagline); n >= maxLength {
			t.Errorf("tagline %d is %d chars, want under %d: %q", i, n, maxLength, tagline)
		}
	}
}

// All() hands out a copy; a caller mutating it must not reach the table.
func TestAllReturnsACopy(t *testing.T) {
	original := Pick(0)
	All()[0] = "clobbered"
	if got := Pick(0); got != original {
		t.Errorf("mutating All() changed the table: Pick(0) = %q, want %q", got, original)
	}
}

// Pick has to accept any int, including the negative ones a hash produces.
func TestPickWraps(t *testing.T) {
	tests := []struct {
		name string
		n    int
		want string
	}{
		{"first", 0, taglines[0]},
		{"last", Count - 1, taglines[Count-1]},
		{"wraps past the end", Count, taglines[0]},
		{"wraps twice", 2*Count + 3, taglines[3]},
		{"negative wraps backwards", -1, taglines[Count-1]},
		{"large negative", -(2*Count + 1), taglines[Count-1]},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Pick(tc.n); got != tc.want {
				t.Errorf("Pick(%d) = %q, want %q", tc.n, got, tc.want)
			}
		})
	}
}

// The whole reason Random takes a source rather than using the package-level
// generator: two runs from the same seed must agree.
func TestRandomIsReproducibleFromItsSource(t *testing.T) {
	const seed = 20260826

	first := make([]string, 32)
	source := rand.New(rand.NewPCG(seed, 0))
	for i := range first {
		first[i] = Random(source)
	}

	source = rand.New(rand.NewPCG(seed, 0))
	for i := range first {
		if got := Random(source); got != first[i] {
			t.Fatalf("draw %d differs between runs of the same seed: %q vs %q", i, got, first[i])
		}
	}
}
