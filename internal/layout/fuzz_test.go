package layout

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

func FuzzLayout(f *testing.F) {
	for _, s := range []string{
		"<p>hello world</p>",
		"<div style='position:fixed;top:0'>A<div style='position:fixed;left:20px'>B<div style='position:fixed;right:0'>C</div></div></div>" + strings.Repeat("<p>x</p>", 30),
		"<div style='position:fixed;top:0;left:0;width:50px;height:50px;transform:rotate(-18deg);z-index:-1'>w</div><p>a</p><p style='break-before:page'>b</p>",
		"<p>x</p><div style='position:fixed;left:1e9px;bottom:-1e9px;width:1e9%'>f<div style='position:fixed;top:1e30px'>g</div></div><div style='break-before:page'>y</div>",
		"<div style='break-inside:avoid;height:90vh'><div style='position:fixed;right:0'>r</div></div><table><tr><td style='position:fixed'>c</td></tr></table>",
		"<svg width=10 height=1e30 viewBox='0 0 1e-9 1'><path d='M0 0 L1 1'/></svg><svg style='display:block;width:1e9px;margin:auto'></svg>",
		"<p>a<svg viewBox='0 0 24 24' stroke=red><rect width=1e300 height=5 rx=1e300 /><circle r=1e9 /></svg>b</p>",
		"<div style='position:relative;overflow:hidden;height:50px'><div style='position:absolute;left:1e7px;top:-1e7px;right:1e7px;bottom:1e9%;width:50%;transform:rotate(3rad);z-index:-5'>x</div><p style='position:relative;top:50%;left:-1e9px'>r</p></div>",
		"<table><tr><td style='position:relative'><div style='position:absolute;bottom:0;right:0;height:200%'>a</div></td></tr></table>",
		"<div style='position:absolute;top:0;margin:auto;height:10px;width:10px;left:0;right:0;bottom:0'><div style='position:absolute;width:5px'></div></div>",
		"<div style='padding:1e30px;margin:-1e30px'>x</div>", "<p style='width:0'>a b c</p>",
		"<div style='border:5px solid;border-radius:999px;overflow:hidden'><p>x</p></div>",
		"<p style='font-size:1e9px;line-height:1e9px'>big</p>", "<ul><li>a<li>b</ul><pre>a\nb</pre>",
		"<div style='break-after:page'>a</div><div style='break-before:page;margin-top:9px'>b</div><p>c</p>",
		"<h2 style='break-after:avoid'>h</h2><h3 style='break-after:avoid'>h</h3><p style='break-inside:avoid'>a<br>b<br>c<br>d<br>e<br>f<br>g<br>h</p>",
		"<table><tr style='break-inside:avoid'><td>a<br>b<br>c<br>d<br>e<br>f<br>g<br>h<br>i<br>j</td></tr><tr style='break-inside:avoid'><td>x</td></tr></table>",
		"<div style='break-inside:avoid'><div style='break-inside:avoid'><h2 style='break-before:avoid'>a</h2><p>b</p></div></div>",
		"<p>a <span style='display:inline-block;padding:2px 9px;margin:0 1e9px 3px;background:red;border-radius:9px'>pill</span> b <span style='display:inline-block;vertical-align:middle'>c d e f</span></p>",
		"<div style='width:50px;white-space:nowrap'><span style='display:inline-block;width:1e9px'>a</span> b c <span style='display:inline-block;vertical-align:top;overflow:hidden'>x<br>y</span></div>",
		"<span style='display:inline-block'><span style='display:inline-block'><span style='display:inline-block;word-break:break-all;overflow-wrap:anywhere'>abc def</span></span><table><tr><td>t</table></span>",
		"<div style='display:flex;flex-wrap:wrap;gap:1e9px 3px;justify-content:space-around;align-items:center'><div style='flex:1 1 1e9px;margin:-1e9px'>a b</div><div style='flex:none;width:1e9px;min-width:0'>c</div><div style='flex:1e9 0 0;max-width:5px'>d<br>e</div></div>",
		"<div style='display:flex'><div style='flex:1;break-inside:avoid'>" + strings.Repeat("<div style='height:0;line-height:90px'>x</div>", 12) + "</div><span style='display:inline-block'>y</span></div><div style='display:flex;column-gap:50%'><div>a</div><div style='display:flex'><div>b</div></div></div>",
		"<div style='display:grid;grid-template-columns:repeat(3,1fr) max-content 20% auto;gap:3px 1e9px'><div>a b c</div><div style='width:1e9px'>d</div><div style='margin:-1e9px'>e<br>f</div><p>g</p>h</div>",
		"<div style='display:grid;grid-template-columns:repeat(100000,1fr 0.1fr);align-items:center'>" + strings.Repeat("<div>x</div>", 50) + "</div>",
		"<div style='display:grid;grid-template-columns:min-content 1fr;break-inside:avoid'><div>aaa bbb</div><div style='break-before:page'>c</div><div>d</div></div>",
		"<table style='table-layout:fixed;width:300px;border-spacing:3px'><colgroup><col style='width:1e7px'><col style='width:150%'><col style='width:0px'><col></colgroup><tr><td>a<td style='width:50%'>b<td>c<td>d</table>",
		"<table style='width:1e9px;border-spacing:1e9px 2px'><colgroup><col style='width:1e6%'><col style='width:10px'><col style='width:30%'></colgroup><tr><td colspan=2 style='width:1e9px'>a b c<td>d</table>",
		"<div style='height:1e9px;border:5px solid'><p style='height:50%'>a b c</p></div><div style='height:7px;overflow:hidden;padding:9px;box-sizing:border-box'>" + strings.Repeat("x y ", 99) + "</div>",
		"<div style='min-width:1e9px;width:5px'>a</div><span style='display:inline-block;min-width:1e9px;max-width:3px'>b</span><div style='min-width:150%;margin:0 auto;display:flex'><div style='min-width:1e7px'>c</div></div>",
		"<p>a <span style='vertical-align:-1e9px'>b<span style='vertical-align:1e9px'>c</span></span> <span style='display:inline-block;vertical-align:1e30px;height:50px'>d</span></p>",
		"<div style='height:200px'><div style='height:100%;margin:-1e9px 0'><div style='height:50%;display:inline-block;width:20px'>x</div></div></div><div style='height:0'><p>a</p><p>b</p></div>",
		"<div style='display:flex;align-items:baseline;height:1e9px'><span style='font-size:1e4px;margin:auto 1e9px auto auto'>a</span><span style='margin-left:auto;min-width:1e9px;height:50%'>b<br>c</span><span style='margin:-1e9px auto;min-width:0'>d</span></div>",
		"<div style='display:flex;flex-wrap:wrap;height:30px;align-items:baseline'><div style='margin:auto;min-width:150%;width:1px'>x y</div><div style='margin-right:auto;height:200%'>z</div><div style='display:flex;height:50%;margin:auto'><i style='height:1e7px'>q</i></div></div>",
		"<table style='height:1e9px;min-width:1e9px'><tr style='height:5px'><td style='width:1e9px;height:1e9px'>a b c<td style='width:150%;min-width:1e7px'>d<td colspan=3 style='width:30%'>e</table>",
		"<table style='width:50%;border-spacing:1e9px 3px'><tr><td colspan=1000 style='width:1e6%'>x<td>y<tr><td style='padding:1e9px'>z<td colspan=2 style='min-width:1e9px'>w</table>",
		"<table><colgroup><col style='width:1e7px'><col span=1000 style='width:0%'></colgroup><tr><td>a<td style='width:0'>b<td style='width:100%'>c<tr style='break-inside:avoid'><td style='height:1e7px'>d</table>",
		"<style>@page a{size:1e9px 5px;margin:1e9px} @page b{margin:0;@top-left{content:'x'}}</style><div style='page:a'>x</div><p style='page:b'>y</p><div style='page:a;break-before:avoid'><p style='page:b'>z</p></div><table style='page:a'><tr><td style='page:b'>q</td></tr></table>",
		"<div style='display:flex;page:a'><div style='page:b'>k</div></div>" + strings.Repeat("<i style='display:block;page:x'>1</i><i style='display:block'>2</i>", 40),
		"<p style='letter-spacing:1e9px'>abc</p>", "<span style='margin:0 1e9px;padding:1e9px'>x</span>",
	} {
		f.Add(s, "p { margin: 3px }")
	}
	f.Fuzz(func(t *testing.T, html, css string) {
		if len(html) > 2048 || len(css) > 1024 || strings.Contains(css, "</") {
			return
		}
		r := newRun(t, "<style>"+css+"</style>"+html, opts{
			extra: "@page { size: 300px 200px; margin: 10px }",
			limits: func(l *budget.Limits) {
				l[budget.Work] = 2_000_000
				l[budget.Pages] = 50
				l[budget.Glyphs] = 200_000
			},
		})
		for range 60 {
			pg, err := r.pag.Next()
			if err != nil {
				var le *budget.LimitError
				if errors.Is(err, io.EOF) || errors.As(err, &le) {
					return
				}
				t.Fatal(err)
			}
			for _, it := range pg.Items {
				checkFinite(t, it)
			}
		}
	})
}
