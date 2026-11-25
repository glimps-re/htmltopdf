package htmltopdf

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

// chrome09's destinations of 02 small (pdfinfo -dests of
// tmp/spike/out/chrome09/report_02-executive_small.pdf, the /XYZ arrays): page
// (0-based) and y in points, in the order the table of contents links to them. Chrome writes y as
// the page height less the offset in the page area, unscaled.
var chrome02SDests = []struct {
	page int
	y    float64
}{{2, 841.92}, {2, 460.17}, {3, 792.42}, {3, 118.92}, {4, 697.92}}

// 02 small has 12 link annotations (5 in the table of contents, 7 to the
// analysis) and 5 destinations on the pages of the findings, within 1 pt of
// Chrome: the 0.7 and 0.96 pt of the two lowest are the line metrics of M3-12.
func TestM310LinksAndDestinationsOf02S(t *testing.T) {
	_, _, doc := renderM2(t, "02-executive", testcorpus.Small)
	links, internal := 0, 0
	for _, p := range doc.Pages {
		for _, l := range p.Links {
			links++
			if l.Dest != "" {
				internal++
			}
		}
	}
	if links != 12 || internal != 5 {
		t.Errorf("%d links, %d internal; Chrome 12 and 5", links, internal)
	}
	if len(doc.Dests) != len(chrome02SDests) {
		t.Fatalf("%d destinations: %+v", len(doc.Dests), doc.Dests)
	}
	// The names are /D<n> in the order the links first use them.
	for i, want := range chrome02SDests {
		d, ok := doc.Dests["D"+string(rune('0'+i))]
		if !ok || d.Page != want.page || math.Abs(d.Y-want.y) > 1.0 || d.X != 0 {
			t.Errorf("D%d: %+v, Chrome page %d y %.2f", i, d, want.page+1, want.y)
		}
	}
	for i, p := range doc.Pages {
		if i > 0 && len(p.Links) > 0 && p.Links[0].Dest != "" && i != 1 {
			t.Errorf("internal links on page %d", i+1)
		}
	}
}

// 10 000 distinct internal targets are kept; the 10 001st link is dropped with a
// clamped warning (pdf.MaxDests). 1-minute guard.
func TestM310DestinationLimit(t *testing.T) {
	start := time.Now()
	doc := func(n int) string {
		var sb strings.Builder
		sb.WriteString("<style>@page{size:2000px 14000px;margin:0} body{margin:0} a,div{font-size:1px;line-height:1px}</style>")
		for i := range n {
			fmt.Fprintf(&sb, `<a href="#t%d">x</a> `, i)
		}
		for i := range n {
			fmt.Fprintf(&sb, `<div id="t%d">y</div>`, i)
		}
		return sb.String()
	}
	for _, n := range []int{10000, 10001} {
		res, data := renderBytes(t, mustNew(t, Options{Limits: Limits{MaxNodes: 100000}}), doc(n))
		d := pdftest.Validate(t, data)
		links, dests := 0, len(d.Dests)
		for _, p := range d.Pages {
			links += len(p.Links)
		}
		clamped := false
		for _, w := range res.Warnings {
			clamped = clamped || (w.Code == "clamped" && w.Detail == "destinations")
		}
		if want := min(n, 10000); links != want || dests != want || clamped != (n > 10000) {
			t.Errorf("%d targets: %d links, %d destinations, warned %v", n, links, dests, clamped)
		}
	}
	if time.Since(start) > time.Minute {
		t.Error("hang")
	}
}

// 25 000 links render: the writer keeps MaxLinks of them and warns, and the render
// never fails on links alone.
func TestM310LinkLimit(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("<style>@page{size:2000px 14000px;margin:0} body{margin:0} a{font-size:1px;line-height:1px}</style>")
	for i := range 25000 {
		if i%2 == 0 {
			sb.WriteString(`<a href="https://e.example/">x</a> `)
		} else {
			fmt.Fprintf(&sb, `<a href="#t%d">y</a> `, i%50)
		}
	}
	for i := range 50 {
		fmt.Fprintf(&sb, `<div id="t%d">z</div>`, i)
	}
	res, data := renderBytes(t, mustNew(t, Options{Limits: Limits{MaxNodes: 200000}}), sb.String())
	d := pdftest.Validate(t, data)
	links := 0
	for _, p := range d.Pages {
		links += len(p.Links)
	}
	warned := false
	for _, w := range res.Warnings {
		warned = warned || (w.Code == "clamped" && w.Detail == "links")
	}
	if links != 20000 || !warned {
		t.Errorf("%d links, warned %v", links, warned)
	}
}
