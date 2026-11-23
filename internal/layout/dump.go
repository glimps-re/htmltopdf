package layout

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// Dump writes a text form of the page for golden tests: the page geometry, then
// (with Config.KeepTree) one line per block fragment and per line box, with their rectangles in layout
// px rounded to 0.01.
func Dump(w io.Writer, pg *Page) error {
	var sb strings.Builder
	fmt.Fprintf(&sb, "page %d size %sx%s area %s,%s %sx%s scale %s\n", pg.Number,
		num(pg.Width), num(pg.Height), num(pg.Area.X), num(pg.Area.Y), num(pg.Area.W), num(pg.Area.H), num(pg.Scale))
	if pg.root != nil {
		dumpFrag(&sb, pg.root, 1)
	}
	if _, err := io.WriteString(w, sb.String()); err != nil {
		return fmt.Errorf("layout: dump: %w", err)
	}
	return nil
}

func num(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64)
}

func dumpFrag(sb *strings.Builder, f *frag, depth int) {
	ind := strings.Repeat("  ", depth)
	name := "block"
	if f.box.Anonymous {
		name = "anon"
	}
	if f.box.Tag != 0 {
		name += " " + f.box.Tag.String()
	}
	flags := ""
	if !f.first {
		flags += " cont"
	}
	if !f.last {
		flags += " more"
	}
	fmt.Fprintf(sb, "%s%s %s,%s %sx%s%s\n", ind, name, num(f.rect.X), num(f.rect.Y), num(f.rect.W), num(f.rect.H), flags)
	for _, c := range f.children {
		dumpFrag(sb, c, depth+1)
	}
	for _, ln := range f.lines {
		var t strings.Builder
		for _, it := range ln.items {
			if it.text != nil {
				t.WriteString(it.text.Text)
			}
		}
		fmt.Fprintf(sb, "%s  line %s,%s %sx%s base %s %q\n", ind, num(ln.rect.X), num(ln.rect.Y), num(ln.rect.W), num(ln.rect.H), num(ln.baseline), t.String())
	}
}
