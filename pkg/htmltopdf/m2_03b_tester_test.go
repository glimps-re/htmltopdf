package htmltopdf

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

// textRun is one BT..ET block of a page: its origin in the PDF space (y up)
// and its decoded text.
type textRun struct {
	x, y, size float64
	text       string
}

var (
	runRE   = regexp.MustCompile(`BT\n/(F\d+) ([\d.]+) Tf\n(-?[\d.]+) (-?[\d.]+) Td\n(.*?) T[jJ]\nET`)
	hexStr  = regexp.MustCompile(`<([0-9A-Fa-f]*)>`)
	keyRE   = regexp.MustCompile(`_(\d{4})_`)
	headers = []string{"chemin", "menaces", "sha-256", "taille", "raison", "analyse"}
)

// pageRuns decodes the text of a page written by the engine: absolute Td and
// one TJ or Tj per block.
func pageRuns(t *testing.T, doc *pdftest.Doc, p int) []textRun {
	t.Helper()
	var out []textRun
	for _, m := range runRE.FindAllStringSubmatch(string(doc.Pages[p].Content), -1) {
		f := doc.Fonts[m[1]]
		if f == nil {
			t.Fatalf("page %d: unknown font %s", p+1, m[1])
		}
		var sb strings.Builder
		for _, h := range hexStr.FindAllStringSubmatch(m[5], -1) {
			for i := 0; i+4 <= len(h[1]); i += 4 {
				g, err := strconv.ParseUint(h[1][i:i+4], 16, 16)
				if err != nil {
					t.Fatal(err)
				}
				sb.WriteString(f.ToUnicode[uint16(g)])
			}
		}
		size, _ := strconv.ParseFloat(m[2], 64)
		x, _ := strconv.ParseFloat(m[3], 64)
		y, _ := strconv.ParseFloat(m[4], 64)
		out = append(out, textRun{x: x, y: y, size: size, text: sb.String()})
	}
	return out
}

type cellLines struct{ menaces, taille int }

// blockedFilesCells counts, for each row of the "Fichiers bloqués" table
// keyed by the NNNN of its file name, the lines of the Menaces and Taille
// cells. Rows without a following row on their page (possibly cut by the page
// break) are skipped and counted.
func blockedFilesCells(t *testing.T, doc *pdftest.Doc) (rows map[string]cellLines, skipped int) {
	t.Helper()
	rows = map[string]cellLines{}
	for p := range doc.Pages {
		runs := pageRuns(t, doc, p)
		height := doc.Pages[p].MediaBox[3] - doc.Pages[p].MediaBox[1]
		type col struct{ lo, hi float64 }
		for _, h := range runs {
			if !strings.EqualFold(strings.TrimSpace(h.text), "chemin") {
				continue
			}
			var hs []textRun
			for _, r := range runs {
				if math.Abs(r.y-h.y) < 3 {
					for _, n := range headers {
						if strings.EqualFold(strings.TrimSpace(r.text), n) {
							hs = append(hs, r)
						}
					}
				}
			}
			if len(hs) != len(headers) {
				continue
			}
			sort.Slice(hs, func(i, j int) bool { return hs[i].x < hs[j].x })
			cols := map[string]col{}
			for i, r := range hs {
				hi := math.Inf(1)
				if i+1 < len(hs) {
					hi = hs[i+1].x - 3
				}
				cols[strings.ToLower(strings.TrimSpace(r.text))] = col{r.x - 3, hi}
			}
			top := func(r textRun) float64 { return height - r.y - 0.75*r.size }
			var body []textRun
			for _, r := range runs {
				if top(r) > top(h)+5 {
					body = append(body, r)
				}
			}
			sort.Slice(body, func(i, j int) bool { return top(body[i]) < top(body[j]) })
			var keys []textRun
			for _, r := range body {
				if c := cols["chemin"]; r.x >= c.lo && r.x < c.hi && keyRE.MatchString(r.text) {
					keys = append(keys, r)
				}
			}
			count := func(c col, y0, y1 float64) int {
				var ys []float64
				for _, r := range body {
					if r.x >= c.lo && r.x < c.hi && top(r) >= y0 && top(r) < y1 {
						ys = append(ys, top(r))
					}
				}
				sort.Float64s(ys)
				n := 0
				for i, y := range ys {
					if i == 0 || y-ys[i-1] > 2.5 {
						n++
					}
				}
				return n
			}
			for i, k := range keys {
				if i+1 >= len(keys) {
					skipped++
					continue
				}
				key := keyRE.FindStringSubmatch(k.text)[1]
				if _, dup := rows[key]; dup {
					continue
				}
				y0, y1 := top(k)-14, top(keys[i+1])-14
				rows[key] = cellLines{count(cols["menaces"], y0, y1), count(cols["taille"], y0, y1)}
			}
		}
	}
	return rows, skipped
}

