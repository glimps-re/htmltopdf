package box

import (
	"strings"
	"testing"

	"golang.org/x/net/html/atom"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/dom"
)

func TestCleanHref(t *testing.T) {
	long := func(n int) string { return "https://a.test/" + strings.Repeat("x", n-len("https://a.test/")) }
	tests := []struct {
		name    string
		in      string
		want    string
		blocked string
	}{
		{"https", "https://example.com/a?b=c#d", "https://example.com/a?b=c#d", ""},
		{"http", "http://example.com", "http://example.com", ""},
		{"mailto", "mailto:a@b.c?subject=x", "mailto:a@b.c?subject=x", ""},
		{"scheme is lowercased, the rest is not", "HtTpS://Example.COM/Path", "https://Example.COM/Path", ""},
		{"leading C0 controls", "\x01\x02https://a.test/", "https://a.test/", ""},
		{"trailing controls and spaces", "https://a.test/ \t\n\x00", "https://a.test/", ""},
		{"leading control before javascript", "\x01javascript:alert(1)", "", "scheme javascript"},
		{"mixed case javascript", "JaVaScRiPt:alert(1)", "", "scheme javascript"},
		{"javascript with spaces around", "  javascript:alert(1)  ", "", "scheme javascript"},
		{"tab inside the scheme", "java\tscript:alert(1)", "", "control character in the target"},
		{"newline inside the scheme", "java\nscript:alert(1)", "", "control character in the target"},
		{"NUL inside", "https://a.test/\x00x", "", "control character in the target"},
		{"DEL inside", "https://a.test/\x7fx", "", "control character in the target"},
		{"vbscript", "vbscript:x", "", "scheme vbscript"},
		{"data", "data:text/html;base64,AAAA", "", "scheme data"},
		{"file", "file:///etc/passwd", "", "scheme file"},
		{"ftp", "ftp://a.test/", "", "scheme ftp"},
		{"tel", "tel:+33123456789", "", "scheme tel"},
		{"relative path", "/a/b", "", "no scheme"},
		{"relative file", "page.html", "", "no scheme"},
		{"scheme-relative", "//a.test/x", "", "no scheme"},
		{"colon after a slash", "a/b:c", "", "no scheme"},
		{"digit first scheme", "1http://a.test", "", "no scheme"},
		{"empty scheme", ":x", "", "no scheme"},
		{"fragment only", "#top", "", ""},
		{"empty", "", "", ""},
		{"only spaces", "   ", "", ""},
		{"space is encoded", "https://a.test/a b", "https://a.test/a%20b", ""},
		{"non ASCII is encoded as UTF-8", "https://a.test/é€", "https://a.test/%C3%A9%E2%82%AC", ""},
		{"existing escapes are kept", "https://a.test/%C3%A9", "https://a.test/%C3%A9", ""},
		{"at the limit", long(maxHrefBytes), long(maxHrefBytes), ""},
		{"just under", long(maxHrefBytes - 1), long(maxHrefBytes - 1), ""},
		{"over", long(maxHrefBytes + 1), "", "target over 2048 bytes"},
		{"over only after encoding", "https://a.test/" + strings.Repeat("é", 1000), "", "https target over 2048 bytes once encoded"},
		{"huge target is refused at once", "https://a.test/" + strings.Repeat("é", 1<<20), "", "target over 2048 bytes"},
		{"scheme of 16 bytes", "abcdefghijklmnop:x", "", "scheme abcdefghijklmnop"},
		{"scheme of 17 bytes", "abcdefghijklmnopq:x", "", "no scheme"},
		{"huge scheme", strings.Repeat("a", 1000) + ":x", "", "no scheme"},
		{"at the limit after encoding", "https://a.test/" + strings.Repeat("é", 100) + strings.Repeat("x", maxHrefBytes-len("https://a.test/")-600), "https://a.test/" + strings.Repeat("%C3%A9", 100) + strings.Repeat("x", maxHrefBytes-len("https://a.test/")-600), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, blocked := cleanHref(tt.in)
			if tt.name == "at the limit after encoding" {
				if blocked != "" || len(got) != maxHrefBytes {
					t.Fatalf("len %d, blocked %q", len(got), blocked)
				}
				return
			}
			if got != tt.want || blocked != tt.blocked {
				t.Fatalf("got %q, %q; want %q, %q", got, blocked, tt.want, tt.blocked)
			}
		})
	}
}

func TestCleanHrefOutputIsPrintableASCII(t *testing.T) {
	for _, in := range []string{"https://a.test/  é\u202e\u0085x", "mailto:é@a.test", "https://a.test/\U0001F600"} {
		got, blocked := cleanHref(in)
		if blocked != "" {
			t.Fatalf("%q blocked: %s", in, blocked)
		}
		for i := 0; i < len(got); i++ {
			if got[i] <= 0x20 || got[i] >= 0x7f {
				t.Fatalf("%q gives byte %#x", in, got[i])
			}
		}
	}
}

// anchorDoc is html > body > a[href] > "x", built by hand so that the target
// can hold any byte (the HTML parser would not let a control character through).
func anchorDoc(href string) *dom.Document {
	return &dom.Document{
		Elements: []dom.Element{
			{Tag: atom.Html, Parent: -1, FirstChild: dom.ElementRef(1), NextSibling: dom.NoNode, PrevElement: -1, SiblingCount: 1},
			{Tag: atom.Body, Parent: 0, FirstChild: dom.ElementRef(2), NextSibling: dom.NoNode, PrevElement: -1, SiblingCount: 1},
			{
				Tag: atom.A, Parent: 1, FirstChild: dom.TextRef(0), NextSibling: dom.NoNode, PrevElement: -1, SiblingCount: 1,
				Attrs: []dom.Attr{{Key: "href", Val: href}},
			},
		},
		Texts: []dom.Text{{Data: "x", Parent: 2, NextSibling: dom.NoNode}},
	}
}

func hasHref(b *Box) bool {
	if b.Href != "" {
		return true
	}
	for _, c := range b.Children {
		if hasHref(c) {
			return true
		}
	}
	return false
}

// The done criterion: dangerous links are dropped by Build, with a warning.
func TestBuildDropsDangerousLinks(t *testing.T) {
	for _, href := range []string{
		"JaVaScRiPt:alert(1)", "\x01javascript:alert(1)", "\x00javascript:alert(1)", "java\tscript:alert(1)",
		"java\nscript:alert(1)", " \x1b javascript:alert(1)", "data:text/html,x", "vbscript:x", "file:///etc/passwd", "/relative",
	} {
		t.Run(strings.ToLower(strings.NewReplacer("\x00", "NUL", "\x01", "SOH", "\t", "TAB", "\n", "LF", "\x1b", "ESC").Replace(href)), func(t *testing.T) {
			b := testBudget(nil)
			d := anchorDoc(href)
			root, err := Build(d, newStyler(t, d, b), b)
			if err != nil {
				t.Fatal(err)
			}
			if hasHref(root) {
				t.Fatal("a box kept the target")
			}
			var blocked bool
			for _, w := range warnings(b) {
				blocked = blocked || strings.HasPrefix(w, string(budget.WarnBlockedLink)+":")
			}
			if !blocked {
				t.Fatalf("no blocked-link warning: %v", warnings(b))
			}
		})
	}
	// And a good one through the same path.
	b := testBudget(nil)
	d := anchorDoc("\x01 https://a.test/ok ")
	root, err := Build(d, newStyler(t, d, b), b)
	if err != nil || !hasHref(root) || len(warnings(b)) != 0 {
		t.Fatalf("good link: err %v, warnings %v", err, warnings(b))
	}
}
