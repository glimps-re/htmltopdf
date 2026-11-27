package htmltopdf

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

// The off-page warning never changes the output. The digests are those of the
// code before the warning (ef6d2d5 with the non-test code of R-01 reverse
// applied, so the DejaVu Serif test fonts are included); regenerate them
// deliberately when a legitimate rendering change moves the bytes.
var templateDigests = map[string]string{
	"00-host-connector/large/options0": "28a3477e8e008691292f73c26928824609dcd0968df21dfa5804ec35d55a9280",
	"00-host-connector/large/options1": "22037fe5057fa251af54294d2eda0feea8f8cb69525efdda343743a741a50cbb",
	"00-host-connector/small/options0": "27f3936680eea0fefaafcaacabd954efb4e535024c68559deaf01c254633da0a",
	"00-host-connector/small/options1": "1457691a1f4faf8e5bf8853e887b89acf28339af49f75ce99c82b8f1829e5d93",
	"01-modern/large/options0":         "8d1f1b1d624f05c864022d050ff3b1071500313e32b71b21d9bda7bf637fdc0b",
	"01-modern/large/options1":         "adb0882e0b1197575d1eb4249b776bcff4cd8bf0ba51c914639e655c9ffab4cf",
	"01-modern/small/options0":         "f2c4b486432b21077f1f919e4236ee06b431442e8197b4d488463380b943eea0",
	"01-modern/small/options1":         "fb91182ccff47d5f875a1ae9179592d54a9e676775001573cb6991cc1054d4ce",
	"02-executive/large/options0":      "e42834a2fb2247b5baec59cdfeb8e92009ad126f26b8a356ba9f48ccc40e3bac",
	"02-executive/large/options1":      "550e083f68b8dbee2b521dbbaa0ba9dc118eb18f69e9ceefc55f08de60753e4c",
	"02-executive/small/options0":      "a9130373be92cef1834db2364b22f63913aea0371802f6401986dd4ad9b2f246",
	"02-executive/small/options1":      "8dc0deed578e3faa65f0a07fdde9c8ec2d508509787536b6740e7f403c3dd7ba",
	"03-analyst/large/options0":        "5ce2b8363bc071d930d04e900205c68e20c19532708496fa85b0760f7de4d319",
	"03-analyst/large/options1":        "fcc609194efdbc186a3a7232ec57d4a5d42374a29e0f8fceace373e1732b008d",
	"03-analyst/small/options0":        "8c13d24de0375069500c5944f4980f93d763a7b61538048b7358f987733d255e",
	"03-analyst/small/options1":        "961484189b1cc7e54477459f53488596a706f4345a2fbea5a2546917f5ef22a8",
	"04-images":                        "4e26c9409c641109d763d2917dc3c63689276f35a8dea0b4bea175ff3694c5ef",
}

