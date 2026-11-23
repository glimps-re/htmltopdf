package box

import (
	"strconv"
	"strings"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/dom"
	"github.com/glimps-re/htmltopdf/internal/style"
)

// MaxColSpan is the largest colspan, also the largest number of table columns.
const MaxColSpan = 1000

// cellSpans sets ColSpan from the colspan attribute: [1, MaxColSpan], 1 when
// absent or invalid. A rowspan above 1 is rendered as 1 and warned.
func (bd *builder) cellSpans(el *dom.Element, bx *Box) {
	bx.ColSpan = 1
	if v, ok := el.Attr("colspan"); ok {
		n, clamped := parseSpan(v)
		bx.ColSpan = n
		if clamped {
			bd.b.Warn(budget.WarnClamped, "colspan")
		}
	}
	if v, ok := el.Attr("rowspan"); ok {
		if n, _ := parseSpan(v); n > 1 {
			bd.b.Warn(budget.WarnClamped, "rowspan")
		}
	}
}

// parseSpan reads an HTML span attribute: the leading digits, at least 1, at
// most MaxColSpan; clamped tells that a larger value was reduced.
func parseSpan(v string) (n int, clamped bool) {
	v = strings.TrimLeft(v, " \t\n\f\r")
	end := 0
	for end < len(v) && v[end] >= '0' && v[end] <= '9' {
		end++
	}
	if end == 0 {
		return 1, false
	}
	digits := strings.TrimLeft(v[:end], "0")
	if len(digits) > 4 {
		return MaxColSpan, true
	}
	n, err := strconv.Atoi(digits)
	switch {
	case err != nil || n < 1:
		return 1, false
	case n > MaxColSpan:
		return MaxColSpan, true
	}
	return n, false
}

// tableStyles holds the anonymous styles a fix-up needs, made on first use.
type tableStyles struct {
	parent *style.Computed
	row    *style.Computed
	cell   *style.Computed
	block  *style.Computed
}

func (bd *builder) anonStyle(slot **style.Computed, parent *style.Computed, d style.Display) (*style.Computed, error) {
	if *slot == nil {
		cs, err := bd.st.Anonymous(parent, d)
		if err != nil {
			return nil, err
		}
		*slot = cs
	}
	return *slot, nil
}

func (bd *builder) anon(slot **style.Computed, parent *style.Computed, d style.Display, children []*Box) (*Box, error) {
	cs, err := bd.anonStyle(slot, parent, d)
	if err != nil {
		return nil, err
	}
	if err := bd.chargeBox(); err != nil {
		return nil, err
	}
	return &Box{Kind: KindBlock, Anonymous: true, Style: cs, Children: children, ColSpan: 1}, nil
}

func isPart(c *Box, ds ...style.Display) bool {
	if c.Kind != KindBlock || c.Anonymous {
		return false
	}
	for _, d := range ds {
		if c.Style.Display == d {
			return true
		}
	}
	return false
}

func isGroup(c *Box) bool {
	return isPart(c, style.DisplayTableRowGroup, style.DisplayTableHeaderGroup, style.DisplayTableFooterGroup)
}

func isColumn(c *Box) bool {
	return isPart(c, style.DisplayTableColumn, style.DisplayTableColumnGroup)
}

// fixTable repairs the children of a table box (CSS 2.1 §17.2.1): row groups,
// rows and captions stay, columns are dropped (widths come in M2), anything
// else is wrapped in anonymous rows; every row holds cells only and every row
// group rows only. Cell contents are not touched.
func (bd *builder) fixTable(tb *Box) error {
	ts := &tableStyles{parent: tb.Style}
	out := make([]*Box, 0, len(tb.Children))
	var loose []*Box
	flush := func() error {
		if len(loose) == 0 {
			return nil
		}
		if allBlank(loose) {
			loose = nil
			return nil
		}
		row, err := bd.looseRow(ts, loose)
		loose = nil
		if err != nil {
			return err
		}
		out = append(out, row)
		return nil
	}
	for _, c := range tb.Children {
		if err := bd.b.Charge(budget.Work, 1); err != nil {
			return err
		}
		switch {
		case isColumn(c):
			if err := flush(); err != nil {
				return err
			}
		case isPart(c, style.DisplayTableCaption):
			if err := flush(); err != nil {
				return err
			}
			out = append(out, c)
		case isGroup(c):
			if err := flush(); err != nil {
				return err
			}
			if err := bd.fixGroup(ts, c); err != nil {
				return err
			}
			out = append(out, c)
		case isPart(c, style.DisplayTableRow):
			if err := flush(); err != nil {
				return err
			}
			if err := bd.fixRow(ts, c); err != nil {
				return err
			}
			out = append(out, c)
		default:
			loose = append(loose, c)
		}
	}
	if err := flush(); err != nil {
		return err
	}
	tb.Children = out
	return nil
}

func (bd *builder) fixGroup(ts *tableStyles, g *Box) error {
	out := make([]*Box, 0, len(g.Children))
	var loose []*Box
	flush := func() error {
		if len(loose) == 0 {
			return nil
		}
		if allBlank(loose) {
			loose = nil
			return nil
		}
		row, err := bd.looseRow(ts, loose)
		loose = nil
		if err != nil {
			return err
		}
		out = append(out, row)
		return nil
	}
	for _, c := range g.Children {
		if err := bd.b.Charge(budget.Work, 1); err != nil {
			return err
		}
		switch {
		case isColumn(c):
			if err := flush(); err != nil {
				return err
			}
		case isPart(c, style.DisplayTableRow):
			if err := flush(); err != nil {
				return err
			}
			if err := bd.fixRow(ts, c); err != nil {
				return err
			}
			out = append(out, c)
		default:
			loose = append(loose, c)
		}
	}
	if err := flush(); err != nil {
		return err
	}
	g.Children = out
	return nil
}

