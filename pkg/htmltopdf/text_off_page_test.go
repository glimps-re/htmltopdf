package htmltopdf

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

func offPageWarnings(ws []Warning) []Warning {
	var out []Warning
	for _, w := range ws {
		if w.Code == WarnTextOffPage {
			out = append(out, w)
		}
	}
	return out
}

// countMatches compares a count with the expected one; the table's depends on
// how many cells the layout puts on the page, so it only has to be large.
func countMatches(name string, got, want int) bool {
	if name == "wide auto table" {
		return got >= 900
	}
	return got == want
}

func TestTextOffPageWarning(t *testing.T) {
	cells := strings.Repeat("<td>x</td>", 1000)
	tests := []struct {
		name   string
		html   string
		detail string
		count  int
	}{
		{"margin right", `<p style="margin-left:5000px">far away text</p>`, "right", 3},
		{"fixed right", `<div style="position:fixed;left:99999px">far</div>`, "right", 1},
		{"absolute left", `<div style="position:absolute;left:-5000px">far</div>`, "left", 1},
		{"partly on the page", `<p style="margin-left:700px">partly on the page, running past the right edge</p>`, "right", 1},
		{"long nowrap line", `<p style="white-space:nowrap">` + strings.Repeat("word ", 400) + `</p>`, "right", 1},
		{"wide auto table", `<table><tr>` + cells + `</tr></table>`, "right", 937},
		{"overflow hidden", `<div style="overflow:hidden"><p style="margin-left:5000px">far away text</p></div>`, "right", 3},
		{"below the page", `<style>@page{size:300px 300px}</style><div style="position:absolute;top:5000px">far</div>`, "below", 1},
		{"above the page", `<style>@page{size:300px 300px}</style><div style="position:absolute;top:-5000px">far</div>`, "above", 1},
		{"huge line-height", `<style>@page{size:300px 300px}</style><div style="position:absolute;top:200px"><p style="line-height:1e7px">a b</p></div>`, "below", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, _ := renderBytes(t, mustNew(t, Options{}), tc.html)
			got := offPageWarnings(res.Warnings)
			if len(got) != 1 || got[0].Detail != tc.detail || !countMatches(tc.name, got[0].Count, tc.count) {
				t.Fatalf("warnings %+v, want %s x%d (all: %+v)", got, tc.detail, tc.count, res.Warnings)
			}
		})
	}
}

func TestTextOffPageNotReported(t *testing.T) {
	tests := map[string]string{
		"transparent":                `<p style="color:transparent;margin-left:5000px">far</p>`,
		"size 0":                     `<p style="font-size:0;margin-left:5000px">far</p>`,
		"on the page":                `<p>near</p>`,
		"capitals fit, ascent above": `<style>@page{margin:0}</style><h1 style="margin:0;font-size:96px;line-height:0.8">TITLE</h1>`,
		"transformed":                `<div style="position:absolute;left:0;transform:translateX(99999px)">far</div>`,
		"rotated":                    `<div style="transform:rotate(1deg);margin-left:5000px">far</div>`,
	}
	for name, html := range tests {
		t.Run(name, func(t *testing.T) {
			res, _ := renderBytes(t, mustNew(t, Options{}), html)
			if got := offPageWarnings(res.Warnings); len(got) != 0 {
				t.Fatalf("warnings %+v", got)
			}
		})
	}
}

func TestTextOffPageCountsRunsPerPage(t *testing.T) {
	html := `<style>@page{size:300px 300px}</style><div style="position:fixed;left:99999px">far</div>` +
		strings.Repeat(`<p style="break-after:page">page</p>`, 4)
	res, _ := renderBytes(t, mustNew(t, Options{}), html)
	want := []Warning{{Code: WarnTextOffPage, Detail: "right", Count: res.Pages}}
	if res.Pages < 4 {
		t.Fatalf("%d pages, want at least 4", res.Pages)
	}
	if got := offPageWarnings(res.Warnings); !slices.Equal(got, want) {
		t.Fatalf("warnings %+v, want %+v", got, want)
	}
}

