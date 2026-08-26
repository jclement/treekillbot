# treekillbot — design

`treekillbot` compiles **Pulp**, a small indentation-based DSL, into pixel-perfect
print-ready PDFs. It exists to make dense paper forms: weekly planner spreads, day pages,
Cornell notes, dot-grid notebooks, ruled TODO panels.

It is a single static Go binary with no external tools, no runtime font dependencies and
no network access.

---

## 1. The shape of the thing

```
 .pulp source
     │
     │  internal/pulp        lex → parse → tree, every token carrying a byte span
     ▼
   AST (name, argument, children, spans)
     │
     │  internal/schema      resolve names against the element/property table
     │  internal/vars        variables, dates, interpolation
     │  internal/themes      a theme is a .pulptheme, compiled by the same code
     │  internal/compile     the cascade; loops expand; `when` prunes
     ▼
   Layout tree (typed, fully resolved, no strings left to interpret)
     │
     │  internal/layout      Measure (bottom-up) → Arrange (top-down), integer ticks
     ▼
   Frame tree (every node has an exact Rect)
     │
     │  internal/draw        paint onto a Canvas
     │  internal/decor       the line decorations, drawn into a content rect
     ▼
   Canvas implementations, all driven by the SAME painting code:
       internal/pdfout       → the PDF                  (signintech/gopdf)
       internal/svgout       → the browser preview      (`treekillbot edit`)
       internal/render.Ops   → recorded ops             (structural golden files)

 internal/pipeline  runs the stages;  internal/ui  renders diagnostics;
 internal/editor    serves the side-by-side editor;  cmd/treekillbot  is the CLI.
```

The stages are one-way and each one's output is a value, not a mutation of the previous
stage's. That is what makes `--dump-layout` possible without a PDF writer, and what lets the
layout engine be tested with no fonts and no document.

**The Canvas seam is what makes the browser preview faithful.** The PDF writer and the SVG
writer receive the identical operation stream over the identical computed rectangles; only
the surface differs. A preview that re-derived the layout from CSS would be wrong in a
hundred small ways, and every one of them would be invisible until something was printed.

---

## 2. Key decisions

Numbered, because the reasoning matters more than the choice and is the thing that is
expensive to reconstruct later.

### D1 — All geometry is integer ticks of 1/16pt, never float64

`internal/geom.Tick` is an `int64` count of 1/16 of a PDF point.

"Pixel perfect" is only meaningful if `sum(children) == parent` is *guaranteed*, not
merely observed on the inputs that were tried. In integers it is an identity. In floats
it is a coincidence that holds until a page size changes.

1/16pt was chosen because it is exactly representable in binary (2⁻⁴) and in four decimal
places (0.0625). At 600dpi a tick is ~0.52 device pixels — finer than any printer resolves,
coarse enough that forty stacked rows cannot drift.

Floats appear in exactly two places: the parser boundary (`0.5in` → ticks) and the PDF
writer boundary (ticks → points). Nowhere in between.

**Correction, found by building it:** gopdf writes every coordinate with `%0.2f`, so the
emitted grid is 0.01pt, not the tick's 0.0625pt, and a tick value does *not* round-trip
through the file. This does not weaken the guarantee that matters. Alignment is a claim
about *relationships*, and equal computed coordinates emit as equal strings — two panels
that share an edge still share it exactly, because both sides round the same number the
same way. What is lost is absolute precision: any single coordinate may sit up to 0.005pt
from its computed value, which is 0.03 device pixels at 600dpi.

The engine keeps its own arithmetic exact regardless, so this is a property of the current
backend rather than of the design. If a future backend emits full precision, nothing above
`internal/pdfout` changes.

### D2 — One divider in the whole engine: `DistributeTicks`

Every split of space — column widths, `fill` heights, ruled line spacing, justified word
gaps — goes through `geom.DistributeTicks(total, weights)`, which uses largest-remainder
(Hamilton) apportionment and **guarantees the parts sum to the total exactly**. Ties break
by lower index, so output depends only on inputs — never on map order or float rounding.

This is why `30% + 70%` lands on the content width to the tick, and why three `fill`
siblings in an odd space produce `534/533/533` rather than a 0.0003pt crack.

### D3 — Top-left origin, y-down, flipped exactly once

The layout engine works in top-left/y-down coordinates. `Rect.ToPDF(pageH)` is the only
y-flip in the codebase.

Reasons: every distribution loop reads `y += h` rather than the sign-inverted form where
off-by-one-border bugs breed; font metrics are naturally downward distances; and
`--dump-layout` output agrees with how a human describes a page. A global
`1 0 0 -1 0 H cm` transform was rejected — it flips glyphs too and confuses every PDF
tool.

