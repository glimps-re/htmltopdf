package htmltopdf

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

// grayPNGURL is a data: URL of a w×h gray PNG.
func grayPNGURL(tb testing.TB, w, h int) string {
	tb.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		tb.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// imageCaseFor returns the src of the first crafted image of the corpus
// rejected with reason.
func imageCaseFor(tb testing.TB, reason string) string {
	tb.Helper()
	for _, c := range testcorpus.ImageCases() {
		if c.Want == reason {
			return c.Src
		}
	}
	tb.Fatalf("no corpus image rejected with %q", reason)
	return ""
}

// pageText joins the text blocks with no separator (pagesText puts spaces and
// newlines), so that an alt fallback can be compared exactly with its neighbours.
func pageText(doc *pdftest.Doc) string {
	var sb strings.Builder
	for _, p := range doc.Pages {
		sb.WriteString(strings.Join(p.Text, ""))
	}
	return sb.String()
}

// Each Reason through Render: one image-rejected warning whose detail is the
// reason keyword only; the alt text is drawn exactly for a build-time
// rejection, never for a corrupt image, whose box stays blank.
func TestImageRejectedThroughRender(t *testing.T) {
	c := mustNew(t, Options{})
	for _, reason := range []string{"scheme", "type", "encoding", "format", "size", "corrupt"} {
		src := imageCaseFor(t, reason)
		for _, alt := range []string{"", ` alt="  Logo  of
	ACME "`} {
			t.Run(reason+"/alt="+strconv.FormatBool(alt != ""), func(t *testing.T) {
				res, out := renderBytes(t, c, `<p>before<img src="`+src+`"`+alt+`>after</p>`)
				if len(res.Warnings) != 1 || res.Warnings[0] != (Warning{Code: WarnImageRejected, Detail: reason, Count: 1}) {
					t.Fatalf("warnings %+v, want one image-rejected %s", res.Warnings, reason)
				}
				doc := pdftest.Validate(t, out)
				text := pageText(doc)
				want := "beforeafter"
				if alt != "" && reason != "corrupt" {
					want = "beforeLogo of ACMEafter"
				}
				if text != want {
					t.Errorf("text %q, want %q", text, want)
				}
				if len(doc.Images) != 0 || strings.Contains(string(doc.Pages[0].Content), " Do") {
					t.Errorf("an image is drawn: %d XObjects", len(doc.Images))
				}
			})
		}
	}
}

// The count and total caps reject the images past them, one warning each.
func TestImageCapsThroughRender(t *testing.T) {
	if testing.Short() {
		t.Skip("decodes four 2048² images")
	}
	c := mustNew(t, Options{})
	for _, d := range testcorpus.ImageDocs() {
		if d.Name != "img_distinct_65" && d.Name != "img_total_5x2048sq" {
			continue
		}
		t.Run(d.Name, func(t *testing.T) {
			res, out := renderBytes(t, c, string(d.HTML()))
			got := map[string]int{}
			for _, w := range res.Warnings {
				if w.Code != WarnImageRejected {
					t.Errorf("warning %+v", w)
				}
				got[w.Detail] += w.Count
			}
			if len(got) != len(d.Rejects) {
				t.Errorf("warnings %v, want %v", got, d.Rejects)
			}
			for k, n := range d.Rejects {
				if got[k] != n {
					t.Errorf("%s: %d warnings, want %d", k, got[k], n)
				}
			}
			pdftest.Validate(t, out)
		})
	}
}

var cmDo = regexp.MustCompile(`([-\d.]+) 0 0 ([-\d.]+) ([-\d.]+) ([-\d.]+) cm\n/Im0 Do`)

// <a><img></a>: the image is drawn and the link annotation is its rectangle.
func TestImageLinkAnnotationIsTheImageRect(t *testing.T) {
	html := `<p>x <a href="https://example.com/r"><img src="` + grayPNGURL(t, 40, 20) + `" width="80"></a> y</p>`
	res, out := renderBytes(t, mustNew(t, Options{}), html)
	if len(res.Warnings) != 0 {
		t.Fatalf("warnings %+v", res.Warnings)
	}
	doc := pdftest.Validate(t, out)
	if len(doc.Images) != 1 {
		t.Fatalf("%d images", len(doc.Images))
	}
	m := cmDo.FindStringSubmatch(string(doc.Pages[0].Content))
	if m == nil {
		t.Fatalf("no image draw in:\n%s", doc.Pages[0].Content)
	}
	var v [4]float64
	for i := range v {
		var err error
		if v[i], err = strconv.ParseFloat(m[i+1], 64); err != nil {
			t.Fatal(err)
		}
	}
	if math.Abs(v[0]-60) > 0.01 || math.Abs(v[1]-30) > 0.01 {
		t.Errorf("image drawn %v×%v pt, want 60×30 (80×40 px)", v[0], v[1])
	}
	want := [4]float64{v[2], v[3], v[2] + v[0], v[3] + v[1]}
	var found bool
	for _, l := range doc.Pages[0].Links {
		if l.URI != "https://example.com/r" {
			continue
		}
		found = true
		for i := range want {
			if math.Abs(l.Rect[i]-want[i]) > 0.01 {
				t.Errorf("link rect %v, want the image rect %v", l.Rect, want)
				break
			}
		}
	}
	if !found {
		t.Errorf("no link annotation: %+v", doc.Pages[0].Links)
	}
}

// hostile_remote: the img sources are never fetched; each is rejected with
// scheme. The link element sits in head, which is never styled: it warns nothing.
func TestHostileRemoteImagesRejected(t *testing.T) {
	for _, tc := range testcorpus.SpikeHostile() {
		if tc.Name != "hostile_remote" {
			continue
		}
		res, out := renderBytes(t, mustNew(t, Options{}), string(tc.HTML()))
		want := []Warning{
			{Code: WarnImageRejected, Detail: "scheme", Count: 2},
		}
		if len(res.Warnings) != len(want) {
			t.Fatalf("warnings %+v, want %+v", res.Warnings, want)
		}
		for i := range want {
			if res.Warnings[i] != want[i] {
				t.Errorf("warning %d: %+v, want %+v", i, res.Warnings[i], want[i])
			}
		}
		if text := pageText(pdftest.Validate(t, out)); text != "ssrf" {
			t.Errorf("text %q", text)
		}
		return
	}
	t.Fatal("hostile_remote not in the corpus")
}

func linksOf(tb testing.TB, out []byte) []pdftest.Link {
	tb.Helper()
	var ls []pdftest.Link
	for _, p := range pdftest.Validate(tb, out).Pages {
		ls = append(ls, p.Links...)
	}
	return ls
}

// Atomic inlines and block-level replaced boxes in a link get one annotation of
// their border box, as in Chrome.
func TestLinkOnAtomsAndReplacedBlocks(t *testing.T) {
	img := grayPNGURL(t, 40, 20)
	c := mustNew(t, Options{})
	for _, tc := range []struct {
		name, inner string
		w, h        float64 // border box, in pt
	}{
		{"inline img", `<img src="` + img + `">`, 30, 15},
		{"block img with border", `<img style="display:block;border:2px solid red" src="` + img + `" width="80">`, 63, 33},
		{"floated img", `<img style="float:left" src="` + img + `">`, 30, 15},
		{"svg", `<svg width="30" height="30"><rect width="30" height="30"/></svg>`, 22.5, 22.5},
		{"block svg", `<svg style="display:block" width="30" height="30"><rect width="30" height="30"/></svg>`, 22.5, 22.5},
		{"inline-block without text", `<span style="display:inline-block;width:50px;height:20px;background:red"></span>`, 37.5, 15},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, out := renderBytes(t, c, `<div><a href="https://example.com/a">`+tc.inner+`</a></div>`)
			ls := linksOf(t, out)
			if len(ls) != 1 || ls[0].URI != "https://example.com/a" {
				t.Fatalf("links %+v", ls)
			}
			r := ls[0].Rect
			if w, h := r[2]-r[0], r[3]-r[1]; math.Abs(w-tc.w) > 0.01 || math.Abs(h-tc.h) > 0.01 {
				t.Errorf("link %v×%v pt, want %v×%v", w, h, tc.w, tc.h)
			}
		})
	}
}

