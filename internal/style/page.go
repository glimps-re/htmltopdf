package style

import (
	"math"
	"unicode/utf8"

	"github.com/glimps-re/htmltopdf/internal/budget"

	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/dom"
)

// Page geometry bounds (design §6.1), in CSS px.
const (
	pxPerIn = 96
	// MinPageSidePx and MaxPageSidePx bound each side of the page: 1 in to 200 in.
	MinPageSidePx = 1 * pxPerIn
	MaxPageSidePx = 200 * pxPerIn
	// MinPageAreaPx is what the margins must leave of each side.
	MinPageAreaPx = 1 * pxPerIn

	mmPx = pxPerIn / 25.4
)

// MarginPos names the margin boxes drawn in M1.
type MarginPos uint8

// Margin box positions.
const (
	MarginTopLeft MarginPos = iota
	MarginTopCenter
	MarginTopRight
	MarginBottomLeft
	MarginBottomCenter
	MarginBottomRight
	numMarginBoxes
)

var marginBoxNames = [numMarginBoxes]string{
	"top-left", "top-center", "top-right", "bottom-left", "bottom-center", "bottom-right",
}

// ContentKind tells what a ContentItem draws.
type ContentKind uint8

// Content item kinds.
const (
	ContentText ContentKind = iota
	ContentPage
	ContentPages
	// ContentCounter is counter(name) in the content of a pseudo-element; Text is
	// the counter name.
	ContentCounter
)

// ContentItem is one part of the content of a margin box: a string (already
// sanitized, since it will be drawn) or the page counters.
type ContentItem struct {
	Kind ContentKind
	Text string
	// Format is the counter style of ContentCounter.
	Format CounterFormat
}

// MarginBox is a margin box with its style and content.
type MarginBox struct {
	Style   *Computed
	Content []ContentItem
}

// PageContext identifies a page. M1 styles every page alike; the fields are
// the input of the page selectors of later milestones.
type PageContext struct {
	// Number is the 1-based page number.
	Number int
	// Name is the named page ("@page name", CSS Page 3 §3), "" for none.
	Name string
}

// PageStyle is the style of a page: its size and margins in CSS px and its
// margin boxes (nil when a box has no content).
type PageStyle struct {
	Width, Height float64
	// Margin is indexed by Top, Right, Bottom and Left.
	Margin [4]float64
	Boxes  [numMarginBoxes]*MarginBox
}

type pageEntry struct {
	p     *cpage
	level uint8
}

func (r *Resolver) addPages(s *Sheet, level uint8) {
	for i := range s.pages {
		p := &s.pages[i]
		if !r.mediaMatches(p.media) {
			continue
		}
		if p.name == "" {
			r.pages = append(r.pages, pageEntry{p: p, level: level})
			continue
		}
		if r.named == nil {
			r.named = make(map[string][]pageEntry)
		}
		r.named[p.name] = append(r.named[p.name], pageEntry{p: p, level: level})
	}
}

// Page returns the style of the page described by pc: the @page rules without a
// name, then those named pc.Name, which win over the first. The result is
// computed once per name and shared; a name without rule shares the style of the
// default page.
func (r *Resolver) Page(pc PageContext) *PageStyle {
	if r.pageStyle == nil {
		r.pageStyle = r.buildPage("")
	}
	if len(r.named[pc.Name]) == 0 {
		return r.pageStyle
	}
	if ps, ok := r.pageNames[pc.Name]; ok {
		return ps
	}
	if r.pageNames == nil {
		r.pageNames = make(map[string]*PageStyle)
	}
	ps := r.buildPage(pc.Name)
	r.pageNames[pc.Name] = ps
	return ps
}

// pageOverlay holds, per cascade bucket, the winning specified value of each
// property of the unnamed @page rules: it is built once per box, so that a page
// name costs its own rules only.
type pageOverlay [4][]propSpec

