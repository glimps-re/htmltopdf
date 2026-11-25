package htmltopdf

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/dom"
	"github.com/glimps-re/htmltopdf/internal/layout"
	"github.com/glimps-re/htmltopdf/internal/paint"
	"github.com/glimps-re/htmltopdf/internal/pdf"
	"github.com/glimps-re/htmltopdf/internal/style"
	"github.com/glimps-re/htmltopdf/internal/text"
	"github.com/glimps-re/htmltopdf/internal/textsafe"
)

// Render reads one HTML document from src and writes one PDF to dst.
// The returned Result is never nil: on error it holds the warnings collected and the
// pages written so far, and dst may hold a partial document that must be discarded.
// Errors: a context or timeout error (errors.Is(err, context.DeadlineExceeded) or
// context.Canceled), *LimitError (errors.Is(err, ErrLimit)), ErrInternal (a
// recovered panic), the error returned by dst.Write, or the error returned by
// src.Read, wrapped. src and dst must not be nil. A single blocked src.Read or
// dst.Write cannot be interrupted by ctx or the timeout: the context is checked
// between calls, so use deadline-aware readers and writers.
func (c *Converter) Render(ctx context.Context, src io.Reader, dst io.Writer) (*Result, error) {
	res, _, err := c.render(ctx, src, dst)
	return res, err
}

// render is Render, also returning the budget for the calibration tests.
func (c *Converter) render(ctx context.Context, src io.Reader, dst io.Writer) (res *Result, b *budget.Budget, err error) {
	res = &Result{}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	b = budget.New(ctx, c.limits, c.hook)

	in := &errReader{ctx: ctx, r: src}
	out := &errWriter{ctx: ctx, w: dst}
	var written int64
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: panic (%T): %s", ErrInternal, r, panicDetail(r))
		}
		res.Bytes = written
		res.Warnings, res.WarningsDropped = publicWarnings(b)
		err = c.classify(err, in, out)
	}()

	r := &renderer{c: c, b: b, ctx: ctx}
	root, title, err := r.build(in)
	if err != nil {
		return res, b, err
	}
	err = r.paginate(root, title, out, res, &written)
	return res, b, err
}

// classify turns an internal error into one of the documented errors.
func (c *Converter) classify(err error, in *errReader, out *errWriter) error {
	switch {
	case err == nil:
		return nil
	case out.err != nil:
		return out.err
	case in.err != nil && !isContext(err):
		return fmt.Errorf("htmltopdf: reading input: %w", in.err)
	case isContext(err):
		return err
	case errors.Is(err, ErrInternal):
		return err
	}
	var le *budget.LimitError
	if errors.As(err, &le) {
		if l, ok := publicLimits[le.Kind]; ok {
			return &LimitError{Limit: l, Max: le.Max}
		}
	}
	if errors.Is(err, pdf.ErrContentSize) {
		return &LimitError{Limit: LimitOutputBytes, Max: pdf.MaxContentBytes}
	}
	return fmt.Errorf("%w: %w", ErrInternal, err)
}

// publicLimits names the public Limit of each budget kind; VarBytes has none.
var publicLimits = map[budget.Kind]Limit{
	budget.InputBytes: LimitInputBytes, budget.Nodes: LimitNodes, budget.CSSBytes: LimitCSSBytes,
	budget.Selectors: LimitSelectors, budget.Styles: LimitStyles, budget.Boxes: LimitBoxes,
	budget.Glyphs: LimitGlyphs, budget.Pages: LimitPages, budget.OutputBytes: LimitOutputBytes,
	budget.Work: LimitWork, budget.HTMLDepth: LimitHTMLDepth,
}

// maxPanicDetail bounds the panic text copied into the error: it may carry
// document content into the host's logs.
const maxPanicDetail = 80

func panicDetail(v any) string {
	s := fmt.Sprint(v)
	if len(s) > maxPanicDetail {
		s = s[:maxPanicDetail]
	}
	return strings.Map(func(r rune) rune {
		if r == utf8.RuneError || r < ' ' || r == 0x7f || textsafe.IsUnsafe(r) {
			return '?'
		}
		return r
	}, s)
}

