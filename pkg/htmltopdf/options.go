package htmltopdf

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/layout"
	"github.com/glimps-re/htmltopdf/internal/style"
)

// Options configures a Converter. The zero value is valid: A4 pages with 10 mm
// margins, embedded Go fonts, default limits.
type Options struct {
	// Fonts are TrueType fonts added to the embedded Go fonts. They are trusted
	// operator configuration (see Font): font data must come from a trusted
	// source, because a crafted font can exhaust memory.
	Fonts []Font

	// Generic names the families used for the CSS generic families. Empty fields
	// use the embedded Go fonts ("Go" for sans-serif and serif, "Go Mono" for
	// monospace).
	Generic GenericFamilies

	// Fallback lists the families tried, in order, for a character that the
	// families of the element cannot draw, before the embedded Go fonts.
	Fallback []string

	// ExtraCSS is a stylesheet with priority over the document: its declarations
	// win over every document declaration, whatever their specificity or
	// !important. Typical use: @page size, margins and footers that hostile
	// content must not be able to change.
	ExtraCSS string

	// Scale multiplies the size of the page-area content (not of the margin
	// boxes), like Chrome's print scale. 0 means 1. Valid range: [0.1, 2].
	Scale float64

	// PageSize, when set, is the paper of every page, whatever the @page size rules
	// say; only their orientation is kept, per page: landscape (the keyword, or a
	// width larger than the height) swaps the sides. Margins and the margin clamp
	// still come from the cascade. The zero value leaves the sizes to @page.
	PageSize PageSize

	// Limits bounds the resources of one Render. Zero fields take the defaults.
	Limits Limits
}

// PageSize is a paper size in CSS px (1/96 in). Each side must lie in
// [MinPageSide, MaxPageSide] (1 in to 200 in), else New returns ErrInvalidOptions;
// the zero value means no paper.
type PageSize struct{ Width, Height float64 }

// Bounds of a PageSize side, in CSS px: those of a @page size.
const (
	MinPageSide = style.MinPageSidePx
	MaxPageSide = style.MaxPageSidePx
)

// Font is one TrueType font file with glyf outlines. Font collections (.ttc),
// CFF-flavoured OpenType fonts and WOFF files are rejected by New.
// Fonts are trusted operator configuration: the caller is responsible for their
// origin and licence; New validates structure, not resistance to crafted fonts,
// which can exhaust memory.
type Font struct {
	// Data is the font file. It is retained by the Converter and must not be
	// modified. At most 32 MiB; all Fonts together at most 128 MiB.
	Data []byte
	// Family overrides the family name read from the font's name table.
	Family string
}

// GenericFamilies names the font families used for the CSS generic families.
type GenericFamilies struct {
	SansSerif string
	Serif     string
	Monospace string
}

// Limits bounds the resources of one Render. A zero field takes its default (see
// DefaultLimits); a negative field is invalid.
type Limits struct {
	// MaxInputBytes is the number of HTML bytes read; default 2 MiB.
	MaxInputBytes int64
	// MaxNodes bounds the DOM nodes: elements, text, comments and attributes;
	// default 100 000. Documents whose node count cannot be bounded by 4 times
	// MaxNodes before parsing are rejected early.
	MaxNodes int
	// MaxCSSBytes bounds the document stylesheets and style attributes; default
	// 512 KiB.
	MaxCSSBytes int64
	// MaxSelectors bounds the selectors of style rules, each member of a list
	// counting; default 5 000.
	MaxSelectors int
	// MaxVarBytes bounds the bytes produced by var() substitution; default
	// 256 KiB. Past it the properties concerned are dropped with a warning.
	MaxVarBytes int64
	// MaxStyles bounds the distinct computed styles; default 20 000.
	MaxStyles int
	// MaxBoxes bounds the boxes of the box tree; default 200 000.
	MaxBoxes int
	// MaxGlyphs bounds the shaped glyphs; default 2 000 000.
	MaxGlyphs int
	// MaxPages bounds the pages; default 500.
	MaxPages int
	// MaxOutputBytes bounds the PDF bytes written; default 64 MiB. Each page
	// content stream (the body, and the margin boxes of a page) is also capped
	// at 16 MiB before compression; a LimitError for
	// it reports Max = 16 MiB.
	MaxOutputBytes int64
	// MaxWork bounds the abstract work units a render may spend; default
	// 100 000 000. It is a count, not a time bound: the cost of a unit varies
	// by stage, so Timeout bounds the time. Decoding images is charged here: a
	// document with many large images can approach the default; callers that
	// expect such documents can raise it.
	MaxWork int64
	// Timeout is the wall-clock bound applied on top of ctx; default 60 s. It
	// bounds the render's wall-clock time together with ctx; MaxWork is not a
	// time bound.
	Timeout time.Duration
}

