package htmltopdf

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

const m6Alt = "ALT TEXT"

// m6CaseDoc puts the crafted src of a corpus row between two words.
func m6CaseDoc(src string) string {
	return `<p>before<img src="` + src + `" alt="` + m6Alt + `">after</p>`
}

// Every crafted image of the corpus through Render with the default options:
// the expected reason (one warning, the reason only), or a drawn image. The
// alt text is drawn for every rejection except a corrupt image, which keeps its
// blank box. The same document through RenderTemplate gives the same bytes.
func TestM6ImageCasesEndToEnd(t *testing.T) {
	for _, c := range testcorpus.ImageCases() {
		t.Run(c.Name, func(t *testing.T) {
			html := m6CaseDoc(c.Src)
			r := m6Render(t, Options{}, html)
			if m6Hostile(t, r) != nil {
				t.Fatalf("limit error %v", r.err)
			}
			wantText := "beforeafter"
			if c.Want == "" {
				if len(r.res.Warnings) != 0 {
					t.Errorf("warnings %+v on an image that must be drawn", r.res.Warnings)
				}
				if len(r.doc.Images) != 1 || m6Do(r.doc) != 1 {
					t.Errorf("%d XObjects, %d draws, want 1 and 1", len(r.doc.Images), m6Do(r.doc))
				}
			} else {
				got := m6Rejects(t, r.res)
				if len(got) != 1 || got[c.Want] != 1 || len(r.res.Warnings) != 1 || r.res.Warnings[0].Count != 1 {
					t.Errorf("warnings %+v, want one %s", r.res.Warnings, c.Want)
				}
				if len(r.doc.Images) != 0 || m6Do(r.doc) != 0 {
					t.Errorf("%d XObjects, %d draws on a rejected image", len(r.doc.Images), m6Do(r.doc))
				}
				if c.Want != "corrupt" {
					wantText = "before" + m6Alt + "after"
				}
			}
			if got := pageText(r.doc); got != wantText {
				t.Errorf("text %q, want %q", got, wantText)
			}

			tmpl := template.Must(template.New("t").Parse(html))
			var buf bytes.Buffer
			res, err := mustNew(t, Options{}).RenderTemplate(context.Background(), tmpl, nil, &buf)
			if err != nil {
				t.Fatalf("RenderTemplate: %v", err)
			}
			if !bytes.Equal(buf.Bytes(), r.data) || res.Pages != r.res.Pages || len(res.Warnings) != len(r.res.Warnings) {
				t.Errorf("RenderTemplate gives %d bytes and %+v, Render %d bytes and %+v", buf.Len(), res.Warnings, len(r.data), r.res.Warnings)
			}
		})
	}
}

// Every hostile image document through Render: its expected outcome, the
// rejections it counts, and the number of XObjects the caps leave.
func TestM6ImageDocsEndToEnd(t *testing.T) {
	xobjects := map[string]int{
		"img_dup_10000": 1, "img_distinct_64": 64, "img_distinct_65": 64, "img_total_4x2048sq": 4,
		"img_total_5x2048sq": 4, "img_nested_inlines_500": 1, "img_display_none_not_counted": 1,
	}
	for _, d := range testcorpus.ImageDocs() {
		t.Run(d.Name, func(t *testing.T) {
			r := m6Render(t, Options{}, string(d.HTML()))
			le := m6Hostile(t, r)
			switch d.Want {
			case testcorpus.Limit:
				if le == nil || le.Limit != LimitWork {
					t.Fatalf("want LimitError{MaxWork}, got %v", r.err)
				}
				return
			case testcorpus.Robust:
				return
			}
			if le != nil {
				t.Fatalf("limit error %v", le)
			}
			got := m6Rejects(t, r.res)
			if len(got) != len(d.Rejects) {
				t.Errorf("rejections %v, want %v", got, d.Rejects)
			}
			for k, n := range d.Rejects {
				if got[k] != n {
					t.Errorf("%s: %d rejections, want %d", k, got[k], n)
				}
			}
			if (d.Want == testcorpus.Degraded) != (len(r.res.Warnings) > 0) {
				t.Errorf("outcome %v with warnings %+v", d.Want, r.res.Warnings)
			}
			if want, ok := xobjects[d.Name]; ok && len(r.doc.Images) != want {
				t.Errorf("%d XObjects, want %d", len(r.doc.Images), want)
			}
		})
	}
}

