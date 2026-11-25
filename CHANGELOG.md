# Changelog

All notable changes to this project are documented in this file. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project
uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

First release: a pure-Go HTML+CSS to PDF renderer for reports, built as four
milestones (M1 to M4). The entry below lists what each added. `docs/css-support.md`
is the reference for what is supported, approximated or ignored.

### Added

#### M1: base engine and the host-connector report
- Public API: `New`, `Converter.Render`, `Options` (`Fonts`, `Generic`, `Fallback`,
  `ExtraCSS`, `Scale`, `Limits`), `Result`, `Warning`, `LimitError`.
- Hostile-input model: size, node, style, selector, box, glyph, page, output, work
  and time limits; no file or network access; panics recovered; deterministic output.
- HTML parser, CSS parser, selectors, `@media` (print), `@page` with margin boxes,
  custom properties and `var()`, cascade with ExtraCSS above the document.
- Block and inline layout with pagination, tables (fixed and auto layout, header
  repeat), margin boxes, `counter(page)` and `counter(pages)`, print scale.
- Backgrounds, borders, border radius, clipping, gradients; TrueType fonts with
  shaping, line breaking and subsetting; ToUnicode text extraction; Flate streams.

#### M2: modern layout
- Forced and avoided breaks, flex layout, grid layout, fixed-table column widths
  as Chrome distributes them, inline-block, `text-transform`, `box-shadow`.

#### M3: executive layout
- `height`, `min-width`, vertical alignment by length, flex auto margins, baseline
  alignment and definite heights, automatic table layout, borders snapped to device
  pixels, `::before`/`::after`, counters, absolute and relative positioning,
  stacking and transforms, SVG boxes, radial and translucent gradients, named pages,
  internal links and named destinations.

#### M4: analyst layout and release
- `Options.PageSize`: the caller's paper with the document's orientation;
  `ErrInvalidOptions` outside `MinPageSide` to `MaxPageSide` (96 to 19200 px).
- Multi-column containers (`columns`, `column-count`, `column-gap`, `column-rule`):
  columns flow like pages, the last segment is balanced as Chrome does, table
  headers repeat in the next column, orphans and widows of 2 apply inside columns;
  the rule between columns can be solid, dashed or dotted.
- Negative margins never lose text: a box that a negative margin would pull above
  the top of the page is placed at the top of the current page, in blocks, table
  cells, flex and grid items and inline-blocks; a first line pushed past the page
  bottom by a margin moves to the next page. A kept-text invariant (every in-flow
  text box is drawn on a page, positioned and rotated ones are emitted) is checked
  in the fuzz targets and over the report corpus.
- Clamps never hide text: the cells of a table row past column 1000 are folded
  into the last kept cell (their content in order, absolute boxes and ids kept,
  with a `clamped` warning) instead of being dropped; cells, flex and grid items
  and inline-blocks laid out apart are never cut by the size of the page they are
  measured on, and a multi-column box there runs its last column downwards. Lines
  and boxes at the length clamp pile up inside their box.
- `position: fixed` (drawn on every page), dashed and dotted borders,
  `text-decoration-thickness`.

### Documented deviations from Chrome
- Orphans and widows of 2 are applied to paragraphs inside multi-column containers
  and to the lines of a cut table row; elsewhere a paragraph breaks wherever a
  line fits.
- Multi-column: `column-width`, `column-span`, `column-fill`, `break-before` and
  `break-after` with `column`, and `min-height` are unsupported and warned; a
  multi-column box inside another is one column (warned); with a `height`, Chrome
  adds columns past the count where this engine continues on the next page; a
  container wider than the page area is not narrowed; a balanced table row cut can
  leave a 1 px sliver.
- A box that a negative margin would pull onto an earlier page, or above the
  document, is placed at the top of the current page, where Chrome draws it on the
  earlier page or not at all. Text that author lengths (line-height,
  vertical-align, text-indent, negative margin-left) place outside the page is
  clipped, as in Chrome.
- The PDF is not tagged (no structure tree, not PDF/UA).
- Non-goals: JavaScript, forms, raster images, `@font-face`, external resources,
  floats, bidirectional reordering, PDF/A, encryption.

### Known limitations
- Only 64-bit platforms are supported: the PDF writer and the resource limits use
  int values beyond 2^31 (there is no build constraint; 32-bit builds fail to compile).

### Known issues
- A grid inside an inline-block or a flex item that is sized by its content is
  sized like a block, not by its tracks.
- A named page (`page: name`) that starts or ends inside a box does not propagate
  to its ancestors.
- Line-box half-leading is floored differently from Blink in some cases, which can
  shift a line by a fraction of a pixel.
- The outer border geometry of collapsed-border tables differs from Chrome's.
- Percentage widths of table columns are distributed approximately.
