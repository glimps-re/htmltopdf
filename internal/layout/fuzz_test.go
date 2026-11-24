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
		"<p>hello world</p>", "<div style='padding:1e30px;margin:-1e30px'>x</div>", "<p style='width:0'>a b c</p>",
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
