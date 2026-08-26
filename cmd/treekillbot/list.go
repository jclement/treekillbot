// The templates and themes commands: what is in the box.
//
// Both are listings, so both write to STDOUT — a listing is the data that was
// asked for, and the same rule that sends a PDF to stdout and the summary to
// stderr sends these there too. `--json` is the machine-readable form; the
// plain form is two aligned columns, because a table with box-drawing in it is
// unpleasant to grep.
//
// Colour is decided by whether STDOUT is a terminal, not by the global console
// setting: the console's colour mode is derived from stderr, and a listing
// piped into `grep` while stderr is still a TTY must not arrive full of escape
// codes.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jclement/treekillbot/internal/templates"
	"github.com/jclement/treekillbot/internal/themes"
	"github.com/jclement/treekillbot/internal/ui"
)

// listRow is one line of a listing: a name, what it is, and an optional note
// off to the right.
type listRow struct {
	Name        string
	Description string
	Note        string
}

func newTemplatesCommand(console *ui.Console) *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "templates",
		Short: "List the starter documents `new` can write",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			available := templates.Available()
			if asJSON {
				return emitTemplatesJSON(console, available)
			}

			rows := make([]listRow, 0, len(available))
			for _, template := range available {
				rows = append(rows, listRow{Name: template.Name, Description: template.Description})
			}
			printRows(console, rows)
			console.Info("write one with `treekillbot new <name>`")
			return nil
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the list as JSON")
	return cmd
}

func newThemesCommand(console *ui.Console) *cobra.Command {
	var (
		asJSON bool
		show   string
	)

	cmd := &cobra.Command{
		Use:   "themes",
		Short: "List the themes, or print one's source with --show",
		Long: "List the available themes.\n\n" +
			"A theme is an ordinary Pulp file of `defaults`. `--show <name>` prints one,\n" +
			"comments and all, so it can be saved into ~/.config/treekillbot/themes and\n" +
			"edited — a user theme with the same name replaces the built-in.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if show != "" {
				source, err := themes.Source(show)
				if err != nil {
					return usageError{err}
				}
				_, err = io.WriteString(console.Out, source)
				return err
			}

			available := themes.Available()
			if asJSON {
				return emitThemesJSON(console, available)
			}

			rows := make([]listRow, 0, len(available))
			for _, theme := range available {
				row := listRow{Name: theme.Name, Description: theme.Description}
				// A built-in needs no annotation; a theme read off disk does,
				// because "why is `default` not what the docs say?" has exactly
				// one answer and this is it.
				if theme.Origin != themes.BuiltinOrigin {
					row.Note = theme.Origin
				}
				rows = append(rows, row)
			}
			printRows(console, rows)
			console.Info("apply one with `--theme <name>`, or copy one to edit with `themes --show <name>`")
			return nil
		},
	}

	f := cmd.Flags()
	f.BoolVar(&asJSON, "json", false, "emit the list as JSON")
	f.StringVar(&show, "show", "", "print a theme's Pulp source instead of the list")
	return cmd
}

// ---- Rendering ----

// printRows writes an aligned two-column listing to stdout.
func printRows(console *ui.Console, rows []listRow) {
	width := 0
	for _, row := range rows {
		if len(row.Name) > width {
			width = len(row.Name)
		}
	}

	color := console.Color && ui.StdoutIsTerminal()
	for _, row := range rows {
		name := fmt.Sprintf("%-*s", width, row.Name)
		description := row.Description
		note := row.Note
		if note != "" {
			note = "  (" + note + ")"
		}
		if color {
			name = console.Palette.Value.Render(name)
			description = console.Palette.Dim.Render(description)
			note = console.Palette.Dim.Render(note)
		}
		fmt.Fprintln(console.Out, strings.TrimRight(name+"  "+description+note, " "))
	}
}

// ---- JSON ----
//
// The two payloads are separate structs rather than one shared shape because
// they are separate contracts: a theme has an origin and a template does not,
// and folding them together would mean emitting a field that is always empty
// for half the callers.

type jsonTemplate struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type jsonTheme struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Origin is "built-in" or the path the theme was read from.
	Origin string `json:"origin"`
}

func emitTemplatesJSON(console *ui.Console, available []templates.Template) error {
	out := make([]jsonTemplate, 0, len(available))
	for _, template := range available {
		out = append(out, jsonTemplate{Name: template.Name, Description: template.Description})
	}
	return encodeJSON(console.Out, out)
}

func emitThemesJSON(console *ui.Console, available []themes.Theme) error {
	out := make([]jsonTheme, 0, len(available))
	for _, theme := range available {
		out = append(out, jsonTheme{Name: theme.Name, Description: theme.Description, Origin: theme.Origin})
	}
	return encodeJSON(console.Out, out)
}

// encodeJSON writes an indented JSON document. Every `--json` listing in the
// CLI goes through it, so their shape cannot drift apart one flag at a time.
func encodeJSON(w io.Writer, payload any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}