// The Work charged to the progressive JPEG of the corpus, measured on the final
// formula, is what the document's expectation relies on: 2048² × 5 ×
// ⌈32/4⌉ is far over the default budget.
func TestM6ProgressiveScansWorkIsOverBudget(t *testing.T) {
	for _, d := range testcorpus.ImageDocs() {
		if d.Name != "img_progressive_32_scans_work" {
			continue
		}
		c := mustNew(t, Options{})
		var buf bytes.Buffer
		_, b, err := c.render(context.Background(), bytes.NewReader(d.HTML()), &buf)
		var le *LimitError
		if !errors.As(err, &le) || le.Limit != LimitWork {
			t.Fatalf("err %v, want LimitError{MaxWork}", err)
		}
		t.Logf("measured Work at the failure: %d of %d", b.Used(budget.Work), le.Max)
		return
	}
	t.Fatal("document not in the corpus")
}

// An img rejected at build time shows its alt text as an inline run even when
// the image is block-level, floated or absolute: the text is drawn
// once, on a page, whatever the layout.
func TestM6AltOfBlockFloatAbsoluteStaysInlineText(t *testing.T) {
	bad := `src="http://attacker.example/x.png" alt="` + m6Alt + `"`
	for name, style := range map[string]string{
		"block":     "display:block",
		"float":     "float:left",
		"absolute":  "position:absolute;top:20px;left:20px",
		"fixed":     "position:fixed;bottom:10px;right:10px",
		"flex item": "flex:1",
		"inline":    "",
	} {
		t.Run(name, func(t *testing.T) {
			html := `<div style="` + map[bool]string{true: "display:flex", false: ""}[name == "flex item"] + `">before<img style="` + style + `" ` + bad + `>after</div>`
			r := m6Render(t, Options{}, html)
			if m6Hostile(t, r) != nil {
				t.Fatalf("limit error %v", r.err)
			}
			if got := m6AllRejects(r.res); got["scheme"] != 1 || len(got) != 1 {
				t.Errorf("rejections %v", got)
			}
			text := pageText(r.doc)
			if strings.Count(text, m6Alt) != 1 || !strings.Contains(text, "before") || !strings.Contains(text, "after") {
				t.Errorf("text %q", text)
			}
		})
	}
}

