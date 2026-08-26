// The edit command: a side-by-side editor and live preview in the browser.
package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"github.com/jclement/treekillbot/internal/editor"
	"github.com/jclement/treekillbot/internal/ui"
)

func newEditCommand(console *ui.Console) *cobra.Command {
	var (
		addr   string
		noOpen bool
		flags  = defaultBuildFlags()
	)

	cmd := &cobra.Command{
		Use:   "edit <file.pulp>",
		Short: "Edit a document with a live preview in the browser",
		Long: "Open a side-by-side editor: the document on the left, a live preview on the right.\n\n" +
			"The preview is drawn by the same code that draws the PDF, over the same computed\n" +
			"geometry, so what you see is what prints — not an approximation of it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			if _, err := os.Stat(path); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return usageError{fmt.Errorf("%s does not exist; create it with `treekillbot new weekly -o %s`", path, path)}
				}
				return err
			}

			buildOpts, err := buildOptionsFor(path, flags)
			if err != nil {
				return err
			}

			server, err := editor.New(editor.Options{Path: path, Addr: addr, Build: buildOpts})
			if err != nil {
				return err
			}
			listener, url, err := server.Listen()
			if err != nil {
				return err
			}

			console.PrintBanner("")
			console.Ok("editing " + path + "\n\n    " + url + "\n\n  Press Ctrl-C to stop.")
			if !noOpen {
				openURL(console, url)
			}

			// http.ErrServerClosed is what a clean shutdown looks like, not a
			// failure worth an exit code.
			if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&addr, "addr", "127.0.0.1:0", "address to listen on; port 0 picks a free one")
	f.BoolVar(&noOpen, "no-open", false, "do not open a browser")
	f.StringArrayVar(&flags.vars, "var", nil, "set a document variable, as name=value (repeatable)")
	f.StringVar(&flags.date, "date", "", "preview as though today were this date (YYYY-MM-DD)")
	f.StringVar(&flags.theme, "theme", "", "apply a named theme")
	f.StringVar(&flags.weekStart, "week-start", "", "first day of the week: monday, sunday or saturday")
	f.StringVar(&flags.fontDir, "font-dir", "", "load additional fonts from this directory")
	f.BoolVar(&flags.grayscale, "grayscale", false, "preview in grey, as it would print")
	f.BoolVar(&flags.debugLayout, "debug-layout", false, "draw every box's rectangles over the artwork")
	return cmd
}
