package htmltopdf

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/layout"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

// FuzzRender: whatever the input, Render does not panic, returns a Result, and
// either succeeds or fails with a documented error. Limits are small so that
// each execution stays cheap.
func FuzzRender(f *testing.F) {
	for _, s := range []string{
		"", "<p>x", reportHTML, "<style>p{color:var(--a)}:root{--a:red}</style><p>x</p>",
		"<table><tr><td>a<td>b</table>", "<div style='width:1e999px;margin:-99999px'>x</div>",
		"<a href='javascript:1'>x</a><img src=x>", "<style>@page{size:1px 1px;margin:99in}</style>x",
		"<p style='font-size:0;line-height:-1;padding:1e9px'>\x00\xff\xfe</p>",
		"<p>a <span style='display:inline-block;padding:2px 6px;border-radius:9px;background:#eee;margin:0 4px 3px 0'>pill</span> <span style='display:inline-block;vertical-align:middle;white-space:nowrap'>b c d</span></p>",
		"<div style='width:40px;white-space:nowrap'><span style='display:inline-block'><span style='display:inline-block;width:1e9px;overflow:hidden'>x<br>y</span></span> z</div><table><tr><td><span style='display:inline-block'>t</span></table>",
		"<div style='display:flex;flex-wrap:wrap;gap:3px 1e9px;justify-content:space-between'><div style='flex:1 1 40px'>a b c</div><div style='flex:none;width:1e9px'>d</div><div style='flex:2 0 0;align-self:center'>e<br>f<br>g</div></div><div style='display:flex'><div style='flex:1'><div style='display:flex'><div>x</div></div></div></div>",
		"<div style='display:grid;grid-template-columns:repeat(3,1fr) max-content;gap:2px 1e9px;align-items:end'><div>a b</div><div style='width:1e9px'>c</div><p>d</p>e<div style='break-inside:avoid'>f<br>g</div></div><div style='display:grid;grid-template-columns:repeat(100000,1fr)'><div>x</div></div>",
		"<style>@page a{size:3in 2in;margin:0;@top-left{content:'x'}} @page{@bottom-right{content:counter(page)}}</style><div style='page:a'>x</div><p>y</p><div style='page:a'>z</div><div style='page:b'>w</div>",
		"<a href='#a'>x</a><a href='#b'>y</a><h2 id=a style='page:p'>A</h2><span id=b style='display:inline-block;height:1e9px'>B</span><a href='#'>z</a><a href='#nothing'>q</a><div style='display:none'><i id=c>c</i></div><a href='#c'>to hidden</a>" + string(bytes.Repeat([]byte("<a href='#a'>r</a>"), 300)),
		"<style>@page{size:landscape;margin:1e9px} @page w{size:5in 1in;@top-left{content:'x'}}</style><p>a</p><div style='page:w'>b</div>",
		"<style>@page{size:1e9px 1px}</style>x",
		"<div style='position:fixed;top:0'>A<div style='position:fixed;left:20px'>B</div></div><style>@page w{size:100px 50px}</style><p>a</p><p style='page:w;break-before:page'>b</p>",
		"<style>.w{position:fixed;top:30%;left:0;right:0;z-index:-1;transform:rotate(-18deg);text-align:center;font-size:40px}</style><div class=w>WM</div>" + string(bytes.Repeat([]byte("<p style='break-after:page'>p</p>"), 20)),
		"<p>a</p><div style='position:fixed;left:0;bottom:-1e9px;width:1e9px'>f<div style='position:fixed;top:1e30px;right:-1e9px'>g</div></div><div style='position:fixed;left:1px'>s</div><p style='break-before:page'>b</p>" + string(bytes.Repeat([]byte("<i style='position:fixed;top:0'>x</i>"), 200)),
		"<title>\x00)>></title><body>\u202e\ufffe", "<div><div><div>" + string(bytes.Repeat([]byte("<b>"), 2000)),
		"<div style='columns:1000;column-gap:0;column-rule:2px solid red'><p>aaa bbb ccc ddd</p></div><div style='columns:1e9;column-gap:1e9px;column-rule:1e9px dotted #00f'><p>x y z</p></div>",
		"<div style='columns:3;column-gap:4px;column-rule:1px solid'><table><thead><tr><th>h</th></tr></thead>" + string(bytes.Repeat([]byte("<tr><td>r<br>s<br>t</td></tr>"), 60)) + "</table></div>",
		"<div style='columns:2'><div style='columns:3'><p>nested a b c d e f</p><div style='columns:2'><p>deeper</p></div></div><p style='break-before:page'>x</p><p style='break-after:page'>y</p></div>",
		"<div style='columns:3;column-rule:1px solid red'>" + reportHTML + "</div>",
		"<p style='margin-left:-1e9px;position:relative;left:1e30px'>x y z</p>",
		"<style>@page{size:1px 1px;margin:0}</style><div style='position:fixed;top:-1e9px;right:-1e9px'>a</div><p style='break-after:page'>b</p><p>c</p>",
		"<div style='position:absolute;top:1e308px;left:-1e308px'>x</div><div style='transform:rotate(1deg);position:absolute;left:1e9px'>y</div>",
		"<div style='overflow:hidden;width:0;height:0'><p style='margin-left:1e7px'>x</p></div><p style='color:transparent;margin-left:9e6px'>t</p>",
	} {
		f.Add([]byte(s))
	}
	hc := testcorpus.Hostile(budget.Defaults())
	for i, tc := range hc {
		if i%6 == 0 {
			if h := tc.HTML(); len(h) < 64<<10 {
				f.Add(h)
			}
		}
	}
	for _, tc := range testcorpus.OffPageCases() {
		if h := tc.HTML(); len(h) < 64<<10 {
			f.Add(h)
		}
	}
	c, err := New(Options{Limits: Limits{
		MaxInputBytes: 256 << 10, MaxNodes: 5000, MaxCSSBytes: 64 << 10, MaxSelectors: 500, MaxStyles: 2000,
		MaxBoxes: 10_000, MaxGlyphs: 100_000, MaxPages: 20, MaxOutputBytes: 8 << 20, MaxWork: 5_000_000,
		Timeout: 5 * time.Second,
	}})
	if err != nil {
		f.Fatal(err)
	}
	// The paper alternates with the @page sizes: the extremes of Options.PageSize.
	paper, err := New(Options{PageSize: PageSize{Width: MinPageSide, Height: MaxPageSide}, Limits: Limits{
		MaxInputBytes: 256 << 10, MaxNodes: 5000, MaxCSSBytes: 64 << 10, MaxSelectors: 500, MaxStyles: 2000,
		MaxBoxes: 10_000, MaxGlyphs: 100_000, MaxPages: 20, MaxOutputBytes: 8 << 20, MaxWork: 5_000_000,
		Timeout: 5 * time.Second,
	}})
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var buf bytes.Buffer
		conv := c
		if len(data)%2 == 1 {
			conv = paper
		}
		var tree *box.Box
		var pages []*layout.Page
		hooked := *conv
		hooked.pageHook = func(root *box.Box, pg *layout.Page) { tree = root; pages = append(pages, pg) }
		res, err := hooked.Render(context.Background(), bytes.NewReader(data), &buf)
		if res == nil {
			t.Fatal("nil Result")
		}
		checkOffPageWarnings(t, res.Warnings)
		switch {
		case err == nil:
			if lost := layout.TextLost(tree, pages); lost != "" {
				t.Fatalf("text lost: %s", lost)
			}
			if res.Pages < 1 || res.Bytes != int64(buf.Len()) || !bytes.HasPrefix(buf.Bytes(), []byte("%PDF-")) || !bytes.HasSuffix(bytes.TrimSpace(buf.Bytes()), []byte("%%EOF")) {
				t.Fatalf("bad success: %+v, %d bytes", res, buf.Len())
			}
		case errors.Is(err, ErrLimit), errors.Is(err, context.DeadlineExceeded):
		default:
			t.Fatalf("unexpected error: %v", err)
		}
		_ = io.Discard
	})
}

// checkOffPageWarnings: the Detail of text-off-page is one of the four sides,
// never document text, with a positive count, once per side.
func checkOffPageWarnings(t *testing.T, ws []Warning) {
	t.Helper()
	sides := map[string]bool{}
	for _, w := range ws {
		if w.Code != WarnTextOffPage {
			continue
		}
		switch w.Detail {
		case "left", "right", "above", "below":
		default:
			t.Fatalf("text-off-page Detail %q is not a side", w.Detail)
		}
		if w.Count < 1 {
			t.Fatalf("text-off-page %q: count %d", w.Detail, w.Count)
		}
		if sides[w.Detail] {
			t.Fatalf("text-off-page %q reported twice", w.Detail)
		}
		sides[w.Detail] = true
	}
}