// The alt text of a rejected image is hostile text: it is drawn exactly as the
// same text in a span, with the same markers, drops and replacements.
func TestM6AltSpoofingIsSanitizedLikeText(t *testing.T) {
	esc := strings.NewReplacer("&", "&amp;", `"`, "&quot;", "<", "&lt;", ">", "&gt;")
	type spoof struct{ name, raw string }
	var cases []spoof
	for _, tc := range testcorpus.TextCases() {
		cases = append(cases, spoof{tc.Name, tc.Raw})
	}
	rlo, lro, zwsp := "\u202e", "\u202d", "\u200b"
	cases = append(cases,
		spoof{"rlo_extension", "report" + rlo + "fdp.exe"},
		spoof{"rlo_then_text", "Invoice " + rlo + "cod.exe"},
		spoof{"nested_overrides", strings.Repeat(rlo+lro, 500) + "x"},
		spoof{"unbalanced_isolates", strings.Repeat("\u2066\u2067\u2068", 400) + "end"},
		spoof{"zero_width_between_letters", "p" + zwsp + "a" + "\u2060" + "y" + "\ufeff" + "pal"},
		spoof{"only_zero_width", strings.Repeat(zwsp, 1000)},
		spoof{"c0_c1_mix", "a\x01b\x02c\x07d\x1be\x7ff\u0085g"},
		spoof{"invalid_utf8", "a\xff\xfe\xc3\x28\xf0\x9f\x98\xed\xa0\x80z"},
		spoof{"entities", "&#x202e;&#8203;&#0;&#x1b;&#xd800;&#x10ffff;&#xffffffff;"},
		spoof{"long_alt_200k", strings.Repeat("alt ", 50_000)},
		spoof{"long_unbroken_alt_100k", strings.Repeat("W", 100_000)},
	)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := esc.Replace(tc.raw)
			if tc.name == "entities" {
				raw = tc.raw
			}
			alt := m6Render(t, Options{}, `<p>X<img src="http://attacker.example/a.png" alt="`+raw+`">Y</p>`)
			span := m6Render(t, Options{}, `<p>X<span>`+raw+`</span>Y</p>`)
			// An unbreakable word overflows the page area to the right, as in any
			// browser: the glyph check is skipped for it, nothing else.
			known := ""
			if tc.name == "long_unbroken_alt_100k" {
				known = "unbreakable word overflows right"
			}
			if m6HostileGlyphs(t, alt, known) != nil || m6HostileGlyphs(t, span, known) != nil {
				t.Fatalf("limit error: alt %v, span %v", alt.err, span.err)
			}
			if got, want := squeeze(pageText(alt.doc)), squeeze(pageText(span.doc)); got != want {
				t.Errorf("alt draws %.80q, the same text in a span %.80q", got, want)
			}
			if got := m6AllRejects(alt.res); got["scheme"] != 1 {
				t.Errorf("rejections %v", got)
			}
		})
	}
}

// squeeze drops the white space: the alt text is trimmed and collapsed, which a
// span's is not at its ends.
func squeeze(s string) string { return strings.Join(strings.Fields(s), "") }

// The alt text has no way to smuggle a raw bidi override into the PDF text: the
// override characters are shown as markers.
func TestM6AltBidiControlsAreMarked(t *testing.T) {
	r := m6Render(t, Options{}, `<p><img src="x.png" alt="invoice&#x202e;txt.exe"></p>`)
	if m6Hostile(t, r) != nil {
		t.Fatal(r.err)
	}
	text := pageText(r.doc)
	if strings.ContainsAny(text, "\u202a\u202b\u202c\u202d\u202e\u2066\u2067\u2068\u2069") || !strings.Contains(text, "invoice") || !strings.Contains(text, "txt.exe") {
		t.Errorf("text %q holds a raw bidi control or lost the alt", text)
	}
}

// connCounter listens on addr and counts the connections it accepts, whatever
// the protocol spoken on them.
type connCounter struct {
	ln net.Listener
	n  atomic.Int64
	wg sync.WaitGroup
}

func newConnCounter(tb testing.TB, addr string) *connCounter {
	tb.Helper()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil
	}
	c := &connCounter{ln: ln}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			c.n.Add(1)
			if err := conn.Close(); err != nil {
				tb.Logf("close: %v", err)
			}
		}
	}()
	return c
}

// stop closes the listener, waits for its goroutine and returns the number of
// connections accepted.
func (c *connCounter) stop(tb testing.TB) int64 {
	tb.Helper()
	if err := c.ln.Close(); err != nil {
		tb.Logf("close listener: %v", err)
	}
	c.wg.Wait()
	return c.n.Load()
}

