# CSS and HTML support

This page lists what `htmltopdf` renders, what it approximates and what it
ignores. It is updated by every change that alters CSS or HTML support.

Anything not listed as supported is dropped and reported as a warning in
`Result.Warnings` (`unsupported-property`, `invalid-value`, `unsupported-at-rule`,
`unsupported-selector`, `unsupported-element`); the render never fails because of it.

Status values: **supported**, **approximated** (rendered differently from a
browser, with the difference stated), **ignored** (dropped with a warning),
**never** (not planned).

## Syntax, cascade and selectors

| Feature | Status | Notes |
|---|---|---|
| Comments, escapes (`\41`, `\A`), `!important` | supported | Escapes are decoded in identifiers, strings and URLs. |
| Error recovery (CSS Syntax §5) | supported | A malformed rule or declaration is dropped alone, the rest of the sheet is kept; each dropped item is reported as `invalid-value`. A `}` ends an unfinished item inside a block. |
| `@media` | supported | Nested `@media` rules, up to 64 levels. |
| `@page` and its 16 margin at-rules | supported | All 16 margin boxes are parsed; corner and side boxes are ignored with `unsupported-at-rule` when the sheet is loaded. |
| `@charset` | ignored | Only accepted at the very start; the text is always UTF-8. |
| `@import`, `@font-face`, `@supports`, `@keyframes` and any other at-rule | ignored | Skipped with its block, reported as `unsupported-at-rule`. Nothing is ever loaded. |
| Nesting of blocks, functions and parentheses | approximated | Limited to 64 levels in one selector or value; a deeper item is dropped with `invalid-value`. |
| Control characters and invalid UTF-8 in CSS text | approximated | Replaced by U+FFFD. |

## Values and units

| Feature | Status | Notes |
|---|---|---|
| Lengths: `px`, `pt`, `pc`, `in`, `cm`, `mm`, `Q`, `em`, `rem`, `%`, unitless `0` | supported | Absolute units are converted to px; values are clamped to ±1e7 px. |
| Other units (`ex`, `ch`, `vw`, `vh`, `vmin`, `vmax`) and `calc()` | ignored | Declaration dropped with `invalid-value`. |
| Colors: `#rgb`, `#rgba`, `#rrggbb`, `#rrggbbaa`, `rgb()`, `rgba()`, named colors, `transparent` | supported | Both the comma and the space/slash syntaxes of `rgb()`; components are clamped. |
| `hsl()`, `hwb()`, `lab()`, `color()`, `currentcolor`, system colors | ignored | Declaration dropped with `invalid-value`. |
| `linear-gradient()` | approximated | Angle (`deg`, `rad`, `grad`, `turn`) or `to <side|corner>`, 2 to 64 color stops with optional percentages; no hints, no lengths as positions. `repeating-` and other gradient types are ignored. |
| Functions with many arguments | approximated | At most 128 arguments or stops are examined; a function with more is rejected. |

## Layout

| Feature | Status | Notes |
|---|---|---|

## Text and fonts

| Feature | Status | Notes |
|---|---|---|

## Paged media

| Feature | Status | Notes |
|---|---|---|

## HTML elements and attributes

| Feature | Status | Notes |
|---|---|---|