### D4 — Border-box sizing, always; two stroke alignment rules

`height: 100pt` on a panel means the **border box** is 100pt. Border and padding eat into
content; they never inflate the box. Only one sizing mode is offered, because on a form
the author is drawing a box of a known size and "adding a border made the cell 102pt" is
simply wrong here.

PDF centres strokes on their path, which is where every half-point bug lives. Settled
once, and every geometry function names which rule it follows:

- **Rule A — box borders are edge-aligned.** A border of width `w` on `(x,y,W,H)` strokes
  along `(x+w/2, y+w/2, W-w, H-w)`, so the stroke's *outer* edge lands on the declared
  rect and the box still measures exactly `W×H`.
- **Rule B — line decorations are centre-aligned.** A rule at `y` covers
  `[y-w/2, y+w/2]`. A writing rule *is* the line; changing its weight must not move it.

Two boxes that touch exactly would each stroke the edge they share, making it twice as heavy
as the lines around it. **Shared edges collapse**: a box does not stroke its left or top edge
when another box with an identical pen already strokes that exact line. Whoever is above or
to the left keeps it, so precisely one stroke survives. `border-collapse: false` opts out
where a deliberate double rule is wanted.

The rule is stated on the *edges*, not on the tree, and that is the part worth remembering.
An earlier version of this document said the container should draw the lattice while its
children drew nothing — but the boxes whose borders touch are frequently not siblings. A row
of columns each holding one bordered panel has the panels as grandchildren of the row, and
their edges coincide without their parents' doing so. Comparing edges works at any nesting
depth, and it is only safe to compare them for exact equality because the engine works in
integer ticks (D1); with float geometry the rule would fire almost at random.

### D5 — Every line is a node

Pulp does not distinguish properties from elements *syntactically*. A line is
`name [argument]` plus an optional indented block. `align: right` is a node named `align`;
`panel "Notes"` is a node named `panel`. The **schema** decides which is which, after
parsing.

This is what makes the original sketch parse verbatim: repeated sibling `section` blocks
are trivially legal, and `panel: "Notes"` carrying both a value and a child block is
unremarkable. It also means a property can gain children later without a grammar change,
and that unknown-name errors resolve against a known set with a known parent — which is
what makes "did you mean `line-style`?" possible.

**Rejected: YAML.** The sketch cannot be YAML — repeated sibling keys are not
representable. YAML would not encode the sketch, it would encode a different, noisier
document, with a `-` and a naming level of nesting taxed on every edit. And the errors
that matter here are semantic (unknown property, missing unit, bad enum), so the schema
layer is hand-written either way; yaml.v3 gives the position of a node, not of the
offending token *inside* a scalar, so `height: 200` could never get a caret under `200`.
The AST is kept as a plain `(name, arg, children, spans)` tree with no Pulp types leaking
downstream, so a `--from-yaml` adapter stays a ~50-line option that we are not building.

### D6 — `line-height` is leading; `line-pitch` is the decoration repeat

CSS's `line-height` keeps its CSS meaning. The repeat distance of whatever `line-style`
draws — ruled lines, dot rows, graph squares, checkbox rows, time slots — is
**`line-pitch`**. A Cornell panel needs both at once, so they cannot share a name.
`dot-pitch` defaults to `line-pitch`, so a square dot grid is one number.

### D7 — The PDF library is `signintech/gopdf`

MIT, actively maintained, and it was verified by building a probe rather than from
memory. Two properties decided it:

- **Byte-identical output across runs.** `fpdf` stamps a wall-clock `/CreationDate` and
  hashes differently every run; gopdf does not. Golden-file tests need this.
- **It returns real `error`s.** `fpdf` latches a sticky internal error and silently no-ops
  everything after it, so a typo'd font name yields a blank page with no signal.

Notable correction to received wisdom: **`github.com/go-pdf/fpdf` is archived** —
development moved to `codeberg.org/go-pdf/fpdf`. The runner-up is that Codeberg module;
what would make us switch is wanting true round line caps, which gopdf lacks.

Known gotchas, all handled at the `internal/pdfout` boundary: `go:embed` cannot cross
`../` (fonts get their own package); gopdf has no `SetLineCap`, so zero-length lines draw
nothing and dots must be filled shapes; missing glyphs are silently replaced with a space
unless `OnGlyphNotFound` is overridden; dash state is sticky and must be reset.

### D8 — Fonts are static instances, embedded, IBM Plex