// Nothing but data: URLs is ever read: raw listeners on 127.0.0.1 and [::1]
// count every connection, whatever the protocol (http, https, ftp, gopher),
// and a PNG file on disk is named every way a loader might open it.
func TestM6NonDataSchemesAreNeverFetched(t *testing.T) {
	v4 := newConnCounter(t, "127.0.0.1:0")
	if v4 == nil {
		t.Skip("no loopback")
	}
	counters := []*connCounter{v4}
	if v6 := newConnCounter(t, "[::1]:0"); v6 != nil {
		counters = append(counters, v6)
	}
	h4 := v4.ln.Addr().String()
	h6 := h4
	if len(counters) == 2 {
		h6 = counters[1].ln.Addr().String()
	}
	port := h4[strings.LastIndex(h4, ":")+1:]
	base := "http://" + h4

	dir := t.TempDir()
	file := filepath.Join(dir, "local.png")
	if err := os.WriteFile(file, pngBytes(t, 4, 4), 0o600); err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(mustGetwd(t), file)
	if err != nil {
		t.Fatal(err)
	}

	srcs := []string{
		base + "/a.png", "//" + h4 + "/b.png", strings.ToUpper(base) + "/c.png", " " + base + "/d.png",
		"\t" + base + "/e.png", "\n" + base + "/f.png", "http:" + h4 + "/g.png", "https://" + h4 + "/h.png",
		"https://" + h6 + "/h6.png", "http://" + h6 + "/m.png", "//" + h6 + "/m2.png",
		"file://" + file, "file:" + file, "FILE://" + file, file, rel, "./" + filepath.Base(file), "file://localhost" + file,
		"ftp://" + h4 + "/i.png", "gopher://" + h4 + "/j", "javascript:alert(1)", "blob:" + base + "/k",
		"about:blank", "cid:logo", "\\\\" + h4 + "\\x.png", "data:" + base, "d\x00ata:image/png;base64,AAAA",
		"http://" + h4 + "@" + h4 + "/l.png", "//127.0.0.1:" + port + "/n.png", "http://localhost:" + port + "/o.png",
	}
	var sb strings.Builder
	for _, s := range srcs {
		sb.WriteString(`<img src="` + s + `" alt="a">`)
	}
	// Every other way of naming an image, or a resource: none is supported, none
	// may be fetched.
	sb.WriteString(`<picture><source srcset="` + base + `/p1.png 1x"><img srcset="` + base + `/p2.png 2x" src="` + base + `/p3.png"></picture>`)
	sb.WriteString(`<input type="image" src="` + base + `/q.png"><svg width="10" height="10"><image href="` + base + `/r.png" width="10" height="10"/><image xlink:href="file://` + file + `"/></svg>`)
	sb.WriteString(`<div style="background:url(` + base + `/s.png);list-style-image:url(` + base + `/t.png);border-image:url(` + base + `/u.png);content:url(` + base + `/v.png)">x</div>`)
	sb.WriteString(`<video poster="` + base + `/w.png"></video><object data="` + base + `/x.png"></object><embed src="` + base + `/y.png"><iframe src="` + base + `/z.html"></iframe>`)
	html := `<html><head><link rel="stylesheet" href="` + base + `/s.css"><style>@import url(` + base + `/i.css);@font-face{font-family:x;src:url(` + base + `/f.ttf)}</style></head><body>` + sb.String() + `</body></html>`

	r := m6Render(t, Options{}, html)
	var hits int64
	for _, c := range counters {
		hits += c.stop(t)
	}
	if hits != 0 {
		t.Errorf("%d connections accepted: something was fetched", hits)
	}
	if m6Hostile(t, r) != nil {
		t.Fatalf("limit error %v", r.err)
	}
	if len(r.doc.Images) != 0 || m6Do(r.doc) != 0 {
		t.Errorf("%d XObjects drawn, %d draws", len(r.doc.Images), m6Do(r.doc))
	}
	for _, w := range r.res.Warnings {
		if w.Code == WarnImageRejected && w.Detail != "scheme" && w.Detail != "type" && w.Detail != "format" && w.Detail != "encoding" {
			t.Errorf("unexpected rejection %+v", w)
		}
	}
	var total int
	for _, w := range r.res.Warnings {
		if w.Code == WarnImageRejected {
			total += w.Count
		}
	}
	// The list, and the <img> of the <picture>.
	if total != len(srcs)+1 {
		t.Errorf("%d image rejections for %d <img>", total, len(srcs)+1)
	}
}

