package htmltopdf

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/pdf"
	"github.com/glimps-re/htmltopdf/internal/style"
	"github.com/glimps-re/htmltopdf/internal/text"
)

// Converter renders documents with a fixed configuration. It is safe for
// concurrent use. Fonts, the UA stylesheet and ExtraCSS are prepared once, in New.
type Converter struct {
	fonts  *text.FontSet
	ua     *style.Sheet
	extra  *style.Sheet
	limits budget.Limits
	scale  float64
	// timeout is applied to every Render on top of its context.
	timeout time.Duration
	// hook receives the stage names of a render, for the heap measurements of
	// the tests; nil in production.
	hook func(stage string)
}

// New validates opts and returns a Converter. It returns an error wrapping
// ErrInvalidOptions for an invalid option, an unsupported or malformed font, or an
// ExtraCSS that contains a syntax error or a declaration the engine rejects.
func New(opts Options) (*Converter, error) {
	scale, err := resolveScale(opts.Scale)
	if err != nil {
		return nil, err
	}
	lim, err := opts.Limits.resolve()
	if err != nil {
		return nil, err
	}
	fonts, err := newFontSet(opts)
	if err != nil {
		return nil, err
	}
	ua, err := uaSheet()
	if err != nil {
		return nil, err
	}
	extra, err := compileExtra(ua, opts.ExtraCSS, scale, lim)
	if err != nil {
		return nil, err
	}
	return &Converter{fonts: fonts, ua: ua, extra: extra, limits: lim.limits, scale: scale, timeout: lim.timeout}, nil
}

func newFontSet(opts Options) (*text.FontSet, error) {
	if len(opts.Fonts) > text.MaxFonts {
		return nil, fmt.Errorf("%w: %d fonts exceed %d", ErrInvalidOptions, len(opts.Fonts), text.MaxFonts)
	}
	total := 0
	for i, f := range opts.Fonts {
		if total += len(f.Data); len(f.Data) > text.MaxFontBytes || total > text.MaxTotalFontBytes {
			return nil, fmt.Errorf("%w: font %d: fonts exceed the size caps", ErrInvalidOptions, i)
		}
	}
	src := make([]text.Source, len(opts.Fonts))
	for i, f := range opts.Fonts {
		// Embedding (pdf) and shaping (text) parse a font differently: both must accept it.
		if err := pdf.ValidateFont(f.Data); err != nil {
			return nil, fmt.Errorf("%w: font %d: %w", ErrInvalidOptions, i, err)
		}
		src[i] = text.Source{Data: f.Data, Family: f.Family}
	}
	g := text.Generic{SansSerif: opts.Generic.SansSerif, Serif: opts.Generic.Serif, Monospace: opts.Generic.Monospace}
	fs, err := text.NewFontSet(src, g, opts.Fallback)
	if err != nil {
		if errors.Is(err, text.ErrInvalidFont) {
			return nil, fmt.Errorf("%w: %w", ErrInvalidOptions, err)
		}
		return nil, fmt.Errorf("%w: %w", ErrInternal, err)
	}
	return fs, nil
}

var uaOnce = sync.OnceValues(func() (*style.Sheet, error) {
	b := budget.New(context.Background(), budget.Defaults(), nil)
	s, err := style.CompileUA(b)
	if err != nil {
		return nil, fmt.Errorf("%w: user-agent stylesheet: %w", ErrInternal, err)
	}
	if err := firstWarning(b); err != nil {
		return nil, fmt.Errorf("%w: user-agent stylesheet: %w", ErrInternal, err)
	}
	return s, nil
})

// uaSheet returns the compiled user-agent stylesheet, shared by every Converter.
func uaSheet() (*style.Sheet, error) { return uaOnce() }

// compileExtra parses and compiles ExtraCSS strictly: any dropped rule,
// declaration, selector or at-rule, and any clamped page size or margin, is an
// invalid option, so that a typo cannot silently remove a footer.
func compileExtra(ua *style.Sheet, src string, scale float64, lim resolved) (*style.Sheet, error) {
	if strings.TrimSpace(src) == "" {
		return nil, nil
	}
	b := budget.New(context.Background(), lim.limits, nil)
	ss, err := css.ParseStylesheet([]byte(src), css.OriginExtra, b)
	if err != nil {
		return nil, fmt.Errorf("%w: ExtraCSS: %w", ErrInvalidOptions, err)
	}
	sheet, err := style.Compile(ss, b)
	if err != nil {
		return nil, fmt.Errorf("%w: ExtraCSS: %w", ErrInvalidOptions, err)
	}
	// The page rules are evaluated here too: size and margin clamps are
	// reported by Page, not by Compile.
	r := style.NewResolver(ua, sheet, nil, defaultEnv(scale), b)
	r.Page(style.PageContext{Number: 1})
	if err := firstWarning(b); err != nil {
		return nil, fmt.Errorf("%w: ExtraCSS: %w", ErrInvalidOptions, err)
	}
	return sheet, nil
}

// firstWarning returns an error describing the first warning of b, or nil.
func firstWarning(b *budget.Budget) error {
	w, _ := b.Warnings()
	if len(w) == 0 {
		return nil
	}
	return fmt.Errorf("%s: %s", w[0].Code, w[0].Detail)
}