// looseRow wraps boxes that cannot sit in a table or a row group in an
// anonymous row.
func (bd *builder) looseRow(ts *tableStyles, loose []*Box) (*Box, error) {
	row, err := bd.anon(&ts.row, ts.parent, style.DisplayTableRow, loose)
	if err != nil {
		return nil, err
	}
	return row, bd.fixRow(ts, row)
}

// fixRow wraps the runs of non-cell children of a row in anonymous cells.
func (bd *builder) fixRow(ts *tableStyles, r *Box) error {
	out := make([]*Box, 0, len(r.Children))
	var loose []*Box
	flush := func() error {
		if len(loose) == 0 {
			return nil
		}
		if allBlank(loose) {
			loose = nil
			return nil
		}
		cell, err := bd.anon(&ts.cell, ts.parent, style.DisplayTableCell, nil)
		if err != nil {
			return err
		}
		cell.Children, err = bd.blockify(ts, loose)
		loose = nil
		if err != nil {
			return err
		}
		out = append(out, cell)
		return nil
	}
	for _, c := range r.Children {
		if err := bd.b.Charge(budget.Work, 1); err != nil {
			return err
		}
		if isPart(c, style.DisplayTableCell) {
			if err := flush(); err != nil {
				return err
			}
			out = append(out, c)
			continue
		}
		if isColumn(c) {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		loose = append(loose, c)
	}
	if err := flush(); err != nil {
		return err
	}
	r.Children = out
	return nil
}

// blockify returns boxes fit to be the children of a block: when they mix
// blocks and inline-level boxes, the inline-level runs go in anonymous blocks.
func (bd *builder) blockify(ts *tableStyles, boxes []*Box) ([]*Box, error) {
	hasBlock, hasInline := false, false
	for _, b := range boxes {
		if b.Kind == KindBlock {
			hasBlock = true
		} else {
			hasInline = true
		}
	}
	if !hasBlock || !hasInline {
		return boxes, nil
	}
	out := make([]*Box, 0, len(boxes))
	var run []*Box
	flush := func() error {
		if len(run) == 0 {
			return nil
		}
		a, err := bd.anon(&ts.block, ts.parent, style.DisplayBlock, run)
		run = nil
		if err != nil {
			return err
		}
		out = append(out, a)
		return nil
	}
	for _, b := range boxes {
		if b.Kind != KindBlock {
			run = append(run, b)
			continue
		}
		if err := flush(); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, flush()
}

// blank reports whether b only holds white space: such boxes between table
// parts are ignored (CSS 2.1 §17.2.1), also under white-space: pre.
func blank(b *Box) bool {
	switch {
	case b.Kind == KindText:
		return strings.Trim(b.Text, " \t\n\f\r") == ""
	case b.Kind == KindLineBreak:
		// A preserved newline has no tag; a br element does.
		return b.Tag == 0
	case b.Kind == KindBlock && b.Anonymous, b.Kind == KindInline && b.Tag == 0:
		for _, c := range b.Children {
			if !blank(c) {
				return false
			}
		}
		return true
	}
	return false
}

func allBlank(boxes []*Box) bool {
	for _, b := range boxes {
		if !blank(b) {
			return false
		}
	}
	return true
}

func isTablePart(c *Box) bool {
	if c.Kind != KindBlock || c.Anonymous {
		return false
	}
	switch c.Style.Display {
	case style.DisplayTableRowGroup, style.DisplayTableHeaderGroup, style.DisplayTableFooterGroup,
		style.DisplayTableRow, style.DisplayTableCell, style.DisplayTableColumn, style.DisplayTableColumnGroup:
		return true
	}
	return false
}

// wrapOrphans wraps the runs of table parts that are not in a table (a cell in
// a div) in anonymous tables, which are then fixed up (CSS 2.1 §17.2.1, rule 3).
// White space between two parts joins the run; a run made only of columns
// (which the fix-up drops) is left alone.
func (bd *builder) wrapOrphans(c *Box) error {
	hasPart := false
	for _, ch := range c.Children {
		if isTablePart(ch) {
			hasPart = true
			break
		}
	}
	if !hasPart {
		return nil
	}
	out := make([]*Box, 0, len(c.Children))
	var run, pending []*Box // pending: blank boxes that join the run if a part follows
	flush := func() error {
		defer func() { run, pending = nil, nil }()
		if len(run) > 0 {
			if onlyColumns(run) {
				out = append(out, run...)
			} else {
				var slot *style.Computed
				tb, err := bd.anon(&slot, c.Style, style.DisplayTable, run)
				if err != nil {
					return err
				}
				if err := bd.fixTable(tb); err != nil {
					return err
				}
				out = append(out, tb)
			}
		}
		out = append(out, pending...)
		return nil
	}
	for _, ch := range c.Children {
		switch {
		case isTablePart(ch):
			run = append(run, pending...)
			pending = nil
			run = append(run, ch)
		case len(run) > 0 && blank(ch):
			pending = append(pending, ch)
		default:
			if err := flush(); err != nil {
				return err
			}
			out = append(out, ch)
		}
	}
	if err := flush(); err != nil {
		return err
	}
	c.Children = out
	return nil
}

// onlyColumns reports whether every box is a column or a column group.
func onlyColumns(boxes []*Box) bool {
	for _, b := range boxes {
		if !isColumn(b) {
			return false
		}
	}
	return true
}

func isTableContainer(d style.Display) bool {
	switch d {
	case style.DisplayTable, style.DisplayTableRowGroup, style.DisplayTableHeaderGroup, style.DisplayTableFooterGroup, style.DisplayTableRow:
		return true
	}
	return false
}
