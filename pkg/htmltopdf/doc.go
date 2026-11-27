// Package htmltopdf renders HTML documents styled with CSS to PDF without a
// browser. Input documents are treated as hostile: one Render is bounded by
// Limits and never reads a file or a network resource. Converter.RenderTemplate
// renders an html/template with hostile data under the same Limits, with no
// second copy of the template output: Render holds it once, as for any src.
//
// # Reference configuration
//
// A Converter is built once and reused; Render may be called from several
// goroutines. The reference configuration reproduces what a headless browser
// prints for an A4 report at a print scale of 0.9: the paper comes from
// Options.PageSize, the document keeps its own orientation, and the page
// furniture is given as ExtraCSS, which the document cannot override.
//
//	conv, err := htmltopdf.New(htmltopdf.Options{
//		PageSize: htmltopdf.PageSize{Width: 794.56, Height: 1122.56},
//		Scale:    0.9,
//		Fonts:    fonts, // TrueType files read by the caller
//		Generic:  htmltopdf.GenericFamilies{SansSerif: "Liberation Sans", Monospace: "DejaVu Sans Mono"},
//		Fallback: []string{"DejaVu Sans"},
//		ExtraCSS: `@page { margin: 96px 28px 124px 28px;
//			@bottom-right { content: "Page " counter(page) " of " counter(pages) } }`,
//	})
//
// ExtraCSS is strict: a rule or declaration that cannot be applied makes New
// fail, so that a typo cannot silently remove a footer. The document's own CSS
// is lenient: what cannot be applied is dropped and reported.
//
// # Limits
//
// Limits bounds the input size, the DOM, the style and selector counts, the
// boxes, the shaped glyphs, the pages, the output size, the abstract work spent
// and the elapsed time. Every limit has a default (see DefaultLimits); a render
// that crosses one stops with a *LimitError, which matches ErrLimit. The limits
// are the only protection against hostile input, so they should be lowered, not
// raised, for untrusted documents. MaxWork counts work, Timeout bounds time.
// Memory use is bounded by them: pages are laid out and written one at a time.
//
// # Degradation
//
// What the engine does not support is ignored and reported in Result.Warnings
// (unsupported properties, values, at-rules, selectors and elements, clamped
// values, missing glyphs, text placed partly or entirely outside the page:
// WarnTextOffPage); a warning never fails the render. Approximations (features
// drawn differently from a browser) are not all warned: docs/css-support.md lists
// every one with its difference. Output is deterministic: the same input
// and options give the same bytes.
//
// # Images
//
// An <img> is drawn when its src is a data: URL holding a base64 PNG or JPEG;
// nothing else is ever fetched or read. Each image has at most 16384 pixels per
// side and 4194304 pixels in all; a render holds at most 64 distinct images
// (identical src values are one image) and 16777216 pixels in all. Their
// encoded bytes count against MaxInputBytes and their decoding against MaxWork
// (see Limits.MaxWork). Every image is decoded and re-encoded (PNG losslessly,
// JPEG at quality 95): no byte of the input image reaches the PDF. A rejected
// image warns WarnImageRejected and its alt text is drawn instead; one whose
// data fails to decode (corrupt) keeps its box, blank.
//
// With RenderTemplate, html/template replaces a data: URL in src with
// "#ZgotmplZ" unless the value has type template.URL: convert only image URLs
// that the caller built or checked.
//
// # Non-goals
//
// JavaScript, forms, images other than PNG and JPEG `data:` URLs in <img>,
// url() in CSS, @font-face and any other resource loading (external
// stylesheets, @import), floats, bidirectional reordering, PDF/A and
// encryption are not supported. The PDF is not tagged: it has no structure
// tree and is not PDF/UA; text stays extractable. Fonts are given as bytes in
// Options.Fonts and are trusted operator configuration.
//
// Only 64-bit platforms are supported: the PDF writer and the resource limits use
// int values beyond 2^31. The package cross-compiles for Windows (amd64 and
// arm64, checked in CI); it is tested on Linux.
package htmltopdf
