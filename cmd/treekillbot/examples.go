// The examples command: the documents compiled into the binary.
package main

import (
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/jclement/treekillbot/internal/examples"
	"github.com/jclement/treekillbot/internal/ui"
)

func newExamplesCommand(console *ui.Console) *cobra.Command {
	var (
		show   string
		output string
		asJSON bool
		all    bool
		force  bool
	)

	cmd := &cobra.Command{
		Use:   "examples",
		Short: "List the example documents built into the binary",
		Long: "The examples are finished, designed documents: a weekly spread, a day page, a\n" +
			"Cornell sheet, engineering graph paper, and the stress-test reference sheets.\n\n" +
			"They travel with the binary, so `brew install` is enough — no repository needed.\n" +
			"Use `--show <name>` to print one, or `--show <name> -o file.pulp` to start from it.\n\n" +
			"For a plain starting point to edit rather than a finished design, see\n" +
			"`treekillbot templates`.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if show != "" {
				return showExample(console, show, output, force)
			}
			return listExamples(console, all, asJSON)
		},
	}

	f := cmd.Flags()
	f.StringVar(&show, "show", "", "print one example's source")
	f.StringVarP(&output, "output", "o", "", "with --show, write to this file instead of stdout")
	f.BoolVar(&all, "all", false, "include the stress-test reference sheets")
	f.BoolVar(&asJSON, "json", false, "emit the listing as JSON")
	f.BoolVar(&force, "force", false, "overwrite an existing file")
	return cmd
}

func listExamples(console *ui.Console, all, asJSON bool) error {
	available := examples.Available()
	if !all {
		var documents []examples.Example
		for _, example := range available {
			if example.Group == examples.GroupDocument {
				documents = append(documents, example)
			}
		}
		available = documents
	}

	if asJSON {
		return encodeJSON(console.Out, available)
	}

	w := tabwriter.NewWriter(console.Out, 0, 0, 2, ' ', 0)
	for _, example := range available {
		fmt.Fprintf(w, "%s\t%s\n", example.Name, example.Description)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if !all {
		hidden := len(examples.Available()) - len(available)
		if hidden > 0 && !console.Quiet {
			console.Info(fmt.Sprintf("%d stress-test sheets hidden; use --all", hidden))
		}
	}
	return nil
}

// showExample prints one example, or writes it to a file so it can be edited.
func showExample(console *ui.Console, name, output string, force bool) error {
	source, err := examples.Source(name)
	if err != nil {
		return usageError{err}
	}
	if output == "" || output == "-" {
		_, err := fmt.Fprint(console.Out, source)
		return err
	}

	// O_EXCL rather than a Stat first: the check and the write would otherwise
	// be two steps with a gap between them, and the whole point is not to
	// clobber work.
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	file, err := os.OpenFile(output, flags, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return usageError{fmt.Errorf("%s already exists; pass --force to overwrite it", output)}
		}
		return err
	}
	defer file.Close()
	if _, err := io.WriteString(file, source); err != nil {
		return err
	}

	console.Ok(fmt.Sprintf("wrote %s\n\n    treekillbot build %s", output, output))
	return nil
}
