// Command treekillbot compiles Pulp documents into print-ready PDFs.
//
// The command layer is deliberately thin: it parses flags, decides where output
// goes, and hands everything else to internal/pipeline. Anything with a
// decision in it belongs in a package that can be tested without a process.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/jclement/treekillbot/internal/ui"
	"github.com/jclement/treekillbot/internal/version"
)

// Exit codes. 3 and 4 deviate from the usual 0/1/2 deliberately: a build tool's
// callers ask exactly one question — is my input wrong, or is the tool broken? —
// and collapsing those into 1 breaks both CI and editor integration.
const (
	exitOK          = 0
	exitRuntime     = 1
	exitUsage       = 2
	exitSourceError = 3
	exitWarnings    = 4
)

// usageError marks a failure as the user's mistake rather than the tool's, so
// main can map it to the right exit code without inspecting the message.
type usageError struct{ err error }

func (u usageError) Error() string { return u.err.Error() }
func (u usageError) Unwrap() error { return u.err }

// sourceError marks a failure as a problem in the document. Diagnostics have
// already been printed by the time one of these is returned.
type sourceError struct{ warningsOnly bool }

func (sourceError) Error() string { return "the document has errors" }

func main() {
	console := ui.NewConsole(false)
	if err := newRootCommand(console).Execute(); err != nil {
		os.Exit(exitCodeFor(console, err))
	}
}

// exitCodeFor maps an error to an exit code, printing it unless it has already
// been reported.
func exitCodeFor(console *ui.Console, err error) int {
	var src sourceError
	if errors.As(err, &src) {
		if src.warningsOnly {
			return exitWarnings
		}
		return exitSourceError
	}
	var usage usageError
	if errors.As(err, &usage) {
		console.Fatal(err)
		return exitUsage
	}
	console.Fatal(err)
	return exitRuntime
}

func newRootCommand(console *ui.Console) *cobra.Command {
	var noColor, quiet, verbose bool

	root := &cobra.Command{
		Use:   "treekillbot",
		Short: "Compile Pulp documents into print-ready PDFs",
		Long: "treekillbot turns a small text description into a pixel-perfect PDF:\n" +
			"weekly planner spreads, day pages, dot-grid notebooks, ruled TODO panels.\n\n" +
			"It kills trees. That is the point.",
		SilenceUsage:  true,
		SilenceErrors: true,
		// The full line, not just the tag: the go-cli house style is
		// "<app> v1.2.3 (abc1234, 2026-01-15T10:30:00Z)", and a bare "dev" tells
		// a bug reporter nothing about which build they are running.
		Version: version.String(),
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if noColor {
				*console = *ui.NewConsole(true)
			}
			console.Quiet = quiet
			console.Verbose = verbose
		},
		// With no subcommand, a bare file argument builds it — `treekillbot
		// weekly.pulp` is what everyone types first, and refusing it to insist
		// on `build` would be pedantry.
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return runBuild(console, args, defaultBuildFlags())
		},
	}

	root.PersistentFlags().BoolVar(&noColor, "no-color", false, "disable coloured output")
	root.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "suppress the build summary")
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "explain what is happening")
	root.SetVersionTemplate("{{.Version}}\n")

	root.AddCommand(
		newBuildCommand(console),
		newCheckCommand(console),
		newEditCommand(console),
		newFmtCommand(console),
		newDocsCommand(console),
		newNewCommand(console),
		newTemplatesCommand(console),
		newExamplesCommand(console),
		newThemesCommand(console),
		newVersionCommand(console),
	)
	return root
}

func newVersionCommand(console *ui.Console) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version, build and runtime details",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(console.Out, version.Detailed())
			return nil
		},
	}
}
