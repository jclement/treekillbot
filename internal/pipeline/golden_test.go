// Golden-file tests over the shipped examples.
//
// The three tiers here are DESIGN.md section 4's, in its order of usefulness:
//
//  1. The `--dump-layout` rectangle tree is the PRIMARY golden. A diff says what
//     moved and by how much, in the vocabulary of the design, and it does not
//     churn when compression, PDF object order or a font subset changes.
//  2. Determinism: the same document built twice is byte-identical.
//  3. Every example compiles with no error diagnostics — one subtest per file,
//     so a broken example names itself.
//
// Regenerate the goldens after a deliberate layout change:
//
//	go test ./internal/pipeline -run Golden -update
//
// Read the diff before committing it. A golden file that is rewritten without
// being read is a test that has been switched off.
//
// Everything here is pinned to a fixed date anchor and a fixed creation time,
// so nothing depends on the clock. The anchor is a Wednesday in ISO week 37 of
// 2026, which is deliberately unremarkable: the ISO edge cases live on
// examples/stress/08b-dates.pulp and are exercised by TestGoldenDateEdgeCases.
package pipeline

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jclement/treekillbot/internal/pulp"
)

var update = flag.Bool("update", false, "rewrite the golden files instead of comparing against them")

// examplesDir is the shipped corpus, relative to this package.
const examplesDir = "../../examples"

// goldenDir holds one rectangle tree per example.
const goldenDir = "testdata/golden"

// overflowExample is the one sheet that is SUPPOSED to overflow: it exists to
// demonstrate the diagnostics, and it prints on its own face the warnings it
// expects to provoke.
const overflowExample = "stress/09-overflow.pulp"

// goldenAnchor is the date every golden is built at. Fixed, because a golden
// built at time.Now() is a test that fails once a week.
var goldenAnchor = time.Date(2026, time.September, 9, 0, 0, 0, 0, time.UTC)

// goldenCreated is the PDF's /CreationDate. Fixed for the same reason, and
// separately from the anchor so that a document which prints a date and a
// document whose metadata carries one cannot be conflated.
var goldenCreated = time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)

// goldenOptions are the build options every test in this file uses.
//
// AllowOverflow is on for the whole corpus rather than only for the overflow
// sheet, because it changes an error into a warning and nothing else: the
// geometry, and therefore the golden, is identical either way. Whether a sheet
// SHOULD overflow is asserted separately, by TestExamplesCompile.
func goldenOptions() Options {
	return Options{
		Anchor:        goldenAnchor,
		Created:       goldenCreated,
		AllowOverflow: true,
		Creator:       "treekillbot (golden test)",
	}
}

