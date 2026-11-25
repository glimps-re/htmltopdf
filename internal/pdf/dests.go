package pdf

import (
	"strconv"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

// MaxDests bounds the distinct internal destinations of a document, MaxLinks its
// link annotations (URI and internal) and MaxDestIDBytes the id of a target.
const (
	MaxDests       = 10000
	MaxLinks       = 20000
	MaxDestIDBytes = 2048
)

// destPos is a destination: where it is placed, or not yet.
type destPos struct {
	placed bool
	page   Ref
	x, y   float64
}

// destTable holds the named destinations in the order they were first used.
type destTable struct {
	ids map[string]int
	pos []destPos
}

// Dest returns the PDF name of the destination of the internal target id,
// allocating it on first use: "/D<n>", never the id (it comes from the
// document). ok is false once MaxDests names exist, or for an id over
// MaxDestIDBytes.
func (w *Writer) Dest(id string) (name string, ok bool) {
	if len(id) > MaxDestIDBytes {
		return "", false
	}
	i, found := w.dests.ids[id]
	if !found {
		if len(w.dests.pos) >= MaxDests {
			return "", false
		}
		if w.dests.ids == nil {
			w.dests.ids = make(map[string]int)
		}
		i = len(w.dests.pos)
		w.dests.ids[id] = i
		w.dests.pos = append(w.dests.pos, destPos{})
	}
	return "/D" + strconv.Itoa(i), true
}

// SetDest places the destination of id on page at (x, y) in points: the first
// call wins. An id past MaxDests is ignored.
func (w *Writer) SetDest(id string, page Ref, x, y float64) error {
	if !validNum(x) || !validNum(y) {
		return ErrInvalidLink
	}
	if _, ok := w.Dest(id); !ok {
		return nil
	}
	if p := &w.dests.pos[w.dests.ids[id]]; !p.placed {
		*p = destPos{placed: true, page: page, x: x, y: y}
	}
	return nil
}

// closeDests writes the /Dests dictionary and returns its reference, 0 when
// there is none. A destination never placed points to the top of the first page.
func (w *Writer) closeDests() (Ref, error) {
	if len(w.dests.pos) == 0 {
		return 0, nil
	}
	ref := w.reserve()
	w.obj = append(w.obj[:0], "<<"...)
	var err error
	for i, p := range w.dests.pos {
		if !p.placed {
			p = destPos{page: w.pages[0], y: w.firstHeight}
		}
		w.obj = append(w.obj, " /D"...)
		w.obj = strconv.AppendInt(w.obj, int64(i), 10)
		w.obj = append(w.obj, " ["...)
		w.obj = appendRef(w.obj, p.page)
		w.obj = append(w.obj, " /XYZ "...)
		if w.obj, err = appendNum(w.obj, p.x); err != nil {
			return 0, err
		}
		w.obj = append(w.obj, ' ')
		if w.obj, err = appendNum(w.obj, p.y); err != nil {
			return 0, err
		}
		w.obj = append(w.obj, " null]"...)
	}
	w.obj = append(w.obj, " >>"...)
	return ref, w.emit(ref, w.obj)
}

// warnDests records a link dropped for want of a destination name.
func (w *Writer) warnDests() { w.out.b.Warn(budget.WarnClamped, "destinations") }

// warnLinks records a link dropped past MaxLinks.
func (w *Writer) warnLinks() { w.out.b.Warn(budget.WarnClamped, "links") }