func isContext(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func publicWarnings(b *budget.Budget) ([]Warning, int) {
	ws, dropped := b.Warnings()
	out := make([]Warning, len(ws))
	for i, w := range ws {
		out[i] = Warning{Code: WarningCode(w.Code), Detail: w.Detail, Count: w.Count}
	}
	return out, dropped
}

// errReader and errWriter remember the first error of the caller's reader and
// writer, to tell it from the errors of the pipeline.
type errReader struct {
	ctx context.Context
	r   io.Reader
	err error
}

func (e *errReader) Read(p []byte) (int, error) {
	if err := e.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := e.r.Read(p)
	if err != nil && !errors.Is(err, io.EOF) && e.err == nil {
		e.err = err
	}
	return n, err
}

type errWriter struct {
	ctx context.Context
	w   io.Writer
	err error
}

func (e *errWriter) Write(p []byte) (int, error) {
	if e.err != nil {
		return 0, e.err
	}
	if err := e.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := e.w.Write(p)
	if err != nil {
		e.err = err
	}
	return n, err
}

type renderer struct {
	c   *Converter
	b   *budget.Budget
	ctx context.Context
	// page is the style of the pages and media the environment of @media; pages
	// gives the style of a named page.
	page  *style.PageStyle
	pages func(name string) *style.PageStyle
}

// defaultEnv is the media environment before the page size is known: the
// area of the default page, at the scale of the render.
func defaultEnv(scale float64) css.MediaEnv { return envOf(style.DefaultPage(), scale) }

func envOf(ps *style.PageStyle, scale float64) css.MediaEnv {
	return css.MediaEnv{
		Print:  true,
		Width:  (ps.Width - ps.Margin[style.Left] - ps.Margin[style.Right]) / scale,
		Height: (ps.Height - ps.Margin[style.Top] - ps.Margin[style.Bottom]) / scale,
	}
}

// build runs the stages up to the box tree: read and parse, document
// stylesheets, cascade and boxes. The DOM is dropped when it returns.
func (r *renderer) build(src io.Reader) (*box.Box, string, error) {
	doc, err := dom.Parse(src, r.b)
	if err != nil {
		return nil, "", err
	}
	sheets, err := r.documentSheets(doc)
	if err != nil {
		return nil, "", err
	}
	r.b.Checkpoint("css")
	if err := r.b.Poll(); err != nil {
		return nil, "", err
	}
	resolver := r.resolver(sheets)
	root, err := box.Build(doc, resolver, r.b)
	if err != nil {
		return nil, "", err
	}
	r.b.Checkpoint("box")
	r.page = resolver.Page(style.PageContext{Number: 1})
	r.pages = func(name string) *style.PageStyle { return resolver.Page(style.PageContext{Name: name}) }
	return root, doc.Title, nil
}

// documentSheets compiles the style elements of the document, in order.
func (r *renderer) documentSheets(d *dom.Document) ([]*style.Sheet, error) {
	var sheets []*style.Sheet
	for i := range d.Elements {
		e := &d.Elements[i]
		if e.NS != dom.NSHTML || e.Tag.String() != "style" {
			continue
		}
		if !r.mediaAttrMatches(e) {
			continue
		}
		var sb strings.Builder
		for ref := e.FirstChild; ref != dom.NoNode; ref = d.NextSibling(ref) {
			if ti, ok := ref.Text(); ok {
				sb.WriteString(d.Texts[ti].Data)
			}
		}
		ss, err := css.ParseStylesheet([]byte(sb.String()), css.OriginDocument, r.b)
		if err != nil {
			return nil, err
		}
		sh, err := style.Compile(ss, r.b)
		if err != nil {
			return nil, err
		}
		sheets = append(sheets, sh)
	}
	return sheets, nil
}

// mediaAttrMatches reports whether the media attribute of a style element
// matches the print environment; a malformed value does not match, with a
// warning. The page area is not known yet: the default A4 area is used.
func (r *renderer) mediaAttrMatches(e *dom.Element) bool {
	v, ok := e.Attr("media")
	if !ok || strings.TrimSpace(v) == "" {
		return true
	}
	if strings.ContainsAny(v, "{};\\/\"'") {
		r.b.Warn(budget.WarnInvalidValue, "style media: "+v)
		return false
	}
	ss, err := css.ParseStylesheet([]byte("@media "+v+"{}"), css.OriginDocument, r.b)
	if err != nil || len(ss.Rules) != 1 {
		r.b.Warn(budget.WarnInvalidValue, "style media: "+v)
		return false
	}
	return css.ParseMediaQuery(ss.Rules[0].Prelude, r.b).Match(defaultEnv(r.c.scale))
}

// resolver builds the style resolver. The media environment is the page area,
// which the page rules themselves give: they are evaluated first on a scratch
// budget for the default environment, and the real resolver is built with the
// environment of the resulting page.
func (r *renderer) resolver(sheets []*style.Sheet) *style.Resolver {
	scratch := budget.New(r.ctx, r.c.limits, nil)
	probe := style.NewResolver(r.c.ua, r.c.extra, sheets, defaultEnv(r.c.scale), scratch)
	env := envOf(probe.Page(style.PageContext{Number: 1}), r.c.scale)
	return style.NewResolver(r.c.ua, r.c.extra, sheets, env, r.b)
}

// deferredPage is a page whose margin boxes wait for the page count. It keeps
// the page header only: the display list is gone once the body is painted.
type deferredPage struct {
	ref  pdf.Ref
	page layout.Page
}

// marginBoxes paints the margin boxes of a page, now that total is known, as
// the deferred content stream of the page.
func (r *renderer) marginBoxes(w *pdf.Writer, painter *paint.Painter, pag *layout.Paginator, d deferredPage, total int) error {
	items, err := pag.MarginBoxes(&d.page, total)
	if err != nil {
		return err
	}
	content := &pdf.Content{}
	whole := display.Rect{W: d.page.Width, H: d.page.Height}
	if _, err := painter.Page(content, paint.Geometry{Width: d.page.Width, Height: d.page.Height, Area: whole, Scale: 1}, items); err != nil {
		return err
	}
	return w.WriteDeferred(d.ref, content)
}

// paginate lays the boxes out page by page, paints each page into the writer
// and closes the document.
func (r *renderer) paginate(root *box.Box, title string, dst io.Writer, res *Result, written *int64) error {
	w := pdf.NewWriter(dst, r.b)
	defer func() { *written = w.Written() }()
	shaper := text.NewShaper(r.c.fonts, r.b)
	pag := layout.NewPaginator(root, layout.Config{Page: r.page, Pages: r.pages, Scale: r.c.scale, Fonts: r.c.fonts, Shaper: shaper, Budget: r.b})
	painter := paint.NewPainter(w, shaper, r.b)
	var deferred []deferredPage
	for {
		pg, err := pag.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		content := &pdf.Content{}
		geom := paint.Geometry{Width: pg.Width, Height: pg.Height, Area: pg.Area, Scale: pg.Scale}
		links, err := painter.Page(content, geom, pg.Items)
		if err != nil {
			return err
		}
		ref, err := w.AddPage(pg.Width*paint.PxToPt, pg.Height*paint.PxToPt, content, pg.HasBoxes, links)
		if err != nil {
			return err
		}
		for _, an := range painter.Anchors() {
			if err := w.SetDest(an.Name, ref, 0, an.Y); err != nil {
				return err
			}
		}
		if pg.HasBoxes {
			deferred = append(deferred, deferredPage{ref: ref, page: layout.Page{Number: pg.Number, Name: pg.Name, Width: pg.Width, Height: pg.Height, Area: pg.Area, Scale: pg.Scale}})
		}
		res.Pages++
		r.b.Checkpoint("page")
	}
	for _, d := range deferred {
		if err := r.b.Poll(); err != nil {
			return err
		}
		if err := r.marginBoxes(w, painter, pag, d, res.Pages); err != nil {
			return err
		}
	}
	r.b.Checkpoint("margin")
	if err := w.Close(pdf.Info{Title: title}); err != nil {
		return err
	}
	r.b.Checkpoint("close")
	return nil
}
