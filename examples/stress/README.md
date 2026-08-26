# The stress test

A numbered set of sheets, one concern each. `examples/stress-test.pulp` is the
printable cover and index; this file is the same index for a reader at a
terminal, plus the things that are true about the tool and easier to write down
than to draw.

Pulp has no page break — one document is one page — so the "comprehensive
stress test" is a set rather than a file. Every sheet is also a reference you
would keep: a ruler, a calibration ramp, a hairline gauge, a font-coverage
table.

## Building the set

```sh
mkdir -p out
for f in examples/stress/*.pulp; do
  treekillbot build "$f" -o "out/$(basename "${f%.pulp}").pdf" --date 2026-09-09
done
```

Every sheet builds with **zero diagnostics** except `09-overflow.pulp`, which
is the one that must not:

```sh
treekillbot build examples/stress/09-overflow.pulp -o out/09.pdf --allow-overflow
```

Use a fixed `--date`. Only `08b-dates.pulp` and the cover show a date, but a
fixed anchor is what makes two builds comparable.

## The sheets

| Sheet | What it proves |
|---|---|
| `01-units.pulp` | Eight 50mm bars — `50mm`, `5cm`, `141.7323pt`, `1.9685in`, `11.811pc`, `188.976px`, `50%` of 100mm, one of two `fill`s of 100mm — must end on **one line**. All eight are 2268 ticks. A ruler in mm and in eighths of an inch to check the print scale, six more bars proving the vertical axis converts the same way, and a staircase of eight bars one tick (1/16pt) apart. |
| `02-colours.pulp` | A 21-step `gray()` ramp with the value under every patch: a printer calibration strip. The same ramp again at 3mm and again as 0.4pt rules, because a tint behaves differently at those three sizes. Then every colour form: `#rgb`, `#rgba`, `#rrggbb`, `#rrggbbaa`, `rgb()` in 0–255, 0–1 and percentage forms, `rgba()`, `gray()`, `gray()` with alpha, `grey()`, `cmyk()`, a CSS name, and `transparent`. |
| `03-hairlines.pulp` | 0.1pt → 3pt with the value beside each stroke, horizontal and vertical, in `gray(0)` and `gray(0.6)`; then the same ruled panel at five `line-width`s. Everything below 0.25pt is drawn AT 0.25pt — the floor is silent, and the first three rows being identical is correct. |
| `04-line-decorations.pulp` | Landscape. All seven `line-style` values — `none`, `ruled`, `dotted`, `graph`, `checkbox`, `cornell`, `time-grid` — at 4mm, 6mm and 9mm. `time-grid` visibly ignores `line-pitch`, because it partitions the height it is given rather than repeating a pitch. |
| `04b-line-tuning.pulp` | `line-distribute` (center/start/end/grow), `line-partial`, `lines`, `line-inset`, `baseline-on-rule`, `margin-rule` over five different styles, every checkbox knob, and `grid-origin: page` vs `box` in adjacent cells so the cross-panel lattice is visible. |
| `05-sizing.pulp` | `fill`, `fill(n)`, percentages summing exactly to the content width, mixed fixed/percent/fill, 1% and 99%, and a height band with fixed, percentage, `auto` and two `fill`s. Also shows that `min-height`/`max-height` clamp `auto` and `fill` children and are **ignored on a fixed height**. |
| `05b-boxes.pulp` | Five levels of 80% nesting, three `gap` values, border-box sizing (padding and border eat in, margin adds), padding shorthand arity and per-side overrides, `valign` on a container, and `spacer fill`. |
| `06-typography.pulp` | Three families × four faces, a 5pt–32pt size ramp, `text-transform`, and a `tracking` ramp including a negative value. |
| `06b-text-flow.pulp` | `align` including `justify`, `valign` including `baseline`, `line-height`, `wrap: false`, `numeric-style`, and text inside a ruled box (which does **not** snap to the rules). |
| `07-borders.pulp` | Every `border-style` at 0.5pt and 2pt, the `border:` shorthand, per-side widths, `border-width` shorthand arity, a `border-radius` ramp to a stadium, and borders over decorations. |
| `07b-chrome.pulp` | All four `title-style`s, all four `title-position`s, `title-align`, `title-transform`, `title-tracking`, `title-font`, `title-size`, `title-padding` and `title-background`. |
| `08-unicode.pulp` | A coverage matrix for the three embedded faces, verified by building it rather than inferred from Unicode blocks. Also `text-transform` on non-ASCII, and what happens to combining marks. |
| `08b-dates.pulp` | Every date built-in printed with its current value, a month grid from `month.weeks`, and every format vocabulary. Build it once per ISO edge case (below). |
| `09-overflow.pulp` | Deliberately does not fit. Prints the six warnings it expects to provoke, so you can check the tool said what it should. |

## Sheet 08 — what the embedded fonts actually cover

Verified by building one probe per family and reading the CLI's warning, not by
reading the Unicode blocks.