// unnamedOverlay returns the overlay of the unnamed rules for the margin box box
// ("" for the page itself).
func (r *Resolver) unnamedOverlay(box string) *pageOverlay {
	if ov, ok := r.pageOv[box]; ok {
		return ov
	}
	var bk buckets
	for _, e := range r.pages {
		bk.addPage(e, box)
	}
	ov := new(pageOverlay)
	for i := range bk {
		ov[i] = lastWins(bk[i])
	}
	if r.pageOv == nil {
		r.pageOv = make(map[string]*pageOverlay)
	}
	r.pageOv[box] = ov
	return ov
}

func (b *buckets) addPage(e pageEntry, box string) {
	decls := e.p.decls
	if box != "" {
		decls = e.p.boxes[box]
	}
	for i := range decls {
		b.add(e.level, &decls[i])
	}
}

// lastWins reduces declarations to the last specified value of each property.
func lastWins(ds []*decl) []propSpec {
	var out []propSpec
	var at [numProps]int
	for _, d := range ds {
		if d.custom || d.deferred {
			continue
		}
		for _, ps := range d.specs {
			if j := at[ps.prop]; j > 0 {
				out[j-1] = ps
				continue
			}
			out = append(out, ps)
			at[ps.prop] = len(out)
		}
	}
	return out
}

// applyPage records the specified values of the @page rules for box ("" for the
// page itself) as the page called name: UA, document normal, document !important,
// ExtraCSS, the rules named name coming after the unnamed ones of their bucket.
// The cost is that of the named rules: the unnamed ones are reduced once.
func (r *Resolver) applyPage(box, name string) {
	ov := r.unnamedOverlay(box)
	var named buckets
	if name != "" {
		for _, e := range r.named[name] {
			named.addPage(e, box)
		}
	}
	for i := range ov {
		for _, ps := range ov[i] {
			r.set(ps.prop, ps.spec)
		}
		for _, d := range named[i] {
			r.applyPageDecl(d)
		}
	}
}

// DefaultPage returns the page style when no @page rule applies: A4 with 10 mm margins.
func DefaultPage() *PageStyle {
	a4 := namedSizes["a4"]
	return &PageStyle{
		Width:  a4[0],
		Height: a4[1],
		Margin: [4]float64{10 * mmPx, 10 * mmPx, 10 * mmPx, 10 * mmPx},
	}
}

func (r *Resolver) buildPage(name string) *PageStyle {
	ps := DefaultPage()
	r.touched = r.touched[:0]
	r.applyPage("", name)
	if s := &r.specs[pPageSize]; s.kind == specValue {
		ps.Width, ps.Height = pageSize(s)
	}
	r.clampSize(ps)
	for i, side := range [4]prop{pMarginTop, pMarginRight, pMarginBottom, pMarginLeft} {
		if s := &r.specs[side]; s.kind == specValue {
			ps.Margin[i] = pageMargin(s, ps.Width)
		}
	}
	r.resetSpecs()
	r.clampMargins(ps)
	for i := range ps.Boxes {
		ps.Boxes[i] = r.buildMarginBox(MarginPos(i), name)
	}
	return ps
}

func (r *Resolver) resetSpecs() {
	for _, p := range r.touched {
		r.specs[p] = spec{}
	}
	r.touched = r.touched[:0]
}

// applyPageDecl records a @page declaration; var() has no meaning there.
func (r *Resolver) applyPageDecl(d *decl) {
	if d.custom || d.deferred {
		return
	}
	for _, ps := range d.specs {
		r.set(ps.prop, ps.spec)
	}
}

// pageMargin resolves a margin; percentages are of the page width.
func pageMargin(s *spec, pageWidth float64) float64 {
	if s.auto {
		return 0
	}
	if s.len.Unit == css.UnitPercent {
		return clampPx(s.len.Value / 100 * pageWidth)
	}
	return pageLength(s.len)
}

func pageSize(s *spec) (w, h float64) {
	w, h = s.len.Value, s.len2.Value
	switch s.n {
	case orientLandscape:
		w, h = math.Max(w, h), math.Min(w, h)
	case orientPortrait:
		w, h = math.Min(w, h), math.Max(w, h)
	}
	return w, h
}

