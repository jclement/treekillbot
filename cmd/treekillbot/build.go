// The build command.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/jclement/treekillbot/internal/compile"
	"github.com/jclement/treekillbot/internal/pipeline"
	"github.com/jclement/treekillbot/internal/pulp"
	"github.com/jclement/treekillbot/internal/themes"
	"github.com/jclement/treekillbot/internal/ui"
	"github.com/jclement/treekillbot/internal/version"
)

// buildFlags is every knob `build` accepts.
type buildFlags struct {
	output         string
	vars           []string
	varsFile       string
	date           string
	weekStart      string
	theme          string
	pageSize       string
	orientation    string
	fontDir        string
	grayscale      bool
	noCompress     bool
	debugLayout    bool
	dumpLayout     bool
	allowOverflow  bool
	allowUndefined bool
	repeat         int
	step           string
	unsafeEnv      bool
	strict         bool
	open           bool
}

func defaultBuildFlags() buildFlags { return buildFlags{} }

func newBuildCommand(console *ui.Console) *cobra.Command {
	flags := defaultBuildFlags()

	cmd := &cobra.Command{
		Use:   "build <file.pulp>",
		Short: "Compile a document into a PDF",
		Long: "Compile a Pulp document into a print-ready PDF.\n\n" +
			"With -o -, the PDF goes to stdout and the summary still goes to stderr,\n" +
			"so `treekillbot build week.pulp -o - > week.pdf` shows you what it made.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBuild(console, args, flags)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&flags.output, "output", "o", "", "write the PDF here; - for stdout (default: the source name with .pdf)")
	f.StringArrayVar(&flags.vars, "var", nil, "set a document variable, as name=value (repeatable)")
	f.StringVar(&flags.varsFile, "vars-file", "", "read variables from a file")
	f.StringVar(&flags.date, "date", "", "render as though today were this date (YYYY-MM-DD)")
	f.StringVar(&flags.weekStart, "week-start", "", "first day of the week: monday, sunday or saturday")
	f.StringVar(&flags.theme, "theme", "", "apply a named theme")
	f.StringVar(&flags.pageSize, "page-size", "", "override the page size, e.g. a4 or '200mm 300mm'")
	f.StringVar(&flags.orientation, "orientation", "", "override the orientation: portrait or landscape")
	f.StringVar(&flags.fontDir, "font-dir", "", "load additional fonts from this directory")
	f.BoolVar(&flags.grayscale, "grayscale", false, "convert every colour to grey for printing")
	f.BoolVar(&flags.noCompress, "no-compress", false, "leave the PDF uncompressed and readable")
	f.BoolVar(&flags.debugLayout, "debug-layout", false, "draw every box's rectangles over the artwork")
	f.BoolVar(&flags.dumpLayout, "dump-layout", false, "print the computed rectangle tree instead of writing a PDF")
	f.BoolVar(&flags.allowOverflow, "allow-overflow", false, "downgrade overflow errors to warnings")
	f.BoolVar(&flags.allowUndefined, "allow-undefined", false, "render undefined variables as empty instead of failing")
	f.BoolVar(&flags.unsafeEnv, "unsafe-env", false, "allow a document to read any environment variable (see DESIGN.md D11)")
	f.BoolVar(&flags.strict, "strict", false, "treat warnings as failures")
	f.IntVar(&flags.repeat, "repeat", 1, "render this many pages, advancing the date by --step each time")
	f.StringVar(&flags.step, "step", "1w", "how far the date moves between repeated pages: 1d, 2w, 1m, 1y")
	f.BoolVar(&flags.open, "open", false, "open the PDF when it is written")
	return cmd
}

func runBuild(console *ui.Console, args []string, flags buildFlags) error {
	started := time.Now()
	source := args[0]

	opts, err := buildOptionsFor(source, flags)
	if err != nil {
		return err
	}

	console.PrintBanner(version.Tag())

	// --dump-layout stops before the PDF writer; everything else renders.
	var result *pipeline.Result
	if flags.dumpLayout {
		result, err = pipeline.BuildFile(source, pipeline.StageLayout, opts)
	} else {
		result, err = pipeline.BuildDocumentFile(source, opts)
	}
	if err != nil {
		return err
	}
	console.PrintDiagnostics(result.Diags)

	if result.Diags.HasErrors() {
		console.PrintFailure(countErrors(result.Diags))
		return sourceError{}
	}

	if flags.dumpLayout {
		fmt.Fprint(console.Out, result.LayoutDump)
		return warningGate(console, result.Diags, flags.strict)
	}

	output := outputPath(source, flags.output)
	if err := writePDF(console, output, result.PDF); err != nil {
		return err
	}
	reportMissingGlyphs(console, result)

	console.PrintSummary(ui.Summary{
		Source:  source,
		Output:  output,
		Theme:   flags.theme,
		Date:    flags.date,
		Result:  result,
		Bytes:   len(result.PDF),
		Fonts:   countFonts(result),
		Elapsed: time.Since(started),
	})

	if flags.open && output != "-" {
		openFile(console, output)
	}
	return warningGate(console, result.Diags, flags.strict)
}

// buildOptions turns flags into pipeline options, reporting anything malformed
// as a usage error rather than letting it surface later as something stranger.
func buildOptions(flags buildFlags) (pipeline.Options, error) {
	return buildOptionsFor("", flags)
}

// buildOptionsFor resolves flags into pipeline options, relative to the
// document being built so that a theme beside it can shadow a built-in.
func buildOptionsFor(sourceDir string, flags buildFlags) (pipeline.Options, error) {
	assignments, err := parseVarAssignments(flags.vars, flags.varsFile)
	if err != nil {
		return pipeline.Options{}, err
	}

	opts := pipeline.Options{
		Repeat:         flags.repeat,
		Step:           flags.step,
		Vars:           assignments,
		WeekStart:      flags.weekStart,
		AllowUndefined: flags.allowUndefined,
		UnsafeEnv:      flags.unsafeEnv,
		FontDir:        flags.fontDir,
		Grayscale:      flags.grayscale,
		NoCompress:     flags.noCompress,
		DebugLayout:    flags.debugLayout,
		AllowOverflow:  flags.allowOverflow,
		Creator:        "treekillbot " + version.Tag(),
		ThemeDir:       filepath.Dir(sourceDir),
		ResolveTheme:   themes.LoadFrom,
	}

	// The document's timestamp comes from --date when given, and from
	// SOURCE_DATE_EPOCH otherwise, so a reproducible build stays reproducible
	// without anyone having to remember a flag.
	if flags.date != "" {
		parsed, err := time.Parse("2006-01-02", flags.date)
		if err != nil {
			return opts, usageError{fmt.Errorf("--date must look like 2026-09-07, got %q", flags.date)}
		}
		opts.Created = parsed
		opts.Anchor = parsed
	} else if epoch := os.Getenv("SOURCE_DATE_EPOCH"); epoch != "" {
		var seconds int64
		if _, err := fmt.Sscan(epoch, &seconds); err == nil {
			opts.Created = time.Unix(seconds, 0).UTC()
		}
	}
	if flags.weekStart != "" {
		switch flags.weekStart {
		case "monday", "sunday", "saturday":
		default:
			return opts, usageError{fmt.Errorf("--week-start must be monday, sunday or saturday, got %q", flags.weekStart)}
		}
	}

	if flags.theme != "" {
		// Resolved relative to the document so a theme file sitting beside it
		// shadows the built-in of the same name, which is how someone iterates
		// on a theme without installing it anywhere.
		theme, err := themes.LoadFrom(filepath.Dir(sourceDir), flags.theme)
		if err != nil {
			return opts, usageError{err}
		}
		opts.Theme = theme
	}

	if flags.pageSize != "" {
		size, err := parsePageSize(flags.pageSize)
		if err != nil {
			return opts, err
		}
		opts.PageSize = &size
	}
	if flags.orientation != "" {
		switch flags.orientation {
		case "portrait", "landscape":
		default:
			return opts, usageError{fmt.Errorf("--orientation must be portrait or landscape, got %q", flags.orientation)}
		}
		opts.Orientation = flags.orientation
	}
	return opts, nil
}

// parseVarAssignments reads --var name=value pairs and an optional file of the
// same, with the command line winning on a conflict.
func parseVarAssignments(pairs []string, file string) (map[string]string, error) {
	out := map[string]string{}
	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", file, err)
		}
		for number, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			name, value, ok := strings.Cut(trimmed, "=")
			if !ok {
				return nil, usageError{fmt.Errorf("%s:%d: expected name=value, got %q", file, number+1, trimmed)}
			}
			out[strings.TrimSpace(name)] = strings.TrimSpace(value)
		}
	}
	for _, pair := range pairs {
		name, value, ok := strings.Cut(pair, "=")
		if !ok {
			return nil, usageError{fmt.Errorf("--var expects name=value, got %q", pair)}
		}
		out[strings.TrimSpace(name)] = value
	}
	return out, nil
}

