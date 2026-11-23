package layout

import (
	"math"

	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/style"
)

// emit appends the display items of f and its descendants.
func emit(f *frag, out display.List) display.List {
	st := f.box.Style
	r := f.rect
	rad := radiiOf(st, r, f.first, f.last)
	out = appendBackground(out, st, r, rad)
	widths := f.border
	if !f.first {
		widths[style.Top] = 0
	}
	if !f.last {
		widths[style.Bottom] = 0
	}
	n := len(out)
	out = appendBorder(out, st, r, rad, widths)
	if f.borderColor != nil && len(out) > n {
		out = recolorBorder(out, f.borderColor)
	}
	clip := st.OverflowHidden && r.W > 0 && r.H > 0
	if clip {
		out = append(out, display.PushClip{Rect: paddingBox(f), Radii: innerRadii(rad, f)})
	}
	for _, c := range f.children {
		out = emit(c, out)
	}
	for _, ln := range f.lines {
		out = emitLine(ln, out)
	}
	if clip {
		out = append(out, display.PopClip{})
	}
	return out
}

// radiiOf resolves the border radii of a fragment: the top corners belong to the
// first fragment and the bottom ones to the last. They are scaled down so that
// adjacent radii never exceed a side (CSS Backgrounds §5.5).
func radiiOf(st *style.Computed, r display.Rect, first, last bool) display.Radii {
	var rad display.Radii
	for i := range rad {
		if (i == style.TopLeft || i == style.TopRight) && !first || (i == style.BottomLeft || i == style.BottomRight) && !last {
			continue
		}
		c := st.Radius[i]
		rad[i].H = math.Max(0, px(c.H, r.W))
		rad[i].V = math.Max(0, px(c.V, r.H))
	}
	f := 1.0
	scale := func(side, sum float64) {
		if sum > side && sum > 0 {
			f = math.Min(f, side/sum)
		}
	}
	scale(r.W, rad[style.TopLeft].H+rad[style.TopRight].H)
	scale(r.W, rad[style.BottomLeft].H+rad[style.BottomRight].H)
	scale(r.H, rad[style.TopLeft].V+rad[style.BottomLeft].V)
	scale(r.H, rad[style.TopRight].V+rad[style.BottomRight].V)
	if f < 1 {
		for i := range rad {
			rad[i].H *= f
			rad[i].V *= f
		}
	}
	return rad
}

// appendBackground adds the background color and gradient of a box.
func appendBackground(out display.List, st *style.Computed, r display.Rect, rad display.Radii) display.List {
	if r.W <= 0 || r.H <= 0 {
		return out
	}
	if st.BackgroundColor.A > 0 {
		out = append(out, display.FillRect{Rect: r, Radii: rad, Color: st.BackgroundColor})
	}
	if st.BackgroundImage != nil {
		out = append(out, display.FillGradient{Rect: r, Radii: rad, Gradient: *st.BackgroundImage})
	}
	return out
}

// appendBorder adds the border of a box with the given side widths (top, right,
// bottom, left; 0 for a side the fragment does not draw), if any is visible.
func appendBorder(out display.List, st *style.Computed, r display.Rect, rad display.Radii, widths [4]float64) display.List {
	if r.W <= 0 || r.H <= 0 {
		return out
	}
	b := display.Border{Rect: r, Radii: rad}
	visible := false
	for i, w := range widths {
		if w <= 0 {
			continue
		}
		b.Sides[i] = display.BorderSide{Width: w, Color: st.Border[i].Color}
		visible = true
	}
	if !visible {
		return out
	}
	return append(out, b)
}

// paddingBox is the border box of f minus the borders it draws.
func paddingBox(f *frag) display.Rect {
	r := f.rect
	top, bottom := f.border[style.Top], f.border[style.Bottom]
	if !f.first {
		top = 0
	}
	if !f.last {
		bottom = 0
	}
	left, right := f.border[style.Left], f.border[style.Right]
	return display.Rect{
		X: r.X + left, Y: r.Y + top,
		W: math.Max(0, r.W-left-right), H: math.Max(0, r.H-top-bottom),
	}
}