| Set | Mono | Sans | Serif |
|---|---|---|---|
| Basic Latin, Latin-1, Latin Extended-A/B | yes | yes | yes |
| Cyrillic | yes | yes | yes |
| Greek | **no** | yes | **no** |
| Typographic punctuation | yes¹ | yes | yes |
| Mathematical operators | yes | yes | yes |
| Currency (incl. `₹ ₽ ₩ ¤`) | yes | yes | yes |
| Vulgar fractions | yes | yes | yes |
| Arrows `← → ↑ ↓ ↔ ↕` | yes | yes | yes |
| Box drawing `─ │ ┌ ┼ ═ ║` | yes | **no** | **no** |
| `✓` U+2713 | yes | yes | yes |
| `☐ ☑ ✗ ✔ ★ ○ ● ■ ◆` | no | no | no |
| Double arrows `⇐ ⇒ ⇧`, `⌘ ⌥ ⌫` | no | no | no |
| CJK, Hangul, kana | no | no | no |
| Emoji | no | no | no |
| Hebrew, Arabic | no | no | no |

¹ except U+2011 NON-BREAKING HYPHEN, which Mono lacks and Sans and Serif have.
U+03C0 GREEK SMALL LETTER PI is the one Greek letter Mono does have.

**RTL and complex-script shaping do not exist here.** Text is measured and drawn
left to right by advance width (DESIGN.md §6). Hebrew and Arabic would come out
in logical order, unshaped and unjoined, even with a font that had them.
**Combining marks do not compose either** — U+0301 is drawn beside its base
letter, not over it, so use precomposed characters.

A glyph no face can draw is replaced with a space by gopdf and reported once per
build:

```
treekillbot: warning: some characters are not in any embedded font and were
dropped: 'Α' (U+0391), 'Β' (U+0392), 'Γ' (U+0393), 'Δ' (U+0394), 'α' (U+03B1),
'β' (U+03B2), 'γ' (U+03B3), 'δ' (U+03B4)
```

The first eight are listed and the rest counted (`… and 47 more`). It is a
warning, not an error, so `--strict` is what makes it fail a build.

## Sheet 08b — the date edge cases

Build the same sheet once per date and read the printed values:

| `--date` | What must appear |
|---|---|
| `2026-12-31` | `week.iso` = `2026-W53`, `week.number` = 53, `year.number` = 2026 |
| `2027-01-01` | `week.iso` = **`2026-W53`** while `today` is 2027-01-01 — the classic off-by-a-year header bug |
| `2021-01-01` | `week.iso` = `2020-W53` |
| `2024-02-29` | `year.leap` = true, `day-of-year` = 60, `month.end` = 2024-02-29 |
| `2027-02-15` | `month.end` = **2027-02-28** — the month clamp, not 3 March |
| `2024-02-15` | `month.end` = 2024-02-29, the same clamp in a leap year |
| `--week-start sunday` | the day list rotates; `week.number` does **not** move |

`--date 2023-02-29` is rejected by the CLI before anything is built.

## Sheet 09 — the warnings it expects

Built with `--allow-overflow`, six diagnostics on stderr in gcc format, sorted
by source position, plus one build-level warning:

```
W031 `width` on a `section` has no effect
W030 `line-height` sets text leading, not the spacing of ruled lines
W010 content does not fit in this section  (needs 120pt, has 82pt, short by 38pt)
W020 text was shrunk to 8.25pt to fit
W020 text was shrunk to 8.50pt to fit
W021 text does not fit and was clipped
warning: some characters are not in any embedded font and were dropped: …
```

Exit codes: `0` clean, `3` source error (which is what the same sheet gives
*without* `--allow-overflow`), `4` warnings under `--strict`.

## Things these sheets found

Written down here because a sheet can only show behaviour, not explain it.

- **`auto-shrink` defaults to `0`, which is off.** DESIGN.md D9 says text
  defaults to shrink-then-clip; the schema's default disables it. With it off,
  `WrapText` returns the unclipped layout, so W021 is unreachable and text
  simply overruns its box in silence. Both W020 and W021 on sheet 09 need an
  explicit `auto-shrink`.
- **`overflow: clip` does not clip text.** The clip is applied to decorations
  only, which is why the `wrap: false` cell on sheet 06b spills past its border.
- **A panel title overlaps its children.** The title band is subtracted from the
  decoration area and not from the content rect the children are arranged in, so
  a `text` child of a titled panel prints on top of the title. `title-position:
  left` does not hold the decoration off either, because only a title *height*
  is subtracted.
- **`em` and `ex` resolve to zero.** The parser accepts them and
  `layout.resolveRelative` is a stub that returns its input unchanged — which is
  0, because the value parser deliberately left the length unresolved. Nothing on
  these sheets uses them.
- **A `for` or `repeat` at the top level of a document is dropped silently**, so
  every loop here is inside a `section` or a `box`.
- **`style: name` is rejected on a leaf** (`text`, `rule`, `spacer`, `image`)
  with `E122 'text' cannot contain 'style'`, so shared text styling on these
  sheets is written out per node.
- **A ternary condition compares a path against a literal only**, so
  `day.month.number == month.number` compares against the eleven characters of
  that path and is always false. That is why the month grid on 08b greys
  weekends rather than the spill days.
- **Single-quoted strings are not raw.** `compile.textContent` interpolates the
  argument before unquoting, so `'{today}'` still expands. Use `{{` and `}}`.
- **`numeric-style`, `opacity`, `bleed` and the `image` element are declared in
  the schema and read by nothing.** `grid` is layout-identical to `box`.