// DefaultLimits returns the limits used for zero fields.
func DefaultLimits() Limits {
	d := budget.Defaults()
	return Limits{
		MaxInputBytes:  d[budget.InputBytes],
		MaxNodes:       int(d[budget.Nodes]),
		MaxCSSBytes:    d[budget.CSSBytes],
		MaxSelectors:   int(d[budget.Selectors]),
		MaxVarBytes:    d[budget.VarBytes],
		MaxStyles:      int(d[budget.Styles]),
		MaxBoxes:       int(d[budget.Boxes]),
		MaxGlyphs:      int(d[budget.Glyphs]),
		MaxPages:       int(d[budget.Pages]),
		MaxOutputBytes: d[budget.OutputBytes],
		MaxWork:        d[budget.Work],
		Timeout:        defaultTimeout,
	}
}

const defaultTimeout = 60 * time.Second

// resolved is a validated Limits: the budget limits and the timeout.
type resolved struct {
	limits  budget.Limits
	timeout time.Duration
}

// resolve fills the zero fields with the defaults and rejects negative ones.
func (l Limits) resolve() (resolved, error) {
	r := resolved{limits: budget.Defaults(), timeout: defaultTimeout}
	set := func(kind budget.Kind, v int64) error {
		switch {
		case v < 0:
			return fmt.Errorf("%w: negative limit %s", ErrInvalidOptions, kind)
		case v > 0:
			r.limits[kind] = v
		}
		return nil
	}
	for _, f := range []struct {
		kind budget.Kind
		v    int64
	}{
		{budget.InputBytes, l.MaxInputBytes},
		{budget.Nodes, int64(l.MaxNodes)},
		{budget.CSSBytes, l.MaxCSSBytes},
		{budget.Selectors, int64(l.MaxSelectors)},
		{budget.VarBytes, l.MaxVarBytes},
		{budget.Styles, int64(l.MaxStyles)},
		{budget.Boxes, int64(l.MaxBoxes)},
		{budget.Glyphs, int64(l.MaxGlyphs)},
		{budget.Pages, int64(l.MaxPages)},
		{budget.OutputBytes, l.MaxOutputBytes},
		{budget.Work, l.MaxWork},
	} {
		if err := set(f.kind, f.v); err != nil {
			return resolved{}, err
		}
	}
	switch {
	case l.Timeout < 0:
		return resolved{}, fmt.Errorf("%w: negative Timeout", ErrInvalidOptions)
	case l.Timeout > 0:
		r.timeout = l.Timeout
	}
	return r, nil
}

func resolveScale(s float64) (float64, error) {
	switch {
	case s == 0:
		return 1, nil
	case math.IsNaN(s) || s < layout.MinScale || s > layout.MaxScale:
		return 0, fmt.Errorf("%w: Scale %v is outside [%v, %v]", ErrInvalidOptions, s, layout.MinScale, layout.MaxScale)
	}
	return s, nil
}

// validatePageSize accepts the zero value, else both sides must lie in
// [MinPageSide, MaxPageSide] (NaN and the infinities do not).
func validatePageSize(p PageSize) error {
	if p == (PageSize{}) {
		return nil
	}
	for _, v := range [2]float64{p.Width, p.Height} {
		if math.IsNaN(v) || v < MinPageSide || v > MaxPageSide {
			return fmt.Errorf("%w: PageSize %v x %v: each side must lie in [%d, %d]", ErrInvalidOptions, p.Width, p.Height, MinPageSide, MaxPageSide)
		}
	}
	return nil
}

// setPaper gives the resolver the paper p, when there is one. The sides take the
// single precision of a CSS length, so that the page is the one a @page size of
// the same px gives, to the byte.
func setPaper(res *style.Resolver, p PageSize) {
	if p != (PageSize{}) {
		res.SetPaper(float64(float32(p.Width)), float64(float32(p.Height)))
	}
}

