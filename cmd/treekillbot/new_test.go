package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jclement/treekillbot/internal/ui"
)

// testConsole returns a console whose two streams can be inspected separately,
// because the thing most worth asserting about this CLI is which of them a
// given piece of output went to.
func testConsole() (*ui.Console, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	return &ui.Console{Out: &out, Err: &errOut}, &out, &errOut
}

// TestNewRefusesToOverwrite is the branch worth testing: everything else in
// `new` is a file copy, and this is the one place the command can destroy
// something the user cared about.
func TestNewRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mine.pulp")
	existing := "# do not lose me\n"
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	console, _, _ := testConsole()
	cmd := newNewCommand(console)
	cmd.SetArgs([]string{"notes", "-o", path})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("new overwrote an existing file")
	}
	var usage usageError
	if !errors.As(err, &usage) {
		t.Errorf("refusing to overwrite should be a usage error (exit 2), got %T", err)
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("the error should name the way out; got: %v", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != existing {
		t.Error("the existing file was modified despite the refusal")
	}
}

func TestNewForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mine.pulp")
	if err := os.WriteFile(path, []byte("# old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	console, _, _ := testConsole()
	cmd := newNewCommand(console)
	cmd.SetArgs([]string{"notes", "-o", path, "--force"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("new --force: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), "page") {
		t.Errorf("--force did not write the template; got %q", string(after))
	}
}

// TestNewWritesTheDocumentToStdout pins the stream contract: with -o - the
// document is the artifact and owns stdout, and the human line stays on stderr
// so `treekillbot new dotgrid -o - > x.pulp` produces a clean file.
func TestNewWritesTheDocumentToStdout(t *testing.T) {
	console, out, errOut := testConsole()
	cmd := newNewCommand(console)
	cmd.SetArgs([]string{"dotgrid", "-o", "-"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("new -o -: %v", err)
	}
	if !strings.Contains(out.String(), "line-style: dotted") {
		t.Error("the template did not go to stdout")
	}
	if strings.Contains(out.String(), "wrote") {
		t.Error("a human message leaked onto stdout")
	}
	if !strings.Contains(errOut.String(), "stdout") {
		t.Errorf("nothing on stderr said what happened; got %q", errOut.String())
	}
}

func TestNewUnknownTemplateIsAUsageError(t *testing.T) {
	console, _, _ := testConsole()
	cmd := newNewCommand(console)
	cmd.SetArgs([]string{"weekley", "-o", "-"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("an unknown template was accepted")
	}
	var usage usageError
	if !errors.As(err, &usage) {
		t.Errorf("an unknown template should be a usage error (exit 2), got %T", err)
	}
	if !strings.Contains(err.Error(), "weekly") {
		t.Errorf("the error should suggest `weekly`; got: %v", err)
	}
}

func TestNewWithNoTemplateListsThem(t *testing.T) {
	console, _, _ := testConsole()
	cmd := newNewCommand(console)
	cmd.SetArgs(nil)
	err := cmd.Execute()
	if err == nil {
		t.Fatal("a bare `new` was accepted")
	}
	if !strings.Contains(err.Error(), "cornell") {
		t.Errorf("the error should list the templates; got: %v", err)
	}
}

// TestNewDefaultsToTheTemplateName covers the path someone actually types.
func TestNewDefaultsToTheTemplateName(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	console, _, errOut := testConsole()
	cmd := newNewCommand(console)
	cmd.SetArgs([]string{"todo"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("new todo: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "todo.pulp")); err != nil {
		t.Fatalf("todo.pulp was not written: %v", err)
	}
	if !strings.Contains(errOut.String(), "treekillbot build todo.pulp") {
		t.Errorf("the next command was not suggested; got %q", errOut.String())
	}
}
