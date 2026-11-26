# Changelog

All notable changes to this project are documented in this file. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project
uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- Raster images: `<img>` whose `src` is a base64 `data:` URL holding a PNG or a
  JPEG is laid out as a replaced box (attributes, `width`/`height`, `min-`/`max-width`
  with the ratio kept, `display`, borders, radius, opacity, transforms, links) and
  drawn. Every image is decoded under internal caps (16 384 px per side, 4 Mpx per
  image, 64 distinct images and 16 Mpx per document, work charged to `MaxWork`)
  and re-encoded: no input image byte reaches the PDF. Nothing is ever fetched.
- `WarnImageRejected` (`image-rejected`): an image that is not drawn; `Detail` is
  the reason (`scheme`, `type`, `encoding`, `format`, `size`, `count`, `total`,
  `corrupt`), never the `src`. A rejected image draws its `alt` text instead; a
  corrupt one keeps its box, blank. `<img>` no longer warns `unsupported-element`.
- Links: an image, inline `<svg>` or inline-block inside `<a href>` is clickable
  over its border box.

### Fixed
- An inline box (inline-block, svg, image) taller than a page moves to the next
  page once and is clipped there, instead of adding pages until `MaxPages`.

### Documented deviations from Chrome
- A percent-encoded `data:` payload, or a C0 control, tab or newline inside
  `data:`, is rejected (Chrome accepts them).
- `min-height` and `max-height` are unsupported, so the ratio rule under them does
  not apply; a percentage height of an inline image falls back to `auto`.
- An image taller than the page moves whole to the next page and is clipped there
  (Chrome slices it).
- No `object-fit`, EXIF orientation or colour management.
- A progressive 4:2:0 JPEG above about 3.7 Mpx is rejected as `size` (conservative
  memory estimate).
- `url()` in CSS stays unsupported.

### Known issues
- A huge empty `height`, padding, border or table height still adds pages until
  `MaxPages` fails the render.
- A huge `line-height` can push glyphs outside the page area.

## [0.1.0]

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
- Overflow of fixed-height boxes never loses text: content that overflows a fixed
  `height` in a flex or grid item, a table cell or an inline-block counts for the
  page fit and is drawn on a page, as in block flow (it is clipped only by
  `overflow: hidden`).
- `position: fixed` (drawn on every page), dashed and dotted borders,
  `text-decoration-thickness`.
- `Converter.RenderTemplate`: renders an html/template with data under the same
  limits as Render, with no second copy of the template output (Render holds it
  once, as for any src).

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
- Overflow of a fixed-height box is placed on pages as one block and does not push
  the following siblings: a margin inside an overflowing item is not cut at page
  breaks (a huge margin there adds pages, and past `MaxPages` the render fails with
  a `LimitError`), and on a continuation page the rest of the overflow can overlap
  the next content; the text stays drawn.
- Inside a multi-column container, content overflowing a fixed-height box counts
  towards the column height, so the container can be taller than in Chrome.
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
- Multi-column layout time grows quadratically with the number of lines in the
  container: about 7000 hostile lines reach the default `MaxWork` and the render
  fails with a `LimitError`.
- With a line-height smaller than the font size, a baseline can sit a few pixels
  below the content area (inside the page margin); the kept-text check does not
  flag it.
- Relative offsets (`position: relative` with `top`) are ignored when deciding
  where a page ends, so a line near a page bottom can be drawn below the page area
  without being flagged.