func base64Of(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func mustGetwd(tb testing.TB) string {
	tb.Helper()
	d, err := os.Getwd()
	if err != nil {
		tb.Fatal(err)
	}
	return d
}

func pngBytes(tb testing.TB, w, h int) []byte {
	tb.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, w, h))); err != nil {
		tb.Fatal(err)
	}
	return buf.Bytes()
}

const m6Many = 10_000

func manyImgs(src func(i int) string, attrs string, sep ...string) string {
	var sb strings.Builder
	for i := range m6Many {
		sb.WriteString(`<img src="` + src(i) + `"` + attrs + `>`)
		sb.WriteString(strings.Join(sep, ""))
	}
	return sb.String()
}

// 10 000 <img> against the limits: duplicates are free; distinct sources stop at
// 64 with one deduplicated warning; the box, page and work limits end the render
// with their LimitError, never a panic or another error.
func TestM6TenThousandImgTags(t *testing.T) {
	same := grayPNGURL(t, 1, 1)
	distinct := func(i int) string {
		var buf bytes.Buffer
		img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
		img.Set(0, 0, color.NRGBA{byte(i & 0xff), byte(i >> 8 & 0xff), 7, 255})
		if err := png.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		return "data:image/png;base64," + base64Of(buf.Bytes())
	}

	t.Run("duplicates", func(t *testing.T) {
		r := m6Render(t, Options{}, manyImgs(func(int) string { return same }, ""))
		if m6Hostile(t, r) != nil {
			t.Fatal(r.err)
		}
		if len(r.res.Warnings) != 0 || len(r.doc.Images) != 1 || m6Do(r.doc) != 10_000 {
			t.Errorf("warnings %+v, %d XObjects, %d draws", r.res.Warnings, len(r.doc.Images), m6Do(r.doc))
		}
	})
	t.Run("distinct", func(t *testing.T) {
		r := m6Render(t, Options{}, manyImgs(distinct, ""))
		if m6Hostile(t, r) != nil {
			t.Fatal(r.err)
		}
		if got := m6Rejects(t, r.res); got["count"] != 10_000-64 || len(r.res.Warnings) != 1 {
			t.Errorf("warnings %+v, want one count warning of %d", r.res.Warnings, 10_000-64)
		}
		if len(r.doc.Images) != 64 || m6Do(r.doc) != 64 {
			t.Errorf("%d XObjects, %d draws, want 64", len(r.doc.Images), m6Do(r.doc))
		}
	})
	t.Run("MaxBoxes", func(t *testing.T) {
		r := m6Render(t, Options{Limits: Limits{MaxBoxes: 5_000}}, manyImgs(func(int) string { return same }, ""))
		if le := m6Hostile(t, r); le == nil || le.Limit != LimitBoxes {
			t.Errorf("err %v, want LimitError{MaxBoxes}", r.err)
		}
	})
	t.Run("MaxWork", func(t *testing.T) {
		r := m6Render(t, Options{Limits: Limits{MaxWork: 20_000}}, manyImgs(func(int) string { return same }, ""))
		if le := m6Hostile(t, r); le == nil || le.Limit != LimitWork {
			t.Errorf("err %v, want LimitError{MaxWork}", r.err)
		}
	})
	t.Run("tall", func(t *testing.T) {
		r := m6Render(t, Options{}, manyImgs(func(int) string { return same }, ` width="900" height="900" style="display:block"`))
		if le := m6Hostile(t, r); le == nil || le.Limit != LimitPages {
			t.Errorf("err %v, want LimitError{MaxPages}", r.err)
		}
	})
	t.Run("MaxOutputBytes", func(t *testing.T) {
		r := m6Render(t, Options{Limits: Limits{MaxOutputBytes: 64 << 10}}, manyImgs(func(int) string { return same }, ` width="900" height="900"`))
		if le := m6Hostile(t, r); le == nil || (le.Limit != LimitOutputBytes && le.Limit != LimitPages) {
			t.Errorf("err %v, want LimitError{MaxOutputBytes}", r.err)
		}
	})
	t.Run("rejected with alt", func(t *testing.T) {
		r := m6Render(t, Options{}, manyImgs(func(int) string { return "http://attacker.example/x.png" }, ` alt="ALT"`, " "))
		if m6Hostile(t, r) != nil {
			t.Fatal(r.err)
		}
		if got := m6Rejects(t, r.res); got["scheme"] != 10_000 || len(r.res.Warnings) != 1 {
			t.Errorf("warnings %+v", r.res.Warnings)
		}
		if n := strings.Count(pageText(r.doc), "ALT"); n != 10_000 {
			t.Errorf("alt drawn %d times, want 10000", n)
		}
	})
}