// examplePaths returns every .pulp file under examples/, in a stable order,
// as paths relative to examplesDir.
func examplePaths(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(examplesDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".pulp" {
			return nil
		}
		rel, relErr := filepath.Rel(examplesDir, path)
		if relErr != nil {
			return relErr
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", examplesDir, err)
	}
	if len(out) == 0 {
		t.Fatalf("no .pulp files under %s — the corpus these tests exist for is missing", examplesDir)
	}
	sort.Strings(out)
	return out
}

// goldenPath maps an example's relative path to its golden file, flattening the
// directory so testdata/golden/ is one readable listing rather than a tree.
func goldenPath(rel string) string {
	name := strings.TrimSuffix(rel, ".pulp")
	name = strings.ReplaceAll(name, "/", "_")
	return filepath.Join(goldenDir, name+".layout")
}

// TestGoldenLayout is the primary golden: the computed rectangle tree for every
// shipped example.
//
// It stops at StageLayout deliberately. The tree is the thing worth diffing,
// and not running the PDF writer keeps the test fast enough to run on every
// save and immune to a change in gopdf's output.
func TestGoldenLayout(t *testing.T) {
	for _, rel := range examplePaths(t) {
		t.Run(rel, func(t *testing.T) {
			result, err := BuildFile(filepath.Join(examplesDir, rel), StageLayout, goldenOptions())
			if err != nil {
				t.Fatalf("building %s: %v", rel, err)
			}
			if errs := errorDiagnostics(result.Diags); len(errs) > 0 {
				t.Fatalf("%s did not compile:\n%s", rel, strings.Join(errs, "\n"))
			}
			if result.LayoutDump == "" {
				t.Fatalf("%s produced an empty layout dump", rel)
			}
			compareGolden(t, goldenPath(rel), result.LayoutDump)
		})
	}
}

// TestExamplesCompile asserts the contract the corpus is held to: every example
// compiles with no error diagnostics, and every example except the overflow
// sheet compiles with no diagnostics AT ALL and no missing glyphs.
//
// Warnings are behaviour too (DESIGN.md section 4, tier 4). An example that
// starts warning has regressed even though it still builds, and a glyph that
// silently stops being drawn is the failure mode the Unicode sheet exists to
// warn about.
func TestExamplesCompile(t *testing.T) {
	for _, rel := range examplePaths(t) {
		t.Run(rel, func(t *testing.T) {
			result, err := BuildFile(filepath.Join(examplesDir, rel), StageRender, goldenOptions())
			if err != nil {
				t.Fatalf("building %s: %v", rel, err)
			}
			if errs := errorDiagnostics(result.Diags); len(errs) > 0 {
				t.Fatalf("%s reported errors:\n%s", rel, strings.Join(errs, "\n"))
			}
			if len(result.PDF) == 0 {
				t.Fatalf("%s produced no PDF bytes", rel)
			}

			if rel == overflowExample {
				assertOverflowSheetDiagnostics(t, result)
				return
			}
			if warnings := warningDiagnostics(result.Diags); len(warnings) > 0 {
				t.Fatalf("%s is expected to build clean but warned:\n%s", rel, strings.Join(warnings, "\n"))
			}
			if len(result.MissingGlyphs) > 0 {
				t.Fatalf("%s asks for %d character(s) no embedded font can draw: %q",
					rel, len(result.MissingGlyphs), string(result.MissingGlyphs))
			}
		})
	}
}

// assertOverflowSheetDiagnostics pins the warning codes examples/stress/
// 09-overflow.pulp prints on its own face. The sheet's whole purpose is to let
// a reader check the tool said what it should, so the list going stale would
// make the sheet a lie.
func assertOverflowSheetDiagnostics(t *testing.T, result *Result) {
	t.Helper()
	want := []string{"W010", "W020", "W020", "W021", "W030", "W031"}

	var got []string
	for _, d := range result.Diags {
		got = append(got, d.Code)
	}
	sort.Strings(got)
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("%s warns [%s]; the sheet prints [%s]. Update both together.",
			overflowExample, strings.Join(got, " "), strings.Join(want, " "))
	}
	if len(result.MissingGlyphs) == 0 {
		t.Fatalf("%s sets Greek in IBM Plex Mono to demonstrate a dropped glyph, and none was reported",
			overflowExample)
	}
}

// TestOverflowSheetIsAnErrorByDefault is the other half of D9: overflow is an
// ERROR unless --allow-overflow is given. If this ever passes silently, the
// default has been weakened and every other example's guarantee is worth less.
func TestOverflowSheetIsAnErrorByDefault(t *testing.T) {
	opts := goldenOptions()
	opts.AllowOverflow = false

	result, err := BuildFile(filepath.Join(examplesDir, overflowExample), StageLayout, opts)
	if err != nil {
		t.Fatalf("building %s: %v", overflowExample, err)
	}
	if !result.Diags.HasErrors() {
		t.Fatalf("%s is supposed to fail without --allow-overflow, and it did not", overflowExample)
	}
}

// TestGoldenDeterminism builds each example twice and compares the PDF bytes.
//
// Byte-identical output is what makes a golden file possible at all, and every
// source of nondeterminism the design eliminates — map iteration, float
// accumulation, font subset ordering, /ID — shows up here as a flake rather
// than as a mystery months later. Two builds in one process share no state:
// each one loads its own font registry and its own compiler.
func TestGoldenDeterminism(t *testing.T) {
	for _, rel := range examplePaths(t) {
		t.Run(rel, func(t *testing.T) {
			path := filepath.Join(examplesDir, rel)

			first, err := BuildFile(path, StageRender, goldenOptions())
			if err != nil {
				t.Fatalf("first build of %s: %v", rel, err)
			}
			second, err := BuildFile(path, StageRender, goldenOptions())
			if err != nil {
				t.Fatalf("second build of %s: %v", rel, err)
			}

			if first.LayoutDump != second.LayoutDump {
				t.Fatalf("%s laid out differently on the second build", rel)
			}
			if !bytes.Equal(first.PDF, second.PDF) {
				t.Fatalf("%s produced %d and %d bytes on two builds of the same input",
					rel, len(first.PDF), len(second.PDF))
			}
		})
	}
}