Variable fonts are useless here: neither candidate library reads `fvar`, so a `[wght]`
variable font yields exactly one master and no bold — you would ship separate files
anyway, plus dead variation tables. IBM Plex (OFL 1.1) gives a mono, a humanist sans and
a serif designed together with shared metrics.

All measurement happens in **integer font design units**, converting to ticks in a single
expression at the end. Summing float advances is how a table's last column ends up a
thousandth of a point off on one machine and not another.

Letter-spacing is applied **between** glyphs (n−1 times), not after every glyph as CSS
does, so centred tracked panel titles are optically centred rather than half a space off.

### D9 — Overflow is an error by default

A dense spread that does not fit is a document bug, and the most valuable thing the tool
can do is say *"day cell #6 needs 104.25pt, has 96.00pt, short by 8.25pt"* rather than
silently squash Sunday. Screen engines default to overflow because they can scroll; paper
cannot.

Default `error` for Page, Section, Column, Grid and Panel. **Text** defaults to `shrink`
(quarter-point steps to a 6pt floor) then `clip` with a warning — text is where the
content, not the layout, is at fault. Decorations never overflow; they floor their counts.
`--allow-overflow` downgrades globally, and CI does not use it.

### D10 — Colour is authored and emitted in the same space

`gray(0.85)` becomes PDF `DeviceGray 0.85`, not an RGB triple a RIP must convert back. The
tint written is the tint the printer receives, identical with or without `--grayscale`.
The shipped themes use `gray()` and nothing else.

This is a printing tool, so the defaults are chosen for paper, not for a monitor: a 10%
grey halftones and dot-gains two to three steps darker on plain 20lb stock than it looks
on screen. **The `default` theme ships with no panel fills at all** — separation comes
from rules and whitespace. Fills darker than `gray(0.80)` under a writing area warn, and
hairlines below 0.25pt are refused because engines snap them to one device pixel, which
means their weight is a property of the printer rather than of the document.

### D11 — Environment variables are declared, never ambient

`{env.HOME}` requires a `vars` declaration; an unqualified `{USER}` never silently picks
up a shell variable.

A `.pulp` file is a document you might *receive*. Ambient expansion makes
`text: "${AWS_SECRET_ACCESS_KEY}"` an exfiltration primitive in a shared planner template.
It also destroys reproducibility and discoverability. The ergonomic path is preserved: a
`vars` entry declared without a value fills from `TKB_VAR_<NAME>`, and a declared-but-
missing variable is a hard error rather than a silent blank on a printed form.

### D12 — Dates are strftime, not Go layouts

Go's reference-time layout is disqualified as the primary format language because
`go:"Q1 2006 review"` renders **"Q8 2026 review"** — the `1` is the month, so a heading
about the first quarter silently becomes a heading about August. A format language in which
ordinary English is a live grenade cannot be the default on a tool whose entire job is
putting dates on paper. strftime wins because every directive starts with `%`, so literals
are always literal, and `date(1)` has already taught everyone the syntax. Named formats
(`iso`, `short`, `long`) cover the common cases; Go layouts remain available when
explicitly tagged `go:"…"`.

Everything date-shaped derives from a single anchor, which `--date` reseeds wholesale —
one flag makes the whole document reproducible and prints next week's page today. Both
`week.number` and `week.iso-year` are exposed, because 2027-01-01 falls in ISO week 53 of
2026 and a header reading "Week 53, 2027" is the classic bug you can only avoid if the
tool hands you both.

Locale is explicitly out of scope: Go has no CLDR, and half-done locale is worse than
none. `month-names:` and `day-names:` directives make a French planner seven lines.

### D13 — Themes are Pulp files

A theme is a `.pulptheme` file, embedded with `go:embed`. One language for the whole
toolchain: `fmt` and `check` work on themes, `themes --show mono` gives an editable starting
point, and there is no second colour/length parser to drift out of sync. User themes in
`~/.config/treekillbot/themes/` deliberately shadow built-ins, `default` included.

A theme carries the same three shapes a document does — a global `defaults` block,
per-element `defaults <element>` blocks, and named `style` bundles — so it is written in
exactly the language it themes, and it reaches the cascade through the same code a document
does rather than a parallel reader.

Two rules bound what a theme may say, both found by trying it:

1. **Page setup is never a theme's business.** A theme that changed the paper would silently
   reflow every document it touched.
2. **Box metrics and decoration switches are refused in the global block and welcome in a
   per-element one.** `border-width` in a bare `defaults` frames every section and column on
   the page; `line-style: dotted` there rules the header band and the page itself. In
   `defaults panel` both are exactly what a theme is for. Neither failure names itself, so
   the global case is refused with the per-element form given as the fix.

