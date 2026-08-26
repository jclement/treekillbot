package docs

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Every diagnostic ends by telling the reader to run
// `treekillbot docs errors <CODE>`. If a code has no entry here, that
// instruction is a lie, and the reader discovers it at the exact moment they
// are already stuck. This test walks the source for emitted codes and requires
// each one to be documented.
func TestEveryEmittedCodeIsDocumented(t *testing.T) {
	root := repoRoot(t)
	emitted := map[string][]string{}

	// Matches the call sites that create a diagnostic with a literal code:
	//   diags.Errorf(src, span, "E021", ...)   Warnf(...)
	pattern := regexp.MustCompile(`(?:Errorf|Warnf)\([^)]*?"([EW]\d{3})"`)

	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(root, path)
		for _, match := range pattern.FindAllStringSubmatch(string(data), -1) {
			emitted[match[1]] = append(emitted[match[1]], relative)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the source: %v", err)
	}
	if len(emitted) == 0 {
		t.Fatal("found no diagnostic codes in the source; the pattern has stopped matching")
	}

	var missing []string
	for code, files := range emitted {
		if _, ok := LookupError(code); !ok {
			missing = append(missing, code+" (emitted from "+strings.Join(unique(files), ", ")+")")
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("these diagnostic codes are emitted but undocumented, so `treekillbot docs errors` "+
			"would fail the reader:\n  %s", strings.Join(missing, "\n  "))
	}
}

// The reverse: documentation for a code nothing emits is stale, and stale docs
// mislead worse than missing ones.
func TestNoDocumentationForCodesNothingEmits(t *testing.T) {
	root := repoRoot(t)
	pattern := regexp.MustCompile(`"([EW]\d{3})"`)
	seen := map[string]bool{}

	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Test files are excluded deliberately. A code named only in a test's
		// expectation list is not emitted by anything, and counting it here
		// would let a retired code keep its documentation indefinitely.
		if entry.IsDir() || !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_test.go") || strings.Contains(path, "internal/docs/") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range pattern.FindAllStringSubmatch(string(data), -1) {
			seen[match[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the source: %v", err)
	}

	var stale []string
	for _, doc := range AllErrors() {
		if !seen[doc.Code] {
			stale = append(stale, doc.Code+" — "+doc.Title)
		}
	}
	if len(stale) > 0 {
		t.Fatalf("these codes are documented but never emitted:\n  %s", strings.Join(stale, "\n  "))
	}
}

func TestEveryDocIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, doc := range AllErrors() {
		if seen[doc.Code] {
			t.Errorf("%s is documented twice", doc.Code)
		}
		seen[doc.Code] = true
		if doc.Title == "" {
			t.Errorf("%s has no title", doc.Code)
		}
		if doc.Explanation == "" {
			t.Errorf("%s has no explanation; the message already said what happened, so this is the part worth reading", doc.Code)
		}
		if doc.Fix == "" {
			t.Errorf("%s does not say what to do about it", doc.Code)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("could not find the repository root")
	return ""
}

func unique(items []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	sort.Strings(out)
	return out
}