// clampSize bounds each side of the page to [1 in, 200 in].
func (r *Resolver) clampSize(ps *PageStyle) {
	clamped := false
	for _, side := range []*float64{&ps.Width, &ps.Height} {
		if v := math.Max(MinPageSidePx, math.Min(MaxPageSidePx, *side)); v != *side || math.IsNaN(*side) {
			*side, clamped = v, true
		}
	}
	if clamped {
		r.b.Warn(budget.WarnClamped, "page size")
	}
}

// clampMargins scales the margins of a side down together when they leave less
// than 1 in of page area (design §6.1).
func (r *Resolver) clampMargins(ps *PageStyle) {
	clamped := false
	for _, pair := range [2]struct {
		a, b int
		size float64
	}{{Top, Bottom, ps.Height}, {Right, Left, ps.Width}} {
		a, b := &ps.Margin[pair.a], &ps.Margin[pair.b]
		*a, *b = math.Max(0, *a), math.Max(0, *b)
		if sum := *a + *b; sum > pair.size-MinPageAreaPx {
			f := math.Max(0, pair.size-MinPageAreaPx) / sum
			*a, *b, clamped = *a*f, *b*f, true
		}
	}
	if clamped {
		r.b.Warn(budget.WarnClamped, "page margins")
	}
}

// Orientations of a named @page size.
const (
	orientNone = iota
	orientPortrait
	orientLandscape
)

var namedSizes = map[string][2]float64{
	"a5": {148 * mmPx, 210 * mmPx}, "a4": {210 * mmPx, 297 * mmPx}, "a3": {297 * mmPx, 420 * mmPx},
	"b5": {176 * mmPx, 250 * mmPx}, "b4": {250 * mmPx, 353 * mmPx},
	"letter": {8.5 * pxPerIn, 11 * pxPerIn}, "legal": {8.5 * pxPerIn, 14 * pxPerIn}, "ledger": {11 * pxPerIn, 17 * pxPerIn},
}

// parsePageSize parses the size descriptor: auto, a named size with an
// optional orientation, an orientation alone (A4) or one or two lengths.
func parsePageSize(toks []css.Token) (spec, bool) {
	size := namedSizes["a4"]
	named, orient := false, orientNone
	var lens []css.Length
	for _, f := range fields(trim(toks)) {
		if kw, ok := ident(f); ok {
			switch s, isNamed := namedSizes[kw]; {
			case kw == "auto":
				named = true
			case kw == "portrait" && orient == orientNone:
				orient = orientPortrait
			case kw == "landscape" && orient == orientNone:
				orient = orientLandscape
			case isNamed && !named:
				size, named = s, true
			default:
				return spec{}, false
			}
			continue
		}
		l, ok := css.ParseLength(f)
		if !ok || l.Value <= 0 || l.Unit == css.UnitPercent {
			return spec{}, false
		}
		lens = append(lens, l)
	}
	if len(lens) > 0 {
		if named || orient != orientNone || len(lens) > 2 {
			return spec{}, false
		}
		w := pageLength(lens[0])
		h := w
		if len(lens) == 2 {
			h = pageLength(lens[1])
		}
		return spec{len: css.Length{Value: w, Unit: css.UnitPx}, len2: css.Length{Value: h, Unit: css.UnitPx}}, true
	}
	if !named && orient == orientNone {
		return spec{}, false
	}
	return spec{
		len:  css.Length{Value: size[0], Unit: css.UnitPx},
		len2: css.Length{Value: size[1], Unit: css.UnitPx},
		n:    int32(orient),
	}, true
}

func pageLength(l css.Length) float64 {
	if l.Unit == css.UnitEm || l.Unit == css.UnitRem {
		return clampPx(l.Value * initialFontSize)
	}
	return clampPx(l.Value)
}

