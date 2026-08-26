// The check command: parse and validate without rendering.
//
// This exists for editors and for CI. It is the fast path — no fonts, no
// layout, no PDF writer — and its output is the diagnostics and nothing else,
// so `treekillbot check` in a save hook costs a millisecond and tells you
// immediately when you have mistyped a property.
package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jclement/treekillbot/internal/pipeline"
	"github.com/jclement/treekillbot/internal/pulp"
	"github.com/jclement/treekillbot/internal/ui"
)

func newCheckCommand(console *ui.Console) *cobra.Command {
	var (
		asJSON bool
		strict bool
		layout bool
	)

	cmd := &cobra.Command{
		Use:   "check <file.pulp>...",
		Short: "Parse and validate without producing a PDF",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			stage := pipeline.StageValidate
			if layout {
				// Checking layout too catches the errors that only appear once
				// boxes have real sizes — overflow, most of all — at the cost
				// of resolving fonts.
				stage = pipeline.StageLayout
			}

			var all pulp.Diagnostics
			for _, path := range args {
				result, err := pipeline.BuildFile(path, stage, pipeline.Options{})
				if err != nil {
					return err
				}
				all = append(all, result.Diags...)
			}
			all.Sort()

			if asJSON {
				return emitJSON(console, all)
			}
			console.PrintDiagnostics(all)

			errorCount, warningCount := countBySeverity(all)
			if errorCount > 0 {
				console.PrintFailure(errorCount)
				return sourceError{}
			}
			if !console.Quiet && warningCount == 0 {
				console.Ok(fmt.Sprintf("%d file%s, no problems", len(args), pluralS(len(args))))
			}
			if strict && warningCount > 0 {
				return sourceError{warningsOnly: true}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "emit diagnostics as JSON for editor integration")
	cmd.Flags().BoolVar(&strict, "strict", false, "treat warnings as failures")
	cmd.Flags().BoolVar(&layout, "layout", false, "also run layout, catching overflow and fit problems")
	return cmd
}

// jsonDiagnostic is the editor-facing shape. It is deliberately flat and uses
// the field names the language-server protocol and most editor plugins already
// expect, so wiring one up needs no translation layer.
type jsonDiagnostic struct {
	File     string `json:"file,omitempty"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	EndLine  int    `json:"endLine,omitempty"`
	EndCol   int    `json:"endColumn,omitempty"`
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Help     string `json:"help,omitempty"`
}

func emitJSON(console *ui.Console, diags pulp.Diagnostics) error {
	out := make([]jsonDiagnostic, 0, len(diags))
	for _, d := range diags {
		entry := jsonDiagnostic{
			Severity: d.Severity.String(),
			Code:     d.Code,
			Message:  d.Message,
			Help:     d.Help,
		}
		if d.Source != nil {
			start := d.Position()
			end := d.Source.Position(d.Span.End)
			entry.File = d.Source.Name
			entry.Line, entry.Column = start.Line, start.Column
			entry.EndLine, entry.EndCol = end.Line, end.Column
		}
		out = append(out, entry)
	}
	encoder := json.NewEncoder(console.Out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(out)
}

func countBySeverity(diags pulp.Diagnostics) (errors, warnings int) {
	for _, d := range diags {
		switch d.Severity {
		case pulp.SeverityError:
			errors++
		case pulp.SeverityWarning:
			warnings++
		}
	}
	return errors, warnings
}