**Every shipped theme defines the same five semantic slots** in a `vars` block — `paper`,
`ink`, `muted`, `rule`, `accent` — which a document references as `{ink}` and so on. Without
them a document that names any colour cannot survive a theme swap: it keeps its dark ink on
`midnight`'s dark sheet and becomes unreadable. Theme constants sit just above the built-ins
in the variable ladder, so a document's own `vars` block, `--vars-file` and `--var` all still
outrank them.

A `style` bundle in a theme is exempt from rule 2: it applies only when a document names it,
and being ruled is what `style: ruled` asked for.

A theme that resolves to no properties at all is an error rather than a silent no-op —
`themes --show mono > mono.pulptheme` in the document's own directory truncates the target
before the command runs, and discovery would then prefer the empty file forever after.

### D14 — The live preview is SVG in a browser, not a native window

`treekillbot edit` serves a local page: the document on the left, a live preview on the
right. The preview is drawn by `internal/svgout`, another `render.Canvas`, so it receives
the same operations over the same computed rectangles as the PDF. It is a *recreation*,
not an approximation, and if the two ever disagree that is a bug in one Canvas rather than
drift between two designs.

**SVG rather than HTML boxes**, because SVG has PDF's primitives — arbitrary paths,
baseline-positioned text, clipping — so the op stream maps one to one. A CSS-box preview
would have to re-derive the layout from properties. Text runs carry `textLength` with the
width our own engine measured, so any disagreement between the browser's shaper and ours is
absorbed into the gaps instead of moving the end of the line.

**A browser rather than Fyne or Gio.** Both are good toolkits and both were considered.
Three things ruled them out here: each would need a *third* Canvas implementation with its
own text shaping, which is exactly the drift this design avoids; both need CGO and OpenGL on
desktop, which would end `CGO_ENABLED=0`, the single static binary, and clean cross-builds;
and a code editor needs selection, undo, IME and accessibility, all of which a `<textarea>`
provides and neither toolkit does. The browser version also works over an SSH port-forward
and on a headless machine. If a chrome-less app window is ever wanted, the cheap version is
a system WebView behind a build tag — the same SVG, the same code.

**No bundled editor library.** Syntax highlighting is a ~100-line Pulp tokenizer that
mirrors `internal/pulp`'s scanner, which keeps the release pipeline a single `go build`
with no npm step — the same promise the binary makes about external tools — and highlights
correctly the two things a generic mode gets wrong: `#ddd` is a colour, and a line's first
word is a name whether or not a colon follows.

**The server binds to loopback and requires a per-session token, and checks `Origin`.**
That is not theatre. A page on the open internet can reach `127.0.0.1`, and without it any
site the user happened to have open could read and overwrite the file being edited. The
`Origin` check is what a DNS-rebinding attack cannot forge.

---

## 3. Cascade and precedence

Property value, lowest priority to highest:

1. Built-in default shipped with the binary
2. Theme
3. `defaults` block — global, then progressively more nested; nearest wins
4. `defaults <type>` block — same nesting rule
5. **Inherited** from the nearest ancestor that set it *explicitly* (inheritable
   properties only)
6. `style: a b` bundles, in listed order
7. Direct property on the node

**Inheritance sits above `defaults`, which is where we part company with CSS.**
In CSS a universal rule beats inheritance, so `* { font-size: 8pt }` silently defeats a
size set on an ancestor — a gotcha people have been complaining about for twenty-five
years. Here a `defaults` block is a *baseline*, while a property written on an ancestor is
a deliberate statement about that whole subtree, so the ancestor wins.

The word *explicitly* is what keeps this composable. Every `Props` carries a second bitmask
recording whether each value was stated by the author (directly or via a `style`) or merely
arrived from a `defaults` block. Only explicit values propagate down. So a nested
`defaults panel: …` is not defeated by a value its ancestors happened to pick up from a
broader `defaults` block, and the nesting rule still works — but a `font-size` you actually
wrote on a section reaches the panels inside it, which is what everyone expects.

**What inherits:** if it describes ink on a glyph or on a ruled line, it inherits
(`font`, `font-size`, `color`, `align`, `valign`, `line-*`, `tracking`). If it describes
the box, it does not (`width`, `height`, `padding`, `margin`, `background`, `border`).
This is CSS's model, gotcha included; `check --explain-property` prints the full cascade
for one property on one node.

Variable resolution, highest priority first: loop bindings → `let` → `--var` →
`--vars-file` → `vars` block → environment (declared only) → theme constants → built-ins.
`--date` sits outside the ladder and rebinds the anchor.

