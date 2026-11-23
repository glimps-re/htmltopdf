package style

import (
	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
)

// UASource is the user-agent stylesheet: the subset of HTML rendering that the
// converter implements, with Chrome's values. Embedded and form elements are
// display: none (the resolver warns about them).
const UASource = `
html, body, address, article, aside, blockquote, center, details, dialog, dir, div, dl, dd, dt,
fieldset, figcaption, figure, footer, form, header, hgroup, hr, legend, listing, main, menu, nav,
ol, p, plaintext, pre, section, summary, ul, h1, h2, h3, h4, h5, h6, xmp { display: block }

area, base, datalist, head, link, meta, noscript, param, rp, script, source, style, template, title, track,
audio, button, canvas, embed, iframe, img, input, object, select, textarea, video { display: none }

li { display: list-item }
table { display: table; border-collapse: separate; border-spacing: 2px; box-sizing: border-box }
thead { display: table-header-group; vertical-align: middle }
tbody { display: table-row-group; vertical-align: middle }
tfoot { display: table-footer-group; vertical-align: middle }
tr { display: table-row; vertical-align: inherit }
td, th { display: table-cell; vertical-align: inherit; padding: 1px }
th { font-weight: bold; text-align: center }
col { display: table-column }
colgroup { display: table-column-group }
caption { display: table-caption; text-align: center }

body { margin: 8px }
p, dl, menu, ol, ul { margin: 1em 0 }
blockquote, figure { margin: 1em 40px }
dd { margin-left: 40px }
ol, ul, menu, dir { padding-left: 40px }
ol ol, ol ul, ul ol, ul ul { margin-top: 0; margin-bottom: 0 }
center { text-align: center }
hr { margin: 0.5em auto; border: 1px solid #9a9a9a }

h1 { font-size: 2em; margin: 0.67em 0; font-weight: bold }
h2 { font-size: 1.5em; margin: 0.83em 0; font-weight: bold }
h3 { font-size: 1.17em; margin: 1em 0; font-weight: bold }
h4 { font-size: 1em; margin: 1.33em 0; font-weight: bold }
h5 { font-size: 0.83em; margin: 1.67em 0; font-weight: bold }
h6 { font-size: 0.67em; margin: 2.33em 0; font-weight: bold }

b, strong { font-weight: bold }
i, em, cite, var, dfn, address { font-style: italic }
u, ins { text-decoration: underline }
s, strike, del { text-decoration: line-through }
small { font-size: smaller }
big { font-size: larger }
mark { background-color: yellow; color: black }
code, kbd, samp, tt, pre, listing, xmp, plaintext { font-family: monospace }
pre, listing, xmp, plaintext { white-space: pre }
a[href] { color: #0000ee; text-decoration: underline }
`

// CompileUA parses and compiles UASource. b is a dedicated budget: the sheet is
// built once and shared, and any warning recorded in b is a bug of UASource.
func CompileUA(b *budget.Budget) (*Sheet, error) {
	ss, err := css.ParseStylesheet([]byte(UASource), css.OriginUA, b)
	if err != nil {
		return nil, err
	}
	return Compile(ss, b)
}
