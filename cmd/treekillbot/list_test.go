package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jclement/treekillbot/internal/themes"
)

// TestTemplatesListGoesToStdout pins the stream contract for a listing: it is
// the data that was asked for, so it belongs on stdout where it can be piped,
// and only the hint goes to stderr.
func TestTemplatesListGoesToStdout(t *testing.T) {
	console, out, errOut := testConsole()
	cmd := newTemplatesCommand(console)
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("templates: %v", err)
	}
	if !strings.Contains(out.String(), "weekly") {
		t.Errorf("the listing did not reach stdout; got %q", out.String())
	}
	if strings.Contains(errOut.String(), "weekly") {
		t.Error("the listing leaked onto stderr")
	}
}

func TestTemplatesJSON(t *testing.T) {
	console, out, _ := testConsole()
	cmd := newTemplatesCommand(console)
	cmd.SetArgs([]string{"--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("templates --json: %v", err)
	}

	var decoded []jsonTemplate
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out.String())
	}
	if len(decoded) < 8 {
		t.Fatalf("expected the whole library, got %d entries", len(decoded))
	}
	for _, entry := range decoded {
		if entry.Name == "" || entry.Description == "" {
			t.Errorf("incomplete entry: %+v", entry)
		}
	}
}

func TestThemesJSONCarriesOrigin(t *testing.T) {
	console, out, _ := testConsole()
	cmd := newThemesCommand(console)
	cmd.SetArgs([]string{"--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("themes --json: %v", err)
	}

	var decoded []jsonTheme
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out.String())
	}
	found := false
	for _, entry := range decoded {
		if entry.Name == "midnight" {
			found = true
			if entry.Origin != themes.BuiltinOrigin {
				t.Errorf("midnight should be built-in, got %q", entry.Origin)
			}
		}
	}
	if !found {
		t.Error("midnight is missing from the listing")
	}
}

// TestThemesShowPrintsSource is what makes a theme editable: --show has to emit
// the file verbatim, comments included, so it can be saved and changed.
func TestThemesShowPrintsSource(t *testing.T) {
	console, out, _ := testConsole()
	cmd := newThemesCommand(console)
	cmd.SetArgs([]string{"--show", "mono"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("themes --show mono: %v", err)
	}
	text := out.String()
	if !strings.HasPrefix(text, "#") {
		t.Error("--show dropped the file's header comment")
	}
	if !strings.Contains(text, "defaults") {
		t.Error("--show produced no defaults block")
	}
}

func TestThemesShowUnknownIsAUsageError(t *testing.T) {
	console, _, _ := testConsole()
	cmd := newThemesCommand(console)
	cmd.SetArgs([]string{"--show", "midnght"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("an unknown theme was accepted")
	}
	var usage usageError
	if !errors.As(err, &usage) {
		t.Errorf("an unknown theme should be a usage error (exit 2), got %T", err)
	}
	if !strings.Contains(err.Error(), "midnight") {
		t.Errorf("the error should suggest `midnight`; got: %v", err)
	}
}