// innerRadii shrinks the outer radii by the border widths.
func innerRadii(rad display.Radii, f *frag) display.Radii {
	var in display.Radii
	w := [4][2]float64{
		style.TopLeft:     {f.border[style.Left], f.border[style.Top]},
		style.TopRight:    {f.border[style.Right], f.border[style.Top]},
		style.BottomRight: {f.border[style.Right], f.border[style.Bottom]},
		style.BottomLeft:  {f.border[style.Left], f.border[style.Bottom]},
	}
	for i := range in {
		in[i].H = math.Max(0, rad[i].H-w[i][0])
		in[i].V = math.Max(0, rad[i].V-w[i][1])
	}
	return in
}

// emitLine appends the inline box backgrounds, the text and the links of a line.
func emitLine(ln *lineFrag, out display.List) display.List {
	out = emitInlineBoxes(ln, out)
	for i := range ln.items {
		if t := ln.items[i].text; t != nil {
			out = append(out, t)
		}
	}
	return emitLinks(ln, out)
}

// emitInlineBoxes paints the background and borders of the inline boxes whose
// edges are on the line: from the start edge (or the line start) to the end
// edge (or the line end).
func emitInlineBoxes(ln *lineFrag, out display.List) display.List {
	var stack []*lineItem
	left, right := ln.rect.X, ln.rect.Right()
	for i := range ln.items {
		it := &ln.items[i]
		switch it.kind {
		case itemOpen:
			stack = append(stack, it)
		case itemClose:
			var start *lineItem
			for j := len(stack) - 1; j >= 0; j-- {
				if stack[j].box == it.box {
					start = stack[j]
					stack = append(stack[:j], stack[j+1:]...)
					break
				}
			}
			x0 := left
			if start != nil {
				x0 = start.x + start.margin
			}
			out = inlineBox(out, it.box.Style, ln, x0, it.x+it.w-it.margin, start != nil, true, it)
		}
	}
	for _, it := range stack {
		out = inlineBox(out, it.box.Style, ln, it.x+it.margin, right, true, false, it)
	}
	return out
}

func inlineBox(out display.List, st *style.Computed, ln *lineFrag, x0, x1 float64, hasStart, hasEnd bool, it *lineItem) display.List {
	r := display.Rect{X: x0, Y: ln.baseline - it.ascent, W: x1 - x0, H: it.ascent + it.descent}
	out = appendBackground(out, st, r, display.Radii{})
	widths := [4]float64{
		st.Border[style.Top].Width, st.Border[style.Right].Width, st.Border[style.Bottom].Width, st.Border[style.Left].Width,
	}
	if !hasStart {
		widths[style.Left] = 0
	}
	if !hasEnd {
		widths[style.Right] = 0
	}
	return appendBorder(out, st, r, display.Radii{}, widths)
}

// emitLinks adds one link per run of consecutive items with the same target.
func emitLinks(ln *lineFrag, out display.List) display.List {
	var (
		href   string
		x0, x1 float64
		asc    float64
		desc   float64
	)
	flush := func() {
		if href != "" && x1 > x0 {
			out = append(out, display.Link{Rect: display.Rect{X: x0, Y: ln.baseline - asc, W: x1 - x0, H: asc + desc}, URI: href})
		}
		href = ""
	}
	for i := range ln.items {
		it := &ln.items[i]
		if it.kind != itemText {
			continue
		}
		if it.href != href {
			flush()
			href, x0, x1, asc, desc = it.href, it.x, it.x, 0, 0
		}
		x1 = it.x + it.w
		asc, desc = math.Max(asc, it.ascent), math.Max(desc, it.descent)
	}
	flush()
	return out
}