---

## 4. Determinism

There is a golden test asserting byte-identical output. Every source of nondeterminism is
eliminated deliberately:

| Source | Handling |
|---|---|
| Float accumulation | Eliminated by D1/D2 — integer ticks, exact apportionment |
| Map iteration order | No `range` over a map in `layout`, `render` or `pdfout`; ordered containers instead |
| `/CreationDate`, `/ModDate` | Fixed under `--deterministic`; derived from `--date` or `SOURCE_DATE_EPOCH` |
| `/ID` | `SHA-256(body)`, never random |
| Font subset glyph IDs | Assigned by first use in a canonical traversal |
| PDF object numbering | Emitted in a fixed traversal order |
| Compression level | Pinned; the primary golden hash is taken with `--no-compress` so a Go flate change fails one clearly-labelled test rather than the suite |

Test tiers, in the order they are useful:

1. **`--dump-layout` rect tree** — the primary golden. A diff says *what moved and by how
   much*, in the vocabulary of the design, and does not churn when compression or object
   order changes. Built before the PDF writer.
2. **`--emit-ops`** — structural JSON-lines drawing ops. The tier a human reviews.
3. **Byte hash** under `--deterministic`.
4. **Diagnostics golden** — overflow and ink warnings are behaviour, and regress silently
   otherwise.

A synthetic test font (1000 upem, every advance 500, ascent 800, descent 200) turns most
text assertions into hand-checkable arithmetic.

---

## 5. CLI contract

`build` (the default verb), `new`, `templates`, `themes`, `fmt`, `check`, `docs`,
`version`, `update`, `completion`.

**stdout is the artifact or the requested data; stderr is everything human** — including
the pretty summary box, even on a TTY. That single rule is what lets
`treekillbot build doc.pulp -o - > out.pdf` still show you the summary.

Exit codes: `0` success, `1` runtime error, `2` usage error, **`3` source error**,
**`4` warnings under `--strict`**. 3 and 4 deviate from the house 0/1/2 deliberately: a
build tool's callers ask exactly one question — *is my input wrong, or is the tool
broken?* — and collapsing those into `1` breaks both CI and editor integration.

Not a TTY means no ANSI, no box, no emoji, ever partial: one summary line on stderr and
diagnostics in **gcc format** (`weekly.pulp:34:5: warning: W021 …`) so editors and CI
parse them for free, plus `::warning file=…` under `GITHUB_ACTIONS`. `CI=true` forces
non-TTY even with a terminal attached.

---

## 6. Declared but not implemented

These have a place in the schema and no effect on the output. They are listed rather than
removed because each is a decision already made and worth keeping, but a property that
silently does nothing is worse than one that does not exist — so until they are wired up,
this is where they are recorded.

| Name | State |
|---|---|
| `numeric-style` | Parsed, never read. Tabular figures are whatever the face defaults to. |
| `opacity` | Parsed, never read. Use an alpha channel on the colour instead. |
| `bleed` | Parsed, never read. No bleed area is added to the media box. |
| `image` | Parses and measures as zero; nothing is painted. |
| `include` | Validates; the file is never spliced in. |
| `baseline-on-rule` | `Decoration.Baselines()` is computed correctly and the painter never asks for it, so text in a ruled box flows normally instead of snapping to the rules. |
| `grid` element | Accepted, and lays out exactly like `box`. The repeating lattice its documentation promises is not there; use `repeat` or `for`. |

One smaller one: a ternary condition can only compare a path against a literal, so
`{a.n == b.n ? … }` compares against the *characters* of `b.n` and is always false, with no
diagnostic.

---

## 7. Known limitations

- No arithmetic in the DSL. No `calc()`, no `100% - 1in`. `fill`, `%`, `auto`, `gap` and
  `padding` cover page-size-independent layout, and every expression language starts with
  "just subtraction".
- No `if`/`else` blocks. `when:` drops a subtree and a value-level ternary covers the
  rest. The cap is the feature: `if` leads to `elif`, then comparisons, then a bad Jinja
  inside a PDF generator.
- Greedy line breaking, not Knuth–Plass. An algorithm whose output moves when you tweak a
  penalty constant is a poor basis for golden files.
- No RTL or complex script shaping, and **combining marks do not compose**: text is measured
  and drawn glyph by glyph at its advance width, with no mark positioning, so U+0301 lands
  beside its base letter rather than over it. Use precomposed characters. The stress-test
  sample states all of this in print, and lists the codepoints no embedded face covers.
- No ICC profiles, separations or overprint. `cmyk()` passes through untouched.
- English only. See D12.