// Result describes a Render.
type Result struct {
	Pages    int       // pages written
	Bytes    int64     // PDF bytes written
	Warnings []Warning // deduplicated, sorted by Code then Detail
	// WarningsDropped counts warnings that did not fit in the 256 distinct entries.
	WarningsDropped int
}

// Warning reports input that was ignored, clamped or substituted.
type Warning struct {
	Code   WarningCode
	Detail string // property, value, selector, element or character; hostile text, at most 80 bytes, control characters replaced
	Count  int    // occurrences
}

// WarningCode classifies a Warning.
type WarningCode string

// Warning codes.
const (
	WarnUnsupportedProperty WarningCode = "unsupported-property" // unknown or not implemented property
	WarnInvalidValue        WarningCode = "invalid-value"        // declaration dropped: unparsable or unsupported value
	WarnUnsupportedAtRule   WarningCode = "unsupported-at-rule"  // @import, @font-face, @supports, unknown at-rules, unsupported margin boxes
	WarnUnsupportedSelector WarningCode = "unsupported-selector" // selector list dropped
	WarnUnsupportedElement  WarningCode = "unsupported-element"  // link, iframe, object, video, canvas, form controls
	WarnBlockedLink         WarningCode = "blocked-link"         // href with a scheme other than http, https, mailto
	WarnMissingGlyph        WarningCode = "missing-glyph"        // no font draws the character; .notdef drawn
	WarnClamped             WarningCode = "clamped"              // length, page size or margins, colspan, rowspan clamped
	WarnVarBudget           WarningCode = "var-budget"           // MaxVarBytes reached: further var() are invalid at computed-value time
	WarnShapingFallback     WarningCode = "shaping-fallback"     // the shaper failed on a run; drawn with nominal glyphs
	// WarnImageRejected is an img that is not drawn; its alt text, if any, is
	// drawn instead (a corrupt image keeps its box, blank). Detail is the reason
	// only, never the src: scheme (not a data: URL; never fetched), type (not
	// PNG or JPEG), encoding (not base64), format (bad or unsupported data),
	// size, count or total (image caps), corrupt (decoding failed). Once 64
	// distinct images are held, count takes precedence: a further http: src
	// warns count, not scheme.
	WarnImageRejected WarningCode = "image-rejected"
	// WarnTextOffPage is text that is partly or entirely outside the page, as
	// positioned by the document (a large margin or offset, a fixed or absolute
	// box, a table wider than the page, a long white-space: nowrap line). Detail
	// is the side of the page it lies entirely beyond, else the side it crosses
	// (the horizontal side first): left, right, above or below, never the text.
	// Count is the number of text runs. A run that crosses an edge by no more
	// than a tenth of its font size (at most 6 pt), transformed text and
	// transparent text are not reported. The top of a run is measured at its
	// capital height: accents and ascenders above it can be cut silently.
	WarnTextOffPage WarningCode = "text-off-page"
)

// Limit names the limit reported by a LimitError.
type Limit string

// Limits that a Render can exceed. Each names the Limits field, except
// LimitHTMLDepth, the fixed nesting bound (512) of the HTML parser.
const (
	LimitInputBytes  Limit = "MaxInputBytes"
	LimitNodes       Limit = "MaxNodes"
	LimitCSSBytes    Limit = "MaxCSSBytes"
	LimitSelectors   Limit = "MaxSelectors"
	LimitStyles      Limit = "MaxStyles"
	LimitBoxes       Limit = "MaxBoxes"
	LimitGlyphs      Limit = "MaxGlyphs"
	LimitPages       Limit = "MaxPages"
	LimitOutputBytes Limit = "MaxOutputBytes"
	LimitWork        Limit = "MaxWork"
	LimitHTMLDepth   Limit = "HTMLDepth"
)

// LimitError reports the limit a Render exceeded.
type LimitError struct {
	Limit Limit
	Max   int64
}

// Error implements error.
func (e *LimitError) Error() string {
	return fmt.Sprintf("htmltopdf: limit exceeded: %s (max %d)", e.Limit, e.Max)
}

// Is reports whether target is ErrLimit.
func (e *LimitError) Is(target error) bool { return target == ErrLimit }

// Errors returned by New and Render.
var (
	ErrInvalidOptions = errors.New("htmltopdf: invalid options")
	ErrLimit          = errors.New("htmltopdf: limit exceeded")
	ErrInternal       = errors.New("htmltopdf: internal error")
)