// A deadline shorter than one decode returns the context error (or a limit) and
// not later than one decode after it: the only step that cannot be interrupted
// is one decode, so the 2 s bound is not held under the race detector. The 1 ms
// deadline may expire before or during the decode; the elapsed time is logged.
func TestM6TimeoutWithAnImage(t *testing.T) {
	var src string
	for _, c := range testcorpus.ImageCases() {
		if c.Name == "png_rgba16_2048x2048_limit" {
			src = c.Src
		}
	}
	if src == "" {
		t.Fatal("corpus row missing")
	}
	c := mustNew(t, Options{Limits: Limits{Timeout: time.Millisecond}})
	start := time.Now()
	_, err := c.Render(context.Background(), strings.NewReader(m6CaseDoc(src)), &bytes.Buffer{})
	var le *LimitError
	if !errors.Is(err, context.DeadlineExceeded) && !errors.As(err, &le) {
		t.Errorf("err %v, want a deadline or a LimitError", err)
	}
	d := time.Since(start)
	t.Logf("returned after %v: %v", d.Round(time.Millisecond), err)
	if d > 2*time.Second && !raceDetectorOn {
		t.Errorf("returned after %v", d)
	}
}

// The images of the PDF are the pixels of the source: a PNG with alpha gives
// its colour samples and its soft mask exactly, a JPEG a re-encoding within PSNR 35 dB, and a
// CMYK JPEG samples within PSNR 34 dB of its decoded reference.
func TestM6ExtractedImagesMatchTheOriginals(t *testing.T) {
	c := mustNew(t, Options{})
	t.Run("png rgba", func(t *testing.T) {
		src := image.NewNRGBA(image.Rect(0, 0, 37, 23))
		for y := range 23 {
			for x := range 37 {
				src.SetNRGBA(x, y, color.NRGBA{byte(x * 7 & 0xff), byte(y * 11 & 0xff), byte((x ^ y) & 0xff), byte((255 - x*y%256) & 0xff)})
			}
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, src); err != nil {
			t.Fatal(err)
		}
		_, out := renderBytes(t, c, `<img src="data:image/png;base64,`+base64Of(buf.Bytes())+`">`)
		doc := pdftest.Validate(t, out)
		if len(doc.Images) != 1 {
			t.Fatalf("%d images", len(doc.Images))
		}
		for _, img := range doc.Images {
			if img.Width != 37 || img.Height != 23 || img.Gray || img.DCT || len(img.Mask) != 37*23 {
				t.Fatalf("image %dx%d gray %v dct %v mask %d", img.Width, img.Height, img.Gray, img.DCT, len(img.Mask))
			}
			for y := range 23 {
				for x := range 37 {
					p := src.NRGBAAt(x, y)
					i := y*37 + x
					if got := [4]byte{img.Data[3*i], img.Data[3*i+1], img.Data[3*i+2], img.Mask[i]}; got != [4]byte{p.R, p.G, p.B, p.A} {
						t.Fatalf("pixel %d,%d: %v, want %v", x, y, got, p)
					}
				}
			}
		}
	})
	t.Run("png gray without alpha has no mask", func(t *testing.T) {
		_, out := renderBytes(t, c, `<img src="`+grayPNGURL(t, 9, 5)+`">`)
		doc := pdftest.Validate(t, out)
		if len(doc.Images) != 1 {
			t.Fatalf("%d images", len(doc.Images))
		}
		for _, img := range doc.Images {
			if !img.Gray || img.Mask != nil || len(img.Data) != 45 {
				t.Errorf("gray %v mask %d data %d", img.Gray, len(img.Mask), len(img.Data))
			}
		}
	})
	// A JPEG is re-encoded and must stay within 35 dB PSNR of the source decoded
	// by image/jpeg.
	for _, tc := range []struct {
		name, file string
		floor      float64
	}{
		{"jpeg", "video-001.jpeg", 35},
		{"gray jpeg", "video-005.gray.jpeg", 35},
	} {
		t.Run(tc.name, func(t *testing.T) { m6JPEGMatches(t, c, tc.file, tc.floor) })
	}
	t.Run("cmyk jpeg", func(t *testing.T) {
		raw := readImage(t, "video-001.cmyk.jpeg")
		src, err := jpeg.Decode(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		twin, err := png.Decode(bytes.NewReader(readImage(t, "video-001.cmyk.png")))
		if err != nil {
			t.Fatal(err)
		}
		_, out := renderBytes(t, c, `<img src="data:image/jpeg;base64,`+base64Of(raw)+`">`)
		doc := pdftest.Validate(t, out)
		if len(doc.Images) != 1 {
			t.Fatalf("%d images", len(doc.Images))
		}
		for _, img := range doc.Images {
			rgb := img.Data
			if img.DCT {
				dec, err := jpeg.Decode(bytes.NewReader(img.Data))
				if err != nil {
					t.Fatal(err)
				}
				rgb = rgbSamples(dec)
			}
			pSrc, pTwin := psnr(rgb, rgbSamples(src)), psnr(rgb, rgbSamples(twin))
			t.Logf("CMYK PSNR %.1f dB against the decoded source, %.1f dB against video-001.cmyk.png", pSrc, pTwin)
			if pSrc < 34 || img.Width != src.Bounds().Dx() || img.Height != src.Bounds().Dy() {
				t.Errorf("CMYK image PSNR %.1f dB (%dx%d)", pSrc, img.Width, img.Height)
			}
		}
	})
}

// m6JPEGMatches renders a stdlib JPEG and holds the DCT stream of the PDF to
// floor dB of PSNR against the source decoded by image/jpeg.
func m6JPEGMatches(t *testing.T, c *Converter, file string, floor float64) {
	raw := readImage(t, file)
	ref, err := jpeg.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	_, out := renderBytes(t, c, `<img src="data:image/jpeg;base64,`+base64Of(raw)+`">`)
	doc := pdftest.Validate(t, out)
	if len(doc.Images) != 1 {
		t.Fatalf("%d images", len(doc.Images))
	}
	for _, img := range doc.Images {
		if !img.DCT || img.Mask != nil {
			t.Fatalf("dct %v mask %d", img.DCT, len(img.Mask))
		}
		dec, err := jpeg.Decode(bytes.NewReader(img.Data))
		if err != nil {
			t.Fatal(err)
		}
		p := psnr(rgbSamples(dec), rgbSamples(ref))
		t.Logf("%s: PSNR %.1f dB, %d bytes from %d", file, p, len(img.Data), len(raw))
		if p < floor {
			t.Errorf("PSNR %.1f dB against the decoded source, want at least %.0f", p, floor)
		}
	}
}

func readImage(tb testing.TB, name string) []byte {
	tb.Helper()
	b, err := os.ReadFile(filepath.Clean(filepath.Join("../../testdata/images", name)))
	if err != nil {
		tb.Fatal(err)
	}
	return b
}

func rgbSamples(img image.Image) []byte {
	b := img.Bounds()
	out := make([]byte, 0, b.Dx()*b.Dy()*3)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			out = append(out, byte(r>>8&0xff), byte(g>>8&0xff), byte(bl>>8&0xff))
		}
	}
	return out
}

