# CSS and HTML support

This page lists what `htmltopdf` renders, what it approximates and what it
ignores. It is updated by every change that alters CSS or HTML support.

Anything not listed as supported is dropped and reported as a warning in
`Result.Warnings` (`unsupported-property`, `invalid-value`, `unsupported-at-rule`,
`unsupported-selector`, `unsupported-element`); the render never fails because of it.

Status values: **supported**, **approximated** (rendered differently from a
browser, with the difference stated), **ignored** (dropped with a warning),
**never** (not planned).

Output PDFs are not tagged: they have no structure tree and do not conform to PDF/UA
(Chrome's are tagged); text remains extractable through ToUnicode.

## Syntax, cascade and selectors

| Feature | Status | Notes |
|---|---|---|
| Origins | supported | UA sheet, document (`<style>` and `style=""`), then ExtraCSS, which wins over every document declaration, `!important` and `style=""` included. Inside the document, `!important` beats normal, and the style attribute beats the selectors. |
| `inherit`, `initial`, `unset` | supported | `all` is ignored (`unsupported-property`). |
| Custom properties and `var()` | supported | Fallbacks, nesting, cycles (invalid, so the fallback applies); a `var()` that cannot be resolved makes its property `unset`. Substitution is limited by `MaxVarBytes`: past it the property is dropped with `var-budget`. |
| Rules for `::before` and `::after` | ignored | Parsed and matched, not applied until generated content exists. |
| Comments, escapes (`\41`, `\A`), `!important` | supported | Escapes are decoded in identifiers, strings and URLs. |
| Error recovery (CSS Syntax §5) | supported | A malformed rule or declaration is dropped alone, the rest of the sheet is kept; each dropped item is reported as `invalid-value`. A `}` ends an unfinished item inside a block. |
| `@media` | supported | Nested `@media` rules, up to 64 levels. |
| `@page` and its 16 margin at-rules | supported | All 16 margin boxes are parsed; corner and side boxes are ignored with `unsupported-at-rule` when the sheet is loaded. |
| `@charset` | ignored | Only accepted at the very start; the text is always UTF-8. |
| `@import`, `@font-face`, `@supports`, `@keyframes` and any other at-rule | ignored | Skipped with its block, reported as `unsupported-at-rule`. Nothing is ever loaded. |
| Selectors: type, `*`, `#id`, `.class`, `[attr]`, `[attr=value]` and `~= \|= ^= $= *=` with `i`/`s` | supported | Names are matched in lowercase (HTML); no namespaces (`ns\|tag` is dropped). |
| Combinators: descendant, `>`, `+`, `~` | supported | A selector has at most 32 compound selectors of at most 32 simple selectors each. |
| `:root`, `:first-child`, `:last-child`, `:nth-child(an+b\|odd\|even)` | supported | |
| `:hover`, `:focus`, `:active`, `:visited`, `:focus-within`, `:focus-visible`, `:target` | supported | Parsed, never match on paper. |
| `::before`, `::after` (also `:before`, `:after`) | supported | Matched on the originating element; the style stage applies them to the generated box. |
| Other pseudo-classes and pseudo-elements (`:not()`, `:is()`, `:first-of-type`, `::first-line`...) | ignored | The whole selector list is dropped with `unsupported-selector`, as CSS requires for an invalid selector. |
| Specificity and the 5 000 selector budget | supported | Every member of a list counts; beyond 5 000 the render fails with `LimitSelectors`. |
| Media queries: `all`, `print`, `screen`, `not`, `only`, `and`, `,` with `width`, `height` (also `min-`/`max-`) and `orientation` | approximated | The medium is always print, with the page area as viewport. Any other feature, a range (`width >= 600px`) or a malformed query evaluates to false and is reported as `invalid-value`; an unknown media type is valid and simply does not match. |
| Nesting of blocks, functions and parentheses | approximated | Limited to 64 levels in one selector or value; a deeper item is dropped with `invalid-value`. |
| Control characters and invalid UTF-8 in CSS text | approximated | Replaced by U+FFFD. |

## Values and units

| Feature | Status | Notes |
|---|---|---|
| Lengths: `px`, `pt`, `pc`, `in`, `cm`, `mm`, `Q`, `em`, `rem`, `%`, unitless `0` | supported | Absolute units are converted to px; values are clamped to ±1e7 px. |
| Other units (`ex`, `ch`, `vw`, `vh`, `vmin`, `vmax`) and `calc()` | ignored | Declaration dropped with `invalid-value`. |
| Colors: `#rgb`, `#rgba`, `#rrggbb`, `#rrggbbaa`, `rgb()`, `rgba()`, named colors, `transparent` | supported | Both the comma and the space/slash syntaxes of `rgb()`; components are clamped. |
| `currentcolor` | supported | In `color` (inherits), `background-color` and the border colors. |
| `hsl()`, `hwb()`, `lab()`, `color()`, system colors | ignored | Declaration dropped with `invalid-value`. |
| `linear-gradient()` | approximated | Angle (`deg`, `rad`, `grad`, `turn`) or `to <side|corner>`, 2 to 64 color stops with optional percentages; no hints, no lengths as positions. `repeating-` and other gradient types are ignored. |
| Functions with many arguments | approximated | At most 128 arguments or stops are examined; a function with more is rejected. |

## Layout

| Feature | Status | Notes |
|---|---|---|
| `display`: `none`, `block`, `inline`, `list-item`, `table` and the table-* values | supported | `list-item` is a block: markers are not drawn. `inline-block`, `flex`, `grid`: dropped with `invalid-value` until their milestones. |
| `box-sizing`, `margin`, `padding` (with `auto`, `%`, 1 to 4 values), `width`, `max-width` | supported | Negative padding and width are invalid. `height`, `min-*`, `max-height`: `unsupported-property`. |
| `border`, `border-<side>`, `border-width/style/color`: `none`, `hidden`, `solid`; `thin`, `medium`, `thick` | supported | The width is 0 while the style is none or hidden. `dashed` and `dotted` are dropped with `invalid-value` until M4. |
| `border-radius` (1 to 4 values, `/`, `%`) and the four corner longhands | supported | |
| `background`, `background-color`, `background-image` | approximated | A color and/or a `linear-gradient()`; positions, sizes and repeats are not supported. |
| `overflow` | approximated | `hidden` and `clip` clip; `auto`, `scroll` and `visible` do not. |
| `border-collapse`, `border-spacing`, `table-layout` | supported | Fixed layout: `table-layout: auto` is laid out as fixed, without a warning. Column widths come from the cells of the first row. Collapsed borders: the wider wins, the left or top cell on a tie, `hidden` removes the edge; row and group borders are ignored. Separated model: each cell draws its own borders, `border-spacing` applies. |
| Block layout: margin collapsing (siblings, parent and first or last child, empty boxes), auto margins, `%` of the containing block, `box-sizing` | supported | The root element and boxes with `overflow: hidden` keep the margins of their children inside. |
| Fragmentation | approximated | Pages break between blocks and between lines; there is no orphans/widows control and `break-*` is ignored until M2. Margins next to a break are truncated. A border or padding that does not fit a page is sliced over the following pages. |
| `Options.Scale` | supported | The page area is laid out at 1/Scale of its size and drawn at Scale; margin boxes are not scaled. |
| `box-shadow`, `position`, `float`, `z-index`, `transform`, `opacity`, `flex*`, `grid*` | ignored | `unsupported-property`. |

## Text and fonts

| Feature | Status | Notes |
|---|---|---|
| `font-family` (names, strings, generic families), `font-size` (lengths, `%`, keywords, `smaller`, `larger`), `font-weight` (1 to 1000, `bold`, `bolder`, `lighter`), `font-style`, `line-height`, `color`, `letter-spacing`, `text-align`, `vertical-align` (`baseline`, `top`, `middle`, `bottom`) | supported | At most 32 font families are kept; font sizes are clamped to 10 000 px. `text-align: justify` is dropped with `invalid-value`. `font` (shorthand): unsupported in M1 (`unsupported-property`): the declaration is dropped, so `font-family`, `font-size`, `font-weight` and `line-height` fall back to their inherited or initial values; supported from M2. |
| `white-space`: `normal`, `pre`; `overflow-wrap` and `word-wrap`: `normal`, `break-word`, `anywhere`; `word-break`: `normal`, `break-all` | supported | `anywhere` places like `break-word`; `break-all` adds a break opportunity between letters, digits and the symbols Chrome treats like them (`_`, `#`, `@`...), after `. , : ; / -` and around `(` and `)`; never inside a grapheme (marks, joiners, Hangul jamo, Indic conjuncts), not under `white-space: pre`, and not between two adjacent inline boxes (`a<b>b</b>`). Their min-content effect (one grapheme) is not implemented: M1 has no min-content consumer (fixed table layout), it comes with auto table layout in M3. `nowrap`, `pre-wrap`, `pre-line` and the other `word-break` values are dropped with `invalid-value` until M2. |
| `text-decoration` (`underline`, `line-through`, `none`) | supported | Not inherited; the box stage propagates it. `overline` is dropped with `invalid-value`. |
| `list-style`, `list-style-type` | approximated | Only `none` is kept; markers are never drawn. |
| UA sheet | supported | Chrome's values for the HTML subset (headings, lists, tables, `a[href]` blue and underlined, `pre`, `code`...). `img`, `object`, `iframe`, `video`, `canvas`, `input`... are `display: none` with `unsupported-element`. |
| Mark positioning (GPOS offsets) | Not supported (M1-M4) | Shaped glyphs keep their advance but not their X/Y offsets, so combining marks are not repositioned: precomposed characters render correctly, stacked or detached marks may overlap their base. |

## Paged media

| Feature | Status | Notes |
|---|---|---|
| `@page` `size`: `auto`, named sizes (`A3`, `A4`, `A5`, `B4`, `B5`, `letter`, `legal`, `ledger`), `landscape`, `portrait`, one or two lengths | supported | Each side is clamped to 1 in to 200 in (`clamped`). |
| `@page` margins: `margin` and `margin-*` | supported | Margins that leave less than 1 in of page area are scaled down together (`clamped`). Other descriptors: `unsupported-property`. |
| `@page` selectors (`:first`, `:left`, named pages) | ignored | The rule is dropped with `unsupported-at-rule` until M2/M3. |
| Margin boxes `@top-left|center|right`, `@bottom-left|center|right` | supported | Font, color, background, padding, `text-align`, `vertical-align`, `content`. Their text is aligned and vertically centered by default. |
| Margin text containing `counter()` | approximation | The text is not wrapped: it stays on one line, clipped to its box, starting at the box start when it overflows. A text without counter wraps in its box. No warning. |
| `content` of a margin box: strings, `counter(page)`, `counter(pages)`, `none`, `normal` | supported | Strings go through the text sanitation of design section 8.4. |

## HTML elements and attributes

| Feature | Status | Notes |
|---|---|---|
| `<a href>` | supported | Only `http`, `https` and `mailto` targets are drawn as links; the scheme is lowercased, spaces and non-ASCII are percent-encoded, a target over 2 KiB or with a control character inside is dropped. Any other scheme, a relative target and `javascript:` are reported as `blocked-link`. `#fragment` is ignored until internal links arrive. |
| `<br>`, white space | supported | Collapsible white space collapses across inline boxes and is dropped at the start and end of a line; `white-space: pre` keeps spaces and turns newlines into line breaks. In preserved text a tab moves to the next multiple of 8 characters (counted in characters, not in widths). |
| Block elements inside inline elements | supported | The inline box is split around the block, as in CSS 2.1 §9.2.1.1; loose inline content next to blocks gets an anonymous block. |
| `<svg>` and `<math>` | ignored | The subtree is skipped and reported as `unsupported-element`. |
| `<table>` and its parts | approximated | Missing rows, groups and cells are added as anonymous boxes; `colspan` is clamped to [1, 1000] (`clamped`), cells past column 1000 are dropped and a colspan that reaches past it is shortened (both `clamped`), `rowspan` is rendered as 1 (`clamped`). A row that does not fit breaks between the line boxes of its cells (all cells continue on the next page); a row of single lines moves whole; a row taller than an empty page is sliced, each line staying in the slice that holds its baseline (`clamped`, only for the sliced case). The first `thead` repeats on each continuation page. `caption` is drawn above the table; `tfoot` comes after the body and is not repeated. `vertical-align` on cells: top, middle, bottom, baseline. |
| `col` and `colgroup` widths | supported | Fixed layout: the width (px, mm or %) of a `col` wins over the width of the first-row cells; `span` is honoured (clamped to 1,000). A column no cell uses still takes its width. |

## Limits worth knowing

| Limit | Notes |
|---|---|
| Content of one page | 16 MiB of drawing operators per page. A single line whose drawing operators exceed 16 MiB fails with `LimitOutputBytes` (about 1.2 M glyphs with letter-spacing; plain text up to `MaxGlyphs` fits). |
| Gradients | At most 64 stops per gradient and 4,096 distinct gradients per document; further ones are drawn as a solid color (`clamped`). Translucent stops are drawn opaque (`unsupported-property`). |
