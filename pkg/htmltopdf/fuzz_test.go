package htmltopdf

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"
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
		"<title>\x00)>></title><body>\u202e\ufffe", "<div><div><div>" + string(bytes.Repeat([]byte("<b>"), 2000)),
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
		res, err := conv.Render(context.Background(), bytes.NewReader(data), &buf)
		if res == nil {
			t.Fatal("nil Result")
		}
		switch {
		case err == nil:
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