func psnr(a, b []byte) float64 {
	if len(a) != len(b) {
		return 0
	}
	var se float64
	for i := range a {
		d := float64(a[i]) - float64(b[i])
		se += d * d
	}
	if se == 0 {
		return math.Inf(1)
	}
	return 10 * math.Log10(255*255/(se/float64(len(a))))
}

// The same document renders to the same bytes twice, images included.
func TestM6RenderIsDeterministic(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`<p>x `)
	for i := range 8 {
		sb.WriteString(`<img src="` + grayPNGURL(t, 10+i, 20) + `" width="40"> `)
	}
	for _, f := range []string{"video-001.cmyk.jpeg", "video-001.jpeg", "video-005.gray.jpeg"} {
		sb.WriteString(`<img src="data:image/jpeg;base64,` + base64Of(readImage(t, f)) + `" width="100">`)
	}
	sb.WriteString(`<img src="http://a.example/x.png" alt="alt"></p>`)
	_, a := renderBytes(t, mustNew(t, Options{}), sb.String())
	_, b := renderBytes(t, mustNew(t, Options{}), sb.String())
	if !bytes.Equal(a, b) {
		t.Error("two renders differ")
	}
}

// The realistic report with images: a logo in a fixed-height flex header,
// screenshots in paragraphs, a chart with alpha, two photographs (one CMYK), a
// table of thumbnails over a page break, an image link and one remote image that
// is rejected for its alt text. It is plain HTML, so the corpus of templates
// 00-03 does not list it.
func TestM6Template04Images(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/templates/04-images.html")
	if err != nil {
		t.Fatal(err)
	}
	const wantSum = "06a0d4817902d99df238ccd1f0ea74d58e13bea793d530161ee9724ce8707b72"
	if got := fmt.Sprintf("%x", sha256.Sum256(raw)); got != wantSum {
		t.Errorf("04-images.html SHA-256 %s: regenerate it with testdata/templates/gen_04.go and update wantSum", got)
	}
	r := m6Render(t, Options{}, string(raw))
	if m6Hostile(t, r) != nil {
		t.Fatal(r.err)
	}
	if len(r.res.Warnings) != 1 || r.res.Warnings[0] != (Warning{Code: WarnImageRejected, Detail: "scheme", Count: 1}) {
		t.Errorf("warnings %+v, want the one rejected remote image", r.res.Warnings)
	}
	if r.res.Pages < 2 {
		t.Errorf("%d pages, want the table on a page of its own", r.res.Pages)
	}
	// logo, 2 screenshots, chart, 2 photographs, 4 thumbnails.
	if len(r.doc.Images) != 10 {
		t.Errorf("%d XObjects, want 10", len(r.doc.Images))
	}
	// the same 10 plus 29 more thumbnails and the linked capture drawn twice.
	if n := m6Do(r.doc); n != 1+2+1+2+1+30 {
		t.Errorf("%d image draws, want %d", n, 1+2+1+2+1+30)
	}
	var withMask int
	for _, img := range r.doc.Images {
		if img.Mask != nil {
			withMask++
		}
	}
	if withMask != 2 {
		t.Errorf("%d images with a soft mask, want the logo and the chart", withMask)
	}
	text := pagesText(r.doc)
	for _, want := range []string{"Analysis report", "Capture 7 is not embedded: remote images are never fetched", "Preview", "sample-29.bin", "End of report."} {
		if !strings.Contains(strings.Join(strings.Fields(text), " "), want) {
			t.Errorf("text lacks %q", want)
		}
	}
	if strings.Contains(text, "ACME Security") || strings.Contains(text, "Screenshot of") {
		t.Errorf("the alt text of a drawn image is in the text")
	}
	var uris []string
	for _, p := range r.doc.Pages {
		for _, l := range p.Links {
			uris = append(uris, l.URI)
		}
	}
	if len(uris) != 2 {
		t.Errorf("links %v, want the report link and the image link", uris)
	}

	tmpl := template.Must(template.New("04").Parse(string(raw)))
	var buf bytes.Buffer
	if _, err := mustNew(t, Options{}).RenderTemplate(context.Background(), tmpl, nil, &buf); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), r.data) {
		t.Error("RenderTemplate and Render differ")
	}
}
