package box

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/raster"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

// pngURL is a data: URL of a w×h gray PNG whose first pixel is v, so that
// different v give distinct sources.
func pngURL(tb testing.TB, w, h int, v byte) string {
	tb.Helper()
	img := image.NewGray(image.Rect(0, 0, w, h))
	img.Pix[0] = v
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		tb.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func buildRoot(tb testing.TB, html string) (*Box, *budget.Budget) {
	tb.Helper()
	b := testBudget(nil)
	d := parseDoc(tb, html, b)
	root, err := Build(d, newStyler(tb, d, b), b)
	if err != nil {
		tb.Fatal(err)
	}
	return root, b
}

func imageBoxes(b *Box) []*Box {
	var out []*Box
	var walk func(*Box)
	walk = func(b *Box) {
		if b.Kind == KindImage {
			out = append(out, b)
		}
		for _, c := range b.Children {
			walk(c)
		}
		for _, c := range b.OutOfFlow {
			walk(c)
		}
	}
	walk(b)
	return out
}

func TestParseDimension(t *testing.T) {
	if math.Pow10(maxDimensionDigits) <= css.MaxLengthPx {
		t.Fatalf("maxDimensionDigits %d no longer exceeds css.MaxLengthPx %v", maxDimensionDigits, css.MaxLengthPx)
	}
	huge := "1" + strings.Repeat("0", 100000)
	for _, tc := range []struct {
		in      string
		want    float64
		pct, ok bool
	}{
		{"100", 100, false, true},
		{"0", 0, false, true},
		{"007", 7, false, true},
		{" \t\n\f\r42", 42, false, true},
		{"50%", 50, true, true},
		{"12.5%", 12.5, true, true},
		{"1.5", 1.5, false, true},
		{"100px", 100, false, true},
		{"100 %", 100, false, true},
		{"50.%", 50, false, true},
		{"50.", 50, false, true},
		{"50.5.5", 50.5, false, true},
		{"1e400", 1, false, true},
		{"5\x00", 5, false, true},
		{"000000000000000000001", 1, false, true},
		{"99999999999999999999", math.MaxFloat64, false, true},
		{"99999999999999999999%", math.MaxFloat64, true, true},
		{huge, math.MaxFloat64, false, true},
		{huge + ".5%", math.MaxFloat64, true, true},
		{"1." + strings.Repeat("9", 100000), 2, false, true},
		{"", 0, false, false},
		{"   ", 0, false, false},
		{".5", 0, false, false},
		{"-5", 0, false, false},
		{"+5", 0, false, false},
		{"NaN", 0, false, false},
		{"Infinity", 0, false, false},
		{"-Inf", 0, false, false},
		{"%", 0, false, false},
		{" 5", 0, false, false},
		{"５", 0, false, false},
	} {
		name := tc.in
		if len(name) > 24 {
			name = name[:24] + "…"
		}
		n, pct, ok := parseDimension(tc.in)
		if n != tc.want || pct != tc.pct || ok != tc.ok {
			t.Errorf("parseDimension(%q) = %v, %v, %v; want %v, %v, %v", name, n, pct, ok, tc.want, tc.pct, tc.ok)
		}
	}
}

// Whatever the attributes, box.Img holds finite, non-negative values no larger
// than css.MaxLengthPx: layout relies on it.
func TestImgDimensionAttributesAreBounded(t *testing.T) {
	src := pngURL(t, 4, 2, 0)
	values := []string{
		"-1", "-0", "NaN", "nan", "Infinity", "-Infinity", "1e308", "1e400", "-1e400",
		"0x10", "99999999999999999999", "99999999999999999999%", "1" + strings.Repeat("0", 5000),
		"10000000", "10000001", "10000001%", "0.0000001", "", "auto", "50%", "100", "\x00",
	}
	var html strings.Builder
	for _, v := range values {
		fmt.Fprintf(&html, `<img src="%s" width="%s" height="%s">`, src, v, v)
	}
	root, b := buildRoot(t, html.String())
	imgs := imageBoxes(root)
	if len(imgs) != len(values) {
		t.Fatalf("%d image boxes, want %d", len(imgs), len(values))
	}
	for i, bx := range imgs {
		for _, l := range []struct {
			name string
			v    float64
			set  bool
		}{{"width", bx.Img.Width.Value, bx.Img.Width.Set}, {"height", bx.Img.Height.Value, bx.Img.Height.Set}} {
			if math.IsNaN(l.v) || math.IsInf(l.v, 0) || l.v < 0 || l.v > css.MaxLengthPx {
				t.Errorf("%q: %s = %v", values[i], l.name, l.v)
			}
			if !l.set && l.v != 0 {
				t.Errorf("%q: unset %s has value %v", values[i], l.name, l.v)
			}
		}
	}
	want := map[string]bool{"-1": false, "NaN": false, "Infinity": false, "1e400": true, "50%": true, "auto": false, "": false}
	for i, v := range values {
		if set, ok := want[v]; ok && imgs[i].Img.Width.Set != set {
			t.Errorf("%q: width Set = %v, want %v", v, imgs[i].Img.Width.Set, set)
		}
	}
	if p := imgs[len(values)-3].Img.Width; !p.Percent || p.Value != 50 {
		t.Errorf("50%%: %+v", p)
	}
	counts := warnCounts(b)
	// 99999999999999999999, its % twin, the 5001-digit value, 10000001 and its % twin.
	if counts["clamped:img width"] != 5 || counts["clamped:img height"] != 5 {
		t.Errorf("clamp warnings: %v", counts)
	}
	if counts["image-rejected:"+raster.ReasonScheme.String()] != 0 {
		t.Errorf("unexpected rejection: %v", counts)
	}
}

func warnCounts(b *budget.Budget) map[string]int {
	ws, _ := b.Warnings()
	out := make(map[string]int)
	for _, w := range ws {
		out[string(w.Code)+":"+w.Detail] += w.Count
	}
	return out
}

func TestImgBoxes(t *testing.T) {
	src := pngURL(t, 8, 4, 0)
	tests := []struct {
		name string
		html string
		want string
	}{
		{"inline in a line", `<p>a <img src="` + src + `"> b</p>`, lines(
			"block p",
			"  text \"a \"",
			"  img img 8x4",
			"  text \" b\"")},
		{"display block", `<div>a<img style="display:block" src="` + src + `">b</div>`, lines(
			"block div",
			"  anon-block",
			"    text \"a\"",
			"  img img 8x4",
			"  anon-block",
			"    text \"b\"")},
		{"display none is skipped", `<p>a<img style="display:none" src="` + src + `" alt="x">b</p>`, lines(
			"block p",
			"  text \"a\"",
			"  text \"b\"")},
		{"no pseudo-elements", `<style>img::before,img::after{content:"P"}</style><p><img src="` + src + `"></p>`, lines(
			"block p",
			"  img img 8x4")},
		{"link", `<a href="https://e.test/"><img src="` + src + `"></a>`, lines(
			"inline a href=\"https://e.test/\"",
			"  img img 8x4 href=\"https://e.test/\"")},
		{"id", `<img id=logo src="` + src + `">`, "img img #logo 8x4"},
		{"identical src twice", `<img src="` + src + `"><img src="` + src + `">`, lines(
			"img img 8x4",
			"img img 8x4")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, b := buildHTML(t, tc.html)
			if got != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
			}
			if ws := warnings(b); len(ws) != 0 {
				t.Errorf("warnings: %v", ws)
			}
		})
	}
}

