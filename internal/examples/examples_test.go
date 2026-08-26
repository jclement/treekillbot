package examples

import (
	"strings"
	"testing"
	"time"

	"github.com/jclement/treekillbot/internal/pipeline"
	"github.com/jclement/treekillbot/internal/pulp"
)

// The examples travel with the binary so that `brew install` is enough. If one
// of them does not compile, someone finds out by scaffolding it, which is the
// worst place to learn.
func TestEveryEmbeddedExampleCompiles(t *testing.T) {
	available := Available()
	if len(available) < 20 {
		t.Fatalf("only %d examples are embedded; the go:embed pattern has stopped matching", len(available))
	}

	anchor := time.Date(2026, time.September, 9, 0, 0, 0, 0, time.UTC)
	for _, example := range available {
		t.Run(example.Name, func(t *testing.T) {
			source, err := Source(example.Name)
			if err != nil {
				t.Fatalf("Source: %v", err)
			}
			result, err := pipeline.Build(
				pulp.NewSource(example.Name+Extension, source),
				pipeline.StageLayout,
				// The overflow sheet is meant to overflow; everything else must
				// not, and that is asserted below.
				pipeline.Options{Anchor: anchor, Created: anchor, AllowOverflow: true},
			)
			if err != nil {
				t.Fatalf("building: %v", err)
			}
			for _, d := range result.Diags {
				if d.Severity == pulp.SeverityError {
					t.Errorf("error: %s", d.Plain())
				}
			}
			if strings.Contains(example.Name, "overflow") {
				return
			}
			for _, d := range result.Diags {
				if d.Severity == pulp.SeverityWarning {
					t.Errorf("warning: %s", d.Plain())
				}
			}
		})
	}
}

func TestEveryExampleIsDescribed(t *testing.T) {
	for _, example := range Available() {
		if example.Description == "" {
			t.Errorf("%s has no header comment, so it lists with a blank description", example.Name)
		}
	}
}

// A name becomes a path segment, so it must not be able to climb out of the
// embedded tree.
func TestSourceRejectsTraversal(t *testing.T) {
	for _, name := range []string{"../go.mod", "../../etc/passwd", "/etc/passwd", "..", "."} {
		if _, err := Source(name); err == nil {
			t.Errorf("Source(%q) succeeded; want a rejection", name)
		}
	}
}

func TestSourceAcceptsBothSpellings(t *testing.T) {
	bare, err := Source("weekly")
	if err != nil {
		t.Fatal(err)
	}
	withExtension, err := Source("weekly.pulp")
	if err != nil {
		t.Fatalf("a name with its extension should also resolve: %v", err)
	}
	if bare != withExtension {
		t.Fatal("the two spellings returned different documents")
	}
	if _, err := Source("stress/01-units"); err != nil {
		t.Fatalf("a stress sheet should resolve by its path: %v", err)
	}
}

// Listings must not depend on filesystem or map order.
func TestAvailableIsStableAndGrouped(t *testing.T) {
	first := Available()
	for i := 0; i < 20; i++ {
		again := Available()
		if len(again) != len(first) {
			t.Fatal("Available returned a different count")
		}
		for j := range first {
			if again[j] != first[j] {
				t.Fatalf("Available is not stable at %d: %v vs %v", j, again[j], first[j])
			}
		}
	}
	// Documents lead; the stress sheets follow.
	seenStress := false
	for _, example := range first {
		if example.Group == GroupStress {
			seenStress = true
			continue
		}
		if seenStress {
			t.Fatalf("%s is a document but comes after a stress sheet", example.Name)
		}
	}
}

func TestUnknownNameSuggests(t *testing.T) {
	_, err := Source("weekley")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "weekly") {
		t.Fatalf("error should suggest the near match: %v", err)
	}
}