func (r *Resolver) buildMarginBox(pos MarginPos, name string) *MarginBox {
	box := marginBoxNames[pos]
	r.touched = r.touched[:0]
	r.set(pTextAlign, spec{kind: specValue, n: int32(marginBoxAlign[pos])})
	r.set(pVerticalAlign, spec{kind: specValue, n: int32(VAlignMiddle)})
	r.applyPage(box, name)
	var content []ContentItem
	if s := &r.specs[pContent]; s.kind == specValue && !s.none {
		content = r.capContent(s.items)
	}
	// Without a grid cache compute charges nothing: it cannot fail.
	c, _ := compute(nil, &r.specs, r.touched, r.b, nil)
	r.resetSpecs()
	if content == nil {
		return nil
	}
	return &MarginBox{Style: c, Content: content}
}

// marginBoxAlign is the default text-align of each margin box.
var marginBoxAlign = [numMarginBoxes]TextAlign{AlignLeft, AlignCenter, AlignRight, AlignLeft, AlignCenter, AlignRight}

// MaxContentBytes bounds the text of one margin box.
const MaxContentBytes = 1024

// capContent keeps at most MaxContentBytes of text in the content items, cutting
// on a rune boundary, and warns when it cuts.
func (r *Resolver) capContent(items []ContentItem) []ContentItem {
	out := make([]ContentItem, 0, len(items))
	left := MaxContentBytes
	cut := false
	for _, it := range items {
		if it.Kind == ContentText {
			if len(it.Text) > left {
				if !cut {
					r.b.Warn(budget.WarnClamped, "margin box content")
					cut = true
				}
				end := left
				for end > 0 && !utf8.RuneStart(it.Text[end]) {
					end--
				}
				it.Text = it.Text[:end]
			}
			left -= len(it.Text)
		}
		out = append(out, it)
	}
	return out
}

// maxContentItems bounds the items of a content value.
const maxContentItems = 64

// parseContent parses the content of a margin box: none, normal, or strings
// and counter(page) / counter(pages). Strings pass through dom.SanitizeText:
// they are drawn.
func parseContent(toks []css.Token) (spec, bool) {
	if kw, ok := ident(toks); ok && (kw == "none" || kw == "normal") {
		return spec{none: true}, true
	}
	items := make([]ContentItem, 0, 4)
	for _, f := range fields(trim(toks)) {
		if len(items) >= maxContentItems {
			return spec{}, false
		}
		switch {
		case len(f) == 1 && f[0].Type == css.TString:
			items = append(items, ContentItem{Kind: ContentText, Text: dom.SanitizeText(f[0].Text)})
		case f[0].Type == css.TFunction && eqFold(f[0].Text, "counter"):
			k, ok := counterItem(f)
			if !ok {
				return spec{}, false
			}
			items = append(items, ContentItem{Kind: k})
		default:
			return spec{}, false
		}
	}
	return spec{items: items}, len(items) > 0
}

func counterItem(f []css.Token) (ContentKind, bool) {
	args := f[1:]
	if n := len(args); n > 0 && args[n-1].Type == css.TRParen {
		args = args[:n-1]
	}
	parts := splitOn(args, isComma)
	if len(parts) < 1 || len(parts) > 2 {
		return 0, false
	}
	if len(parts) == 2 {
		if kw, ok := ident(parts[1]); !ok || kw != "decimal" {
			return 0, false
		}
	}
	switch kw, _ := ident(parts[0]); kw {
	case "page":
		return ContentPage, true
	case "pages":
		return ContentPages, true
	}
	return 0, false
}

// HasBoxes reports whether the page has a margin box to draw.
func (p *PageStyle) HasBoxes() bool {
	for _, b := range p.Boxes {
		if b != nil {
			return true
		}
	}
	return false
}

// ContentOnly returns a block style with the text properties of c and nothing
// else: no margin, padding, border, radius or background, automatic width and
// text-align left. A margin box draws its own box model and lays out only its
// text with this style.
func (c *Computed) ContentOnly() *Computed {
	in := *c
	in.Display = DisplayBlock
	in.Margin, in.Padding = [4]Dim{}, [4]Dim{}
	in.Border = [4]Border{}
	in.Radius = [4]Corner{}
	in.BackgroundColor, in.BackgroundImage = css.Color{}, nil
	in.Width = Dim{Kind: DimAuto}
	in.TextAlign = AlignLeft
	return &in
}