func TestImgSourceIsShared(t *testing.T) {
	src := pngURL(t, 3, 3, 0)
	root, _ := buildRoot(t, `<img src="`+src+`"><p><img src="`+src+`"></p>`)
	imgs := imageBoxes(root)
	if len(imgs) != 2 || imgs[0].Img.Src != imgs[1].Img.Src {
		t.Fatalf("sources not shared: %d boxes", len(imgs))
	}
}

// A rejected image becomes the text of its alt, white space collapsed, or
// nothing; the warning detail is the reason keyword, never the src.
func TestImgRejectedFallsBackToAlt(t *testing.T) {
	const secret = "169.254.169.254"
	tests := []struct {
		name, html, want, reason string
	}{
		{"remote with alt", `<p>a <img src="http://` + secret + `/x" alt=" logo  of
			ACME "> b</p>`, lines(
			"block p",
			"  text \"a \"",
			"  text \"logo of ACME\"",
			"  text \" b\""), "scheme"},
		{"file without alt", `<p>a<img src="file:///etc/passwd">b</p>`, lines(
			"block p",
			"  text \"a\"",
			"  text \"b\""), "scheme"},
		{"empty alt", `<p>a<img src="https://` + secret + `/" alt="">b</p>`, lines(
			"block p",
			"  text \"a\"",
			"  text \"b\""), "scheme"},
		{"blank alt", `<p>a<img src="//` + secret + `" alt=" ` + "\t\n" + `">b</p>`, lines(
			"block p",
			"  text \"a\"",
			"  text \"b\""), "scheme"},
		{"no src", `<p><img alt="none"></p>`, lines(
			"block p",
			"  text \"none\""), "scheme"},
		{"gif", `<p><img src="data:image/gif;base64,R0lGODlhAQABAAAAACw=" alt="g"></p>`, lines(
			"block p",
			"  text \"g\""), "type"},
		{"not base64", `<p><img src="data:image/png,` + secret + `" alt="e"></p>`, lines(
			"block p",
			"  text \"e\""), "encoding"},
		{"bad magic", `<p><img src="data:image/png;base64,AAAAAAAAAAAA" alt="f"></p>`, lines(
			"block p",
			"  text \"f\""), "format"},
		{"pre alt is collapsed too", `<p style="white-space:pre"><img src="x" alt="a
  b"></p>`, lines(
			"block p",
			"  text \"a b\""), "scheme"},
		{"alt styled by img", `<p><img style="text-transform:uppercase" src="x" alt="up"></p>`, lines(
			"block p",
			"  text \"UP\""), "scheme"},
		{"alt in a link", `<a href="https://e.test/"><img src="x" alt="go"></a>`, lines(
			"inline a href=\"https://e.test/\"",
			"  text href=\"https://e.test/\" \"go\""), "scheme"},
		{"display block alt stays inline", `<div><img style="display:block" src="x" alt="t"></div>`, lines(
			"block div",
			"  text \"t\""), "scheme"},
		{"width and height ignored", `<p><img src="x" width=500 height=500 alt="w"></p>`, lines(
			"block p",
			"  text \"w\""), "scheme"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, b := buildHTML(t, tc.html)
			if got != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
			}
			ws, _ := b.Warnings()
			if len(ws) != 1 || ws[0].Code != budget.WarnImageRejected || ws[0].Detail != tc.reason || ws[0].Count != 1 {
				t.Errorf("warnings %+v, want one image-rejected %s", ws, tc.reason)
			}
		})
	}
}