// parsePageSize accepts either a name or a width and height pair.
func parsePageSize(text string) (compile.PageSize, error) {
	src := pulp.NewSource("--page-size", text)
	var diags pulp.Diagnostics
	values := pulp.ParseValues(src, pulp.Span{Start: 0, End: len(text)}, text, &diags)
	if len(values) == 2 && values[0].Kind == pulp.KindLength && values[1].Kind == pulp.KindLength {
		return compile.PageSize{Width: values[0].Length, Height: values[1].Length, Name: "custom"}, nil
	}
	size := compile.NamedPageSize(text)
	if size.Name == "letter" && !strings.EqualFold(strings.TrimSpace(text), "letter") {
		return size, usageError{fmt.Errorf("unknown page size %q; known sizes are %s",
			text, strings.Join(compile.PageSizeNames(), ", "))}
	}
	return size, nil
}

// outputPath decides where the PDF goes: the flag if given, otherwise the
// source's name with a .pdf extension, in the current directory.
func outputPath(source, flag string) string {
	if flag != "" {
		return flag
	}
	base := filepath.Base(source)
	return strings.TrimSuffix(base, filepath.Ext(base)) + ".pdf"
}

// writePDF sends the document to a file or to stdout.
func writePDF(console *ui.Console, path string, data []byte) error {
	if path != "-" {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
		return nil
	}
	// Writing a PDF to a terminal fills the screen with binary and teaches
	// nobody anything, so it is refused rather than merely discouraged.
	if ui.StdoutIsTerminal() {
		return usageError{fmt.Errorf("refusing to write a PDF to the terminal; redirect it, as in `-o - > out.pdf`, or give a filename")}
	}
	_, err := console.Out.Write(data)
	return err
}