// Text and an image of the same link on one line make one annotation that
// covers both.
func TestLinkRunMergesTextAndImage(t *testing.T) {
	img := grayPNGURL(t, 40, 40)
	_, out := renderBytes(t, mustNew(t, Options{}), `<p><a href="https://example.com/m">go <img src="`+img+`"></a> after</p>`)
	ls := linksOf(t, out)
	if len(ls) != 1 {
		t.Fatalf("links %+v", ls)
	}
	if r := ls[0].Rect; r[2]-r[0] <= 30 || r[3]-r[1] < 30-0.01 {
		t.Errorf("link rect %v does not cover the text and the 30 pt image", r)
	}
}

// A hostile href on an image or an inline-block is handled exactly as on text:
// same annotations, same warnings.
func TestAtomLinkHrefSanitizedAsText(t *testing.T) {
	img := grayPNGURL(t, 10, 10)
	c := mustNew(t, Options{})
	for _, href := range []string{
		"javascript:alert(1)", "file:///etc/passwd", "JAVASCRIPT:x", " https://example.com/ok ",
		"https://example.com/" + strings.Repeat("a", 3000), "https://example.com/\x01", "#missing", "#target",
		"https://例え.テスト/パス?q=<>\"", "relative/path",
	} {
		render := func(inner string) ([]pdftest.Link, []Warning) {
			html := `<p id="target"><a href="` + htmlAttrEscape(href) + `">` + inner + `</a></p>`
			res, out := renderBytes(t, c, html)
			ls := linksOf(t, out)
			for i := range ls {
				ls[i].Rect = [4]float64{}
			}
			return ls, res.Warnings
		}
		wantLinks, wantWarn := render("text")
		for _, inner := range []string{`<img src="` + img + `">`, `<span style="display:inline-block;width:10px;height:10px"></span>`} {
			gotLinks, gotWarn := render(inner)
			if !slices.Equal(gotLinks, wantLinks) || !slices.Equal(gotWarn, wantWarn) {
				t.Errorf("href %.40q, %s: links %+v warnings %+v; text gives %+v %+v", href, inner[:5], gotLinks, gotWarn, wantLinks, wantWarn)
			}
		}
	}
}

func htmlAttrEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", `"`, "&quot;", "<", "&lt;").Replace(s)
}

// An atom moved left of the text before it, by a relative offset, stays inside
// the link of its run, and the text keeps its link.
func TestLinkRunCoversAnAtomMovedLeft(t *testing.T) {
	img := grayPNGURL(t, 40, 40)
	html := `<p style="margin-left:400px"><a href="https://example.com/l">text <img style="position:relative;left:-300px" src="` + img + `"></a></p>`
	_, out := renderBytes(t, mustNew(t, Options{}), html)
	doc := pdftest.Validate(t, out)
	ls := doc.Pages[0].Links
	if len(ls) != 1 {
		t.Fatalf("links %+v", ls)
	}
	m := cmDo.FindStringSubmatch(string(doc.Pages[0].Content))
	if m == nil {
		t.Fatal("image not drawn")
	}
	x, err := strconv.ParseFloat(m[3], 64)
	if err != nil {
		t.Fatal(err)
	}
	r := ls[0].Rect
	if r[0] > x+0.01 {
		t.Errorf("link starts at %v, right of the image at %v", r[0], x)
	}
	if r[2] < 400*0.75 {
		t.Errorf("link ends at %v, before the text at 300 pt", r[2])
	}
}