// Images with display none are not added to the raster set: they do not use up
// MaxImages.
func TestImgDisplayNoneIsNotCounted(t *testing.T) {
	var html strings.Builder
	for i := range raster.MaxImages + 8 {
		fmt.Fprintf(&html, `<img style="display:none" src="%s">`, pngURL(t, 1, 1, byte(i)))
	}
	for i := range raster.MaxImages {
		fmt.Fprintf(&html, `<img src="%s">`, pngURL(t, 1, 1, byte(i+100)))
	}
	root, b := buildRoot(t, html.String())
	if n := len(imageBoxes(root)); n != raster.MaxImages {
		t.Errorf("%d image boxes, want %d", n, raster.MaxImages)
	}
	if ws := warnings(b); len(ws) != 0 {
		t.Errorf("warnings: %v", ws)
	}
}

func TestImgCountCapWarnsOncePerImg(t *testing.T) {
	var html strings.Builder
	for i := range raster.MaxImages + 3 {
		fmt.Fprintf(&html, `<img src="%s" alt="n%d">`, pngURL(t, 1, 1, byte(i)), i)
	}
	root, b := buildRoot(t, html.String())
	if n := len(imageBoxes(root)); n != raster.MaxImages {
		t.Errorf("%d image boxes, want %d", n, raster.MaxImages)
	}
	if c := warnCounts(b)["image-rejected:count"]; c != 3 {
		t.Errorf("count warnings = %d, want 3", c)
	}
}

// A budget error of raster (Work) fails the build: it is not a rejection.
func TestImgBudgetErrorPropagates(t *testing.T) {
	const work = 20000
	src := pngURL(t, 2, 2, 0)
	padded := strings.Replace(src, ",", ","+strings.Repeat(" ", 64*work), 1)
	for _, tc := range []struct {
		src  string
		fail bool
	}{{src, false}, {padded, true}} {
		b := testBudget(func(l *budget.Limits) { l[budget.Work] = work })
		d := parseDoc(t, `<p><img src="`+tc.src+`"></p>`, b)
		_, err := Build(d, newStyler(t, d, b), b)
		var le *budget.LimitError
		if tc.fail != (err != nil) || tc.fail && (!errors.As(err, &le) || le.Kind != budget.Work) {
			t.Errorf("src of %d bytes: err = %v", len(tc.src), err)
		}
	}
}

// The alt of a rejected image is hostile text: it is sanitized exactly as a
// text node is (bidi controls shown, controls replaced, zero-width dropped).
func TestImgAltIsSanitizedLikeText(t *testing.T) {
	esc := strings.NewReplacer("&", "&amp;", `"`, "&quot;", "<", "&lt;", ">", "&gt;")
	for _, tc := range testcorpus.TextCases() {
		t.Run(tc.Name, func(t *testing.T) {
			raw := esc.Replace(tc.Raw)
			want, _ := buildHTML(t, `<p>`+raw+`</p>`)
			got, _ := buildHTML(t, `<p><img src="x" alt="`+raw+`"></p>`)
			if got != want {
				t.Errorf("alt:\n%s\ntext node:\n%s", got, want)
			}
		})
	}
}