func TestTextOffPageDetailNeverCarriesText(t *testing.T) {
	res, _ := renderBytes(t, mustNew(t, Options{}), `<p style="margin-left:5000px">secret-marker</p>`)
	for _, w := range res.Warnings {
		if strings.Contains(w.Detail, "secret") {
			t.Fatalf("warning leaks the text: %+v", w)
		}
	}
}

func TestTextOffPageRenderingUnchanged(t *testing.T) {
	html := `<p style="margin-left:5000px">far away text</p><p>near</p>`
	_, data := renderBytes(t, mustNew(t, Options{}), html)
	doc := pagesText(pdftest.Validate(t, data))
	if strings.Contains(doc, "far away") || !strings.Contains(doc, "near") {
		t.Fatalf("text %q", doc)
	}
}

// Expected warnings of the report templates: text-off-page must not appear.
func TestTemplatesHaveNoOffPageWarning(t *testing.T) {
	invalid := func(d string, n int) Warning { return Warning{Code: WarnInvalidValue, Detail: d, Count: n} }
	want := map[string][]Warning{
		"00-host-connector": nil,
		"01-modern": {
			invalid("content: string(scan-id)", 1), invalid("content: string(section)", 1),
			{Code: WarnUnsupportedAtRule, Detail: "@page selector", Count: 1},
			{Code: WarnUnsupportedProperty, Detail: "string-set", Count: 2},
		},
		"02-executive": {invalid(`content: leader(".") target-counter(attr(href), page)`, 1)},
		"03-analyst": {
			invalid(`content: "RAPPORT TECHNIQUE · " string(scan)`, 1),
			{Code: WarnUnsupportedProperty, Detail: "string-set", Count: 1},
		},
	}
	tpl := os.DirFS("../../testdata/templates")
	for name, ws := range want {
		for _, size := range []testcorpus.Size{testcorpus.Small, testcorpus.Large} {
			html, err := testcorpus.Report(tpl, name, size)
			if err != nil {
				t.Fatal(err)
			}
			for oi, o := range []Options{referenceOptions(t), m2Options(t)} {
				t.Run(fmt.Sprintf("%s/%s/options%d", name, size, oi), func(t *testing.T) {
					res, _ := renderBytes(t, mustNew(t, o), string(html))
					if !slices.Equal(res.Warnings, ws) || res.WarningsDropped != 0 {
						t.Fatalf("warnings %+v (dropped %d), want %+v", res.Warnings, res.WarningsDropped, ws)
					}
				})
			}
		}
	}
	html, err := os.ReadFile("../../testdata/templates/04-images.html")
	if err != nil {
		t.Fatal(err)
	}
	res, _ := renderBytes(t, mustNew(t, Options{}), string(html))
	if want := []Warning{{Code: WarnImageRejected, Detail: "scheme", Count: 1}}; !slices.Equal(res.Warnings, want) {
		t.Fatalf("04-images warnings %+v, want %+v", res.Warnings, want)
	}
}

func TestTextOffPageNegativeLetterSpacing(t *testing.T) {
	tests := []struct {
		name   string
		html   string
		drawn  string
		detail string
	}{
		{"visible, box inverted", `<p style="margin-left:5px;letter-spacing:-1em">AAAAAAAAAA</p>`, "AAAAAAAAAA", ""},
		{"visible, last glyph past the box", `<style>@page{margin:0}</style><div style="position:absolute;left:20px;font-size:40px;letter-spacing:-1.05em">AA</div>`, "AA", ""},
		{"really off the page", `<p style="margin-left:5000px;letter-spacing:-1em">AAAAAAAAAA</p>`, "", "right"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, data := renderBytes(t, mustNew(t, Options{}), tc.html)
			if got := strings.TrimSpace(pagesText(pdftest.Validate(t, data))); got != tc.drawn {
				t.Fatalf("drawn %q, want %q", got, tc.drawn)
			}
			got := offPageWarnings(res.Warnings)
			if tc.detail == "" && len(got) != 0 || tc.detail != "" && (len(got) != 1 || got[0].Detail != tc.detail) {
				t.Fatalf("warnings %+v, want %q", got, tc.detail)
			}
		})
	}
}