// TestGoldenDateEdgeCases pins the ISO week and month-clamp answers that
// examples/stress/08b-dates.pulp is printed to check.
//
// The layout of that sheet is fixed, so the only thing that can move is the
// text, and the only thing that can move the text is the date arithmetic. A
// golden per anchor is therefore a golden of the date built-ins, taken through
// the same path a real document uses rather than through a unit test of
// internal/vars.
func TestGoldenDateEdgeCases(t *testing.T) {
	const sheet = "stress/08b-dates.pulp"
	dates := []string{
		"2026-12-31", // ISO week 53 of 2026, calendar year 2026
		"2027-01-01", // ISO week 53 of 2026 while today's year is 2027
		"2021-01-01", // ISO week 53 of 2020
		"2024-02-29", // a leap day
		"2027-02-15", // month.end clamps to the 28th
		"2024-02-15", // the same clamp in a leap year
	}

	for _, date := range dates {
		t.Run(date, func(t *testing.T) {
			anchor, err := time.Parse("2006-01-02", date)
			if err != nil {
				t.Fatalf("parsing %s: %v", date, err)
			}
			opts := goldenOptions()
			opts.Anchor = anchor

			result, err := BuildFile(filepath.Join(examplesDir, sheet), StageLayout, opts)
			if err != nil {
				t.Fatalf("building %s at %s: %v", sheet, date, err)
			}
			if errs := errorDiagnostics(result.Diags); len(errs) > 0 {
				t.Fatalf("%s at %s reported errors:\n%s", sheet, date, strings.Join(errs, "\n"))
			}
			compareGolden(t, filepath.Join(goldenDir, "dates_"+date+".layout"), result.LayoutDump)
		})
	}
}

// compareGolden compares actual against the file at path, or rewrites it under
// -update.
func compareGolden(t *testing.T, path, actual string) {
	t.Helper()

	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("creating %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(actual), 0o644); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v\nRun `go test ./internal/pipeline -run Golden -update` to create it.", path, err)
	}
	if string(want) == actual {
		return
	}
	t.Fatalf("%s does not match:\n%s\nRun `go test ./internal/pipeline -run Golden -update` once you have read the diff.",
		path, firstDifference(string(want), actual))
}

// firstDifference reports the first line that differs, with its number.
//
// A whole-tree diff of a seven-hundred-line rectangle dump tells you nothing;
// the first line that moved usually tells you everything, because the rest of
// the file has moved with it.
func firstDifference(want, got string) string {
	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")

	for i := 0; i < len(wantLines) && i < len(gotLines); i++ {
		if wantLines[i] == gotLines[i] {
			continue
		}
		return "line " + itoa(i+1) + ":\n  want: " + wantLines[i] + "\n  got:  " + gotLines[i]
	}
	if len(wantLines) == len(gotLines) {
		return "the files differ but no line does, which means a trailing newline changed"
	}
	return "the trees are the same for " + itoa(minInt(len(wantLines), len(gotLines))) +
		" lines, then the golden has " + itoa(len(wantLines)) + " lines and the build produced " +
		itoa(len(gotLines))
}

// errorDiagnostics renders every error as a line, for a failure message that
// says what was wrong rather than that something was.
func errorDiagnostics(diags pulp.Diagnostics) []string {
	return renderDiagnostics(diags, pulp.SeverityError)
}

func warningDiagnostics(diags pulp.Diagnostics) []string {
	return renderDiagnostics(diags, pulp.SeverityWarning)
}

func renderDiagnostics(diags pulp.Diagnostics, severity pulp.Severity) []string {
	var out []string
	for _, d := range diags {
		if d.Severity != severity {
			continue
		}
		line := "  " + d.Code + " " + d.Message
		if d.Source != nil {
			pos := d.Position()
			line = "  " + d.Source.Name + ":" + itoa(pos.Line) + ":" + itoa(pos.Column) +
				": " + d.Code + " " + d.Message
		}
		out = append(out, line)
	}
	return out
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
