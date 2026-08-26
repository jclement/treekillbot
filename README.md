# treekillbot

Compiles **Pulp**, a small indentation-based DSL, into pixel-perfect print-ready PDFs —
weekly planner spreads, day pages, Cornell notes, dot-grid notebooks, ruled TODO panels.

One static Go binary. No external tools, no runtime font dependencies, no network access.

It kills trees. That is the point.

## Install

```sh
brew install jclement/tap/treekillbot          # also installs it as `tkb`
go install github.com/jclement/treekillbot/cmd/treekillbot@latest
docker run --rm -v "$PWD:/work" ghcr.io/jclement/treekillbot build doc.pulp -o out.pdf
```

Or grab a binary from [releases](https://github.com/jclement/treekillbot/releases). Every
release ships a `checksums.txt` signed with keyless cosign.

## Quick start

```sh
treekillbot new weekly -o weekly.pulp     # a plain starting point to edit
treekillbot build weekly.pulp             # → weekly.pdf
treekillbot edit weekly.pulp              # side-by-side editor + live preview
```

Everything ships inside the binary, so a `brew install` is all you need:

```sh
treekillbot examples                              # the finished, designed documents
treekillbot examples --show daily -o daily.pulp   # start from one
treekillbot examples --all                        # plus the stress-test sheets
```

`new` gives you something deliberately plain that scaffolds cleanly under any
`--theme`; `examples --show` gives you a finished design with its own typography and
greys. Both are `.pulp` files you own from that point on.

Or write one by hand. Put this in `notes.pulp`:

```pulp
page
  padding: 0.5in

  panel "This week"
    height: 2in
    border: 0.5pt
    padding: 6pt
    line-style: ruled
    line-pitch: 0.25in

  panel "Notes"
    height: fill
    border: 0.5pt
    padding: 6pt
    line-style: dotted
    line-pitch: 0.2in
```

```sh
treekillbot build notes.pulp -o notes.pdf
```

## The language in sixty seconds

**Every line is a node**: a name, an optional argument, and an optional indented block.
The schema — not the grammar — decides which names are elements and which are properties.
That is why `panel "Notes"` can carry both a value and children, and why three sibling
`section` blocks are unremarkable. (See [DESIGN.md](DESIGN.md) D5 for why it is not YAML.)

- **Width fills by default.** Height is where the interesting sizing lives: `fill`,
  `fill(2)`, `40%`, `auto`, or a length.
- **A run of consecutive `column` siblings forms a row.** That is the only horizontal
  construct.
- **Lengths carry a unit** — `16pt`, `0.5in`, `12mm`. A bare number is an error that tells
  you what to write instead. The exception is `0`, whose unit cannot matter.
- **Touching borders collapse.** Two boxes that share an edge draw it once, so a lattice of
  cells has interior lines the same weight as its outer frame. `border-collapse: false` if
  you want the double rule.
- **Dates are built in**, which is the whole point of a planner generator:

```pulp
section
  height: fill
  gap: 5pt
  for day in week.days
    column
      panel "{day.short} {day.dd}"
        height: fill
        line-style: ruled
        line-pitch: 15pt
```

```sh
treekillbot build weekly.pulp --date 2026-09-09          # print next week's page today
treekillbot build weekly.pulp --repeat 13 --step 1w      # a quarter of spreads, one job
```

A document can pick its own theme with a `theme` directive, and `--theme` on the command
line overrides it. Every theme defines the same five semantic colours, so a document that
references them survives a theme swap — including onto `midnight`, where the ink is pale
and the paper is dark:

```pulp
theme blueprint

section
  text "Heading"
    color: {accent}
  text "Subtitle"
    color: {muted}
```

`treekillbot docs props` lists every property; `treekillbot docs elements` every element,
and `treekillbot themes --show blueprint` prints a theme's source to copy and edit.

## Commands

`build` is the default verb, so `treekillbot weekly.pulp` and
`treekillbot build weekly.pulp` are the same thing.

| Command | What it does |
|---|---|
| `build` | Compile a `.pulp` file to PDF |
| `edit` | Side-by-side editor with a live preview in the browser |
| `check` | Parse and validate without rendering — for editors and CI |
| `fmt` | Rewrite documents in canonical form |
| `new` | Start a document from a built-in template |
| `templates` | List the built-in starting points |
| `examples` | List the example documents; `--show <name>` prints or writes one |
| `themes` | List themes; `--show <name>` prints one's source to copy and edit |
| `docs` | Reference for properties, elements, errors, colours and page sizes |
| `version` | Version, commit, build date, Go version, OS/arch |

### `build` flags

| Flag | Meaning |
|---|---|
| `-o, --output` | Where the PDF goes. `-` writes to stdout |
| `--var name=value` | Set a document variable (repeatable) |
| `--vars-file` | Read `name=value` lines from a file |
| `--date YYYY-MM-DD` | Render as though today were this date |
| `--repeat N` | Render N pages, advancing the date by `--step` each time |
| `--step` | How far the date moves between pages: `1d`, `2w`, `1m`, `1y` |
| `--week-start` | `monday` (default), `sunday` or `saturday` |
| `--theme` | Apply a named theme |
| `--page-size`, `--orientation` | Override the document's page setup |
| `--grayscale` | Convert every colour to grey for printing |
| `--font-dir` | Load additional fonts, shadowing the built-ins |
| `--dump-layout` | Print the computed rectangle tree instead of a PDF |
| `--debug-layout` | Draw every box's rectangles over the artwork |
| `--allow-overflow` | Downgrade overflow errors to warnings |
| `--allow-undefined` | Render undefined variables as empty instead of failing |
| `--unsafe-env` | Let a document read any environment variable (see DESIGN.md D11) |
| `--no-compress` | Leave the PDF uncompressed and readable |
| `--strict` | Treat warnings as failures |
| `--open` | Open the PDF when it is written |

Global: `--no-color`, `-q/--quiet`, `-v/--verbose`.

## Environment variables

Environment access is **declared, never ambient** — a `.pulp` file is a document you might
receive from someone else, and ambient expansion would make
`text: {env.AWS_SECRET_ACCESS_KEY}` an exfiltration primitive in a shared template.

| Variable | Meaning |
|---|---|
| `TKB_VAR_<NAME>` | Supplies a variable the document declared in its `vars` block |
| `SOURCE_DATE_EPOCH` | Fixes the PDF timestamp for reproducible builds |
| `NO_COLOR`, `CI`, `GITHUB_ACTIONS` | Force plain, machine-readable output |

A `vars` entry **with** a value is a default the document supplies. A `vars` entry
**without** one is a required parameter: it fills from `--var` or `TKB_VAR_<NAME>`, is an
error if neither supplies it, and permits `{env.<NAME>}` for that one name.

```pulp
vars
  owner: Jeff            # a default the document supplies
  client:                # required — pass --var client=… or set TKB_VAR_CLIENT
```

## Output and exit codes

**stdout is the artifact or the requested data; stderr is everything human** — including
the summary box, even on a terminal. That is what lets `treekillbot build doc.pulp -o - >
out.pdf` send a PDF down the pipe and still show you what it made.

When stdout is not a terminal, there is no colour and no box: one summary line on stderr,
and diagnostics in gcc format (`weekly.pulp:34:5: warning: W021 …`) that editors and CI
parse for free. `CI=true` forces this even with a terminal attached.

| Code | Meaning |
|---|---|
| `0` | Success |
| `1` | Runtime error — the tool failed |
| `2` | Usage error — bad flags |
| `3` | Source error — the document is wrong |
| `4` | Warnings, under `--strict` |

3 and 4 deviate from the usual 0/1/2 deliberately: a build tool's callers ask exactly one
question — *is my input wrong, or is the tool broken?* — and collapsing those into `1`
breaks both CI and editor integration.

## Development

| Task | What it does |
|---|---|
| `mise run setup` | Zero → runnable |
| `mise run dev` | Run it |
| `mise run test` | Full suite with `-race` |
| `mise run lint` | Lint + vet + format check. Never writes |
| `mise run fmt` | The mutating counterpart |
| `mise run check` | `lint` + `test` — the gate before calling work done |
| `mise run build` | Build a stamped binary |
| `mise run snapshot` | A local GoReleaser snapshot |
| `mise run release` | Interactive tag bump; CI does the rest |

[DESIGN.md](DESIGN.md) is the architecture and, more usefully, the numbered decisions with
the reasoning behind them — why geometry is integer ticks, why overflow is an error, why
themes are written in Pulp, and what the PDF backend cannot do.

## Why the name

It turns text into paper. `.pulp` is what you feed it.

## Licence

MIT. © 2026 Jeff Clement. Bundled IBM Plex fonts are OFL 1.1; see
`internal/fonts/assets/OFL.txt`.