func TestOffPageTemplatesBytesUnchanged(t *testing.T) {
	tpl := os.DirFS("../../testdata/templates")
	render := func(t *testing.T, key string, html []byte, o Options) {
		res, data := renderBytes(t, mustNew(t, o), string(html))
		if got := offPageWarnings(res.Warnings); len(got) != 0 {
			t.Errorf("text-off-page on a template: %+v", got)
		}
		want, ok := templateDigests[key]
		if !ok {
			t.Fatalf("no digest for %s", key)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != want {
			t.Errorf("PDF digest %s, want %s", got, want)
		}
	}
	for _, name := range []string{"00-host-connector", "01-modern", "02-executive", "03-analyst"} {
		for _, size := range []testcorpus.Size{testcorpus.Small, testcorpus.Large} {
			html, err := testcorpus.Report(tpl, name, size)
			if err != nil {
				t.Fatal(err)
			}
			for oi, o := range []Options{referenceOptions(t), m2Options(t)} {
				key := fmt.Sprintf("%s/%s/options%d", name, size, oi)
				t.Run(key, func(t *testing.T) { render(t, key, html, o) })
			}
		}
	}
	t.Run("04-images", func(t *testing.T) {
		html, err := fs.ReadFile(tpl, "04-images.html")
		if err != nil {
			t.Fatal(err)
		}
		render(t, "04-images", html, Options{})
	})
}

func textWords(tb testing.TB, data []byte) map[string]bool {
	tb.Helper()
	out := map[string]bool{}
	for _, w := range strings.Fields(pagesText(pdftest.Validate(tb, data))) {
		out[w] = true
	}
	return out
}

func TestOffPageHostileCorpus(t *testing.T) {
	variants := []struct {
		name string
		opts func(testing.TB) Options
	}{
		{"default", func(testing.TB) Options { return Options{} }},
		{"host-connector", referenceOptions},
	}
	for _, v := range variants {
		c := mustNew(t, v.opts(t))
		for _, tc := range testcorpus.OffPageCases() {
			t.Run(v.name+"/"+tc.Name, func(t *testing.T) {
				res, data := renderBytes(t, c, string(tc.HTML()))
				got := offPageWarnings(res.Warnings)
				reported := 0
				switch {
				case tc.Detail == "" && len(got) != 0:
					t.Fatalf("text-off-page %+v, want none", got)
				case tc.Detail != "" && (len(got) != 1 || got[0].Detail != tc.Detail):
					t.Fatalf("text-off-page %+v, want only %q (all: %+v)", got, tc.Detail, res.Warnings)
				case tc.Detail != "":
					reported = got[0].Count
					want := tc.Runs
					if tc.Repeats {
						want *= res.Pages
					}
					if tc.AtLeast && reported < want || !tc.AtLeast && reported != want {
						t.Errorf("count %d, want %d (at least: %v), %d pages", reported, want, tc.AtLeast, res.Pages)
					}
				}
				if tc.Pages != 0 && res.Pages != tc.Pages {
					t.Errorf("%d pages, want %d", res.Pages, tc.Pages)
				}
				present := textWords(t, data)
				for _, w := range tc.Visible {
					if !present[w] {
						t.Errorf("%q missing from the PDF, it is on the page", w)
					}
				}
				for _, w := range tc.Hidden {
					if present[w] {
						t.Errorf("%q is in the PDF, the page cannot show it", w)
					}
				}
				missing := 0
				for _, w := range tc.Markers {
					if !present[w] {
						missing++
					}
				}
				if missing > reported {
					t.Errorf("%d words are not in the PDF but only %d runs are reported", missing, reported)
				}
			})
		}
	}
}

func TestOffPageTableOverColumnLimit(t *testing.T) {
	html := "<table><tr>" + strings.Repeat("<td>zq</td>", 1001) + "</table>"
	res, err := mustNew(t, Options{}).Render(t.Context(), strings.NewReader(html), io.Discard)
	if err != nil {
		var le *LimitError
		if !errors.As(err, &le) {
			t.Fatalf("unexpected error %v", err)
		}
		return
	}
	clamped := slices.ContainsFunc(res.Warnings, func(w Warning) bool {
		return w.Code == WarnClamped && w.Detail == "table columns > 1000"
	})
	got := offPageWarnings(res.Warnings)
	if !clamped || len(got) != 1 || got[0].Detail != "right" {
		t.Fatalf("clamped %v, text-off-page %+v, want the columns clamped and right reported (all: %+v)", clamped, got, res.Warnings)
	}
}

func TestOffPageDeterministic(t *testing.T) {
	for _, tc := range testcorpus.OffPageCases() {
		t.Run(tc.Name, func(t *testing.T) {
			c := mustNew(t, Options{})
			var first []Warning
			var firstSum [32]byte
			for i := range 10 {
				res, data := renderBytes(t, c, string(tc.HTML()))
				sum := sha256.Sum256(data)
				if i == 0 {
					first, firstSum = res.Warnings, sum
					continue
				}
				if !slices.Equal(res.Warnings, first) || sum != firstSum {
					t.Fatalf("run %d: warnings %+v (first %+v), same bytes %v", i, res.Warnings, first, sum == firstSum)
				}
			}
		})
	}
}

const (
	inkVisible = iota
	inkOnly
	inkCulled
)

// Moving a word across the page edge goes from drawn and unreported, to drawn
// and reported while only the glyphs are off the page, to not drawn and
// reported; never back, and never silent.
func TestOffPageInkSweep(t *testing.T) {
	tests := []struct {
		name     string
		from, to int
	}{
		{"above", -200, 40},
		{"below", 100, 400},
	}
	c := mustNew(t, Options{})
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var states []int
			for top := tc.from; top <= tc.to; top += 2 {
				res, data := renderBytes(t, c, string(testcorpus.InkSweepDocument(top)))
				drawn := textWords(t, data)["zqink"]
				got := offPageWarnings(res.Warnings)
				switch {
				case len(got) > 1 || len(got) == 1 && (got[0].Detail != tc.name || got[0].Count != 1):
					t.Fatalf("top %d: %+v", top, got)
				case !drawn && len(got) == 0:
					t.Fatalf("top %d: word neither drawn nor reported", top)
				case drawn && len(got) == 0:
					states = append(states, inkVisible)
				case drawn:
					states = append(states, inkOnly)
				default:
					states = append(states, inkCulled)
				}
			}
			if tc.name == "above" {
				slices.Reverse(states)
			}
			if !slices.IsSorted(states) {
				t.Fatalf("states not monotone: %v", states)
			}
			for _, s := range []int{inkVisible, inkOnly, inkCulled} {
				if !slices.Contains(states, s) {
					t.Errorf("sweep never reached state %d: %v", s, states)
				}
			}
		})
	}
}
