// The fmt command: canonical formatting for Pulp documents.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jclement/treekillbot/internal/pulp"
	"github.com/jclement/treekillbot/internal/schema"
	"github.com/jclement/treekillbot/internal/ui"
)

func newFmtCommand(console *ui.Console) *cobra.Command {
	var (
		check bool
		list  bool
		diff  bool
	)

	cmd := &cobra.Command{
		Use:   "fmt [file.pulp...]",
		Short: "Rewrite documents in canonical form",
		Long: "Rewrite Pulp documents in canonical form: two-space indentation, elements in the\n" +
			"bare form and properties in the colon form, comments as `# `.\n\n" +
			"With no files, formats standard input to standard output.\n" +
			"A file that does not parse is left alone — moving the text an error points at\n" +
			"only makes the error harder to find.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			options := pulp.FormatOptions{Canonical: canonicalName}

			if len(args) == 0 {
				return formatStdin(console, options)
			}

			paths, err := expandPulpPaths(args)
			if err != nil {
				return err
			}

			var changed []string
			for _, path := range paths {
				original, err := os.ReadFile(path)
				if err != nil {
					return fmt.Errorf("reading %s: %w", path, err)
				}
				formatted, diags := pulp.Format(pulp.NewSource(path, string(original)), options)
				if diags.HasErrors() {
					console.PrintDiagnostics(diags)
					return sourceError{}
				}
				if formatted == string(original) {
					continue
				}
				changed = append(changed, path)

				switch {
				case list || check:
					fmt.Fprintln(console.Out, path)
				case diff:
					fmt.Fprint(console.Out, unifiedDiff(path, string(original), formatted))
				default:
					if err := os.WriteFile(path, []byte(formatted), fileMode(path)); err != nil {
						return fmt.Errorf("writing %s: %w", path, err)
					}
				}
			}

			if check && len(changed) > 0 {
				console.Warn(fmt.Sprintf("%d file%s not in canonical form", len(changed), pluralS(len(changed))))
				// Exit 3, the source-problem code: the input is not what it
				// should be, which is a different thing from the tool failing.
				return sourceError{}
			}
			if !list && !check && !diff && !console.Quiet {
				if len(changed) == 0 {
					console.Ok("already canonical")
				} else {
					console.Ok(fmt.Sprintf("formatted %d file%s", len(changed), pluralS(len(changed))))
				}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&check, "check", false, "list files that need formatting and exit 3 if any do")
	cmd.Flags().BoolVarP(&list, "list", "l", false, "list files that need formatting")
	cmd.Flags().BoolVarP(&diff, "diff", "d", false, "print a diff instead of rewriting")
	return cmd
}

// canonicalName bridges the schema to the formatter, which cannot import it.
func canonicalName(name string) (string, bool, bool) {
	if def, ok := schema.Element(name); ok {
		return def.Name, true, true
	}
	if id, ok := schema.Lookup(name); ok {
		return schema.Name(id), false, true
	}
	return name, false, false
}

func formatStdin(console *ui.Console, options pulp.FormatOptions) error {
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("reading stdin: %w", err)
	}
	formatted, diags := pulp.Format(pulp.NewSource("<stdin>", string(input)), options)
	if diags.HasErrors() {
		console.PrintDiagnostics(diags)
		return sourceError{}
	}
	_, err = io.WriteString(console.Out, formatted)
	return err
}

// expandPulpPaths turns arguments into a file list, walking directories so that
// `treekillbot fmt .` does what everyone expects.
func expandPulpPaths(args []string) ([]string, error) {
	var out []string
	for _, arg := range args {
		info, err := os.Stat(arg)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", arg, err)
		}
		if !info.IsDir() {
			out = append(out, arg)
			continue
		}
		err = filepath.WalkDir(arg, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				// Skipping dot-directories keeps `fmt .` from wandering into
				// .git and every vendored tree under it.
				if name := entry.Name(); name != "." && strings.HasPrefix(name, ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if isPulpFile(path) {
				out = append(out, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func isPulpFile(path string) bool {
	switch filepath.Ext(path) {
	case ".pulp", ".pulptheme":
		return true
	}
	return false
}

// fileMode preserves an existing file's permissions rather than forcing 0644.
func fileMode(path string) os.FileMode {
	if info, err := os.Stat(path); err == nil {
		return info.Mode().Perm()
	}
	return 0o644
}

// unifiedDiff renders a minimal line diff.
//
// It is deliberately naive — a full Myers diff for a formatter that only ever
// changes whitespace and punctuation would be a lot of code to make the output
// marginally tidier.
func unifiedDiff(path, before, after string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n+++ %s (formatted)\n", path, path)
	oldLines := strings.Split(before, "\n")
	newLines := strings.Split(after, "\n")
	for i := 0; i < len(oldLines) || i < len(newLines); i++ {
		var oldLine, newLine string
		if i < len(oldLines) {
			oldLine = oldLines[i]
		}
		if i < len(newLines) {
			newLine = newLines[i]
		}
		if oldLine == newLine {
			continue
		}
		if i < len(oldLines) {
			fmt.Fprintf(&b, "-%s\n", oldLine)
		}
		if i < len(newLines) {
			fmt.Fprintf(&b, "+%s\n", newLine)
		}
	}
	return b.String()
}