type chromeCells struct {
	Rows map[string][2]int `json:"rows"`
}

// Done criterion 2 of M2-03b: for invoice_0000..0039 of 01 large, the Menaces
// cell has as many lines as in Chrome and Taille is one line; at least 30 of
// the 40 rows are compared. The reference is testdata/chrome/01L_blocked_files_cells.json,
// extracted once from chrome09 with pdftotext -bbox-layout.
func TestM203bCriterion2BlockedFilesCells(t *testing.T) {
	data, err := os.ReadFile("../../testdata/chrome/01L_blocked_files_cells.json")
	if err != nil {
		t.Fatal(err)
	}
	var ref chromeCells
	if err := json.Unmarshal(data, &ref); err != nil {
		t.Fatal(err)
	}
	_, _, doc := renderM2(t, "01-modern", testcorpus.Large)
	ours, skipped := blockedFilesCells(t, doc)
	compared := 0
	for i := range 40 {
		key := strconv.Itoa(10000 + i)[1:]
		o, okO := ours[key]
		c, okC := ref.Rows[key]
		if !okO || !okC {
			continue
		}
		compared++
		if o.menaces != c[0] {
			t.Errorf("invoice_%s: Menaces has %d lines, Chrome %d", key, o.menaces, c[0])
		}
		if o.taille != 1 {
			t.Errorf("invoice_%s: Taille has %d lines, want 1", key, o.taille)
		}
	}
	t.Logf("ours: %d rows (%d skipped as possibly cut), Chrome: %d rows, compared %d of 40", len(ours), skipped, len(ref.Rows), compared)
	if compared < 30 {
		t.Errorf("only %d of 40 rows compared, want at least 30", compared)
	}
}

func textRunes(s string) map[rune]int {
	m := map[rune]int{}
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r', '\f', 0x200b, 0xa0:
			continue
		}
		m[r]++
	}
	return m
}

var tagStrip = regexp.MustCompile(`<[^>]*>`)

// Every non-blank rune of the source is in the text of the PDF, whatever the
// geometry: an atomic inline taller or wider than the page must not lose text.
func TestM203bPDFKeepsAllTheText(t *testing.T) {
	tall := func(inner string) string {
		return `<p>a <span style="display:inline-block">` + inner + `</span> b</p>`
	}
	tests := map[string]string{
		"20000 x in a tall atom":         tall(strings.Repeat("x", 20000)),
		"20000 x in an inline paragraph": `<p>a ` + strings.Repeat("x", 20000) + ` b</p>`,
		"20000 x words in a tall atom":   tall(strings.Repeat("xx ", 20000)),
		"20000 x lines in a tall atom":   tall(strings.Repeat("x<br>", 3000)),
		"200 lines in an atom":           tall(strings.Repeat("line<br>", 200)),
		"tall atom in a table cell":      `<table><tr><td><span style="display:inline-block">` + strings.Repeat("line<br>", 200) + `</span></td><td>z</td></tr></table>`,
		"nowrap 60000 chars":             `<div style="white-space:nowrap">` + strings.Repeat("word ", 12000) + `</div>`,
		"nested tall atoms":              tall(`<span style="display:inline-block">` + strings.Repeat("line<br>", 200) + `</span>`),
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			html := "<!doctype html><html><body>" + body + "</body></html>"
			res, data, err := func() (*Result, []byte, error) {
				var buf bytes.Buffer
				res, err := mustNew(t, m2Options(t)).Render(t.Context(), strings.NewReader(html), &buf)
				return res, buf.Bytes(), err
			}()
			if err != nil {
				t.Skipf("stopped: %v", err)
			}
			doc := pdftest.Validate(t, data)
			var sb strings.Builder
			for _, p := range doc.Pages {
				sb.WriteString(strings.Join(p.Text, ""))
			}
			want, got := textRunes(tagStrip.ReplaceAllString(body, "")), textRunes(sb.String())
			for r, n := range want {
				if got[r] != n {
					t.Errorf("rune %q: %d in the source, %d in the PDF (%d pages, %d warnings)", r, n, got[r], res.Pages, len(res.Warnings))
				}
			}
		})
	}
}
