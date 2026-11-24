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