// reportMissingGlyphs warns about characters no embedded font could draw.
//
// This exists because the PDF library silently substitutes a space, so without
// it a document containing an unavailable glyph renders as a gap with nothing
// anywhere to say why.
func reportMissingGlyphs(console *ui.Console, result *pipeline.Result) {
	if len(result.MissingGlyphs) == 0 {
		return
	}
	var shown []string
	for i, r := range result.MissingGlyphs {
		if i == 8 {
			shown = append(shown, fmt.Sprintf("and %d more", len(result.MissingGlyphs)-8))
			break
		}
		shown = append(shown, fmt.Sprintf("%q (U+%04X)", r, r))
	}
	console.Warn("some characters are not in any embedded font and were dropped: " + strings.Join(shown, ", "))
}

// warningGate turns warnings into a failure under --strict.
func warningGate(console *ui.Console, diags pulp.Diagnostics, strict bool) error {
	if !strict {
		return nil
	}
	count := 0
	for _, d := range diags {
		if d.Severity == pulp.SeverityWarning {
			count++
		}
	}
	if count == 0 {
		return nil
	}
	console.Warn(fmt.Sprintf("%d warning%s, and --strict was given", count, pluralS(count)))
	return sourceError{warningsOnly: true}
}

func countErrors(diags pulp.Diagnostics) int {
	count := 0
	for _, d := range diags {
		if d.Severity == pulp.SeverityError {
			count++
		}
	}
	return count
}

// countFonts reports how many distinct faces the document embedded.
func countFonts(result *pipeline.Result) int { return result.FontsUsed }

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
