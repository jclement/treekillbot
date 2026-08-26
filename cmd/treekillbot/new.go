// The new command: scaffold a starter document.
//
// `new` writes a template out verbatim — there is no substitution step, so what
// lands on disk is exactly the file in internal/templates and is guaranteed by
// that package's tests to build. The command's only real decisions are where it
// goes and whether it may overwrite something.
package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jclement/treekillbot/internal/templates"
	"github.com/jclement/treekillbot/internal/ui"
)

func newNewCommand(console *ui.Console) *cobra.Command {
	var (
		output string
		force  bool
	)

	cmd := &cobra.Command{
		Use:   "new <template>",
		Short: "Write a starter document from a built-in template",
		Long: "Write one of the built-in templates to a file, ready to edit and build.\n\n" +
			"`treekillbot templates` lists them. Every template renders as it is written,\n" +
			"so the first thing to do with a new one is build it. With -o - the document\n" +
			"goes to stdout instead, for piping it somewhere else.",
		// MaximumNArgs rather than ExactArgs so that a bare `treekillbot new`
		// can answer with the list of templates, which is what the person who
		// typed it wanted to know.
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return usageError{fmt.Errorf("`new` needs a template name. The built-in templates are: %s",
					strings.Join(templates.Names(), ", "))}
			}
			name := args[0]

			source, err := templates.Source(name)
			if err != nil {
				return usageError{err}
			}

			path := output
			if path == "" {
				path = name + templates.Extension
			}
			if path == "-" {
				if _, err := io.WriteString(console.Out, source); err != nil {
					return err
				}
				console.Ok(fmt.Sprintf("wrote the %s template to stdout", name))
				return nil
			}

			if err := writeNewFile(path, source, force); err != nil {
				return err
			}
			console.Ok(fmt.Sprintf("wrote %s — build it with `treekillbot build %s`", path, path))
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVarP(&output, "output", "o", "", "write the document here; - for stdout (default: <template>.pulp)")
	f.BoolVar(&force, "force", false, "overwrite the file if it already exists")
	return cmd
}

// writeNewFile writes the template, refusing to clobber an existing file.
//
// O_EXCL rather than a Stat followed by a write: the check and the write have
// to be the same operation, or a `new` into a directory something else is
// writing can still destroy a file the tool just said it would not touch.
func writeNewFile(path, source string, force bool) error {
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	file, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return usageError{fmt.Errorf("%s already exists. Pass --force to overwrite it, or -o to write somewhere else", path)}
		}
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if _, err := io.WriteString(file, source); err != nil {
		file.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return file.Close()
}
