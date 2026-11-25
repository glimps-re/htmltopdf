package pdf

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
)

func destWriter(buf *bytes.Buffer) (*Writer, *budget.Budget) {
	b := newBudget(1 << 30)
	return NewWriter(buf, b), b
}

var destRect = [4]float64{10, 10, 50, 20}

// Destinations are named /D<n> in the order of first use, a SetDest allocating
// like a link; the id of the document never reaches the file.
func TestDestNamesAndPlacement(t *testing.T) {
	var buf bytes.Buffer
	w, _ := destWriter(&buf)
	hostile := "x) /A << /S /JavaScript >> ("
	p1, err := w.AddPage(200, 300, sampleContent(), false, []Link{{Rect: destRect, Dest: hostile}})
	if err != nil {
		t.Fatal(err)
	}
	if n, ok := w.Dest(hostile); !ok || n != "/D0" {
		t.Fatalf("first name %q %v", n, ok)
	}
	if err := w.SetDest("placed first", p1, 0, 5); err != nil {
		t.Fatal(err)
	}
	if n, _ := w.Dest("placed first"); n != "/D1" {
		t.Errorf("SetDest allocated %q, want /D1", n)
	}
	if err := w.SetDest(hostile, p1, 0, 250); err != nil {
		t.Fatal(err)
	}
	if err := w.SetDest(hostile, p1, 0, 1); err != nil { // the first call wins
		t.Fatal(err)
	}
	if err := w.Close(Info{}); err != nil {
		t.Fatal(err)
	}
	doc := pdftest.Validate(t, buf.Bytes())
	if d := doc.Dests["D0"]; d.Y != 250 || d.Page != 0 {
		t.Errorf("D0 %+v, want y 250", d)
	}
	if strings.Contains(buf.String(), "JavaScript") {
		t.Error("the id reached the file")
	}
}

func TestDestsInTheFile(t *testing.T) {
	var buf bytes.Buffer
	w, _ := destWriter(&buf)
	p1, err := w.AddPage(200, 300, sampleContent(), false, []Link{{Rect: destRect, Dest: "a"}, {Rect: destRect, Dest: "b"}, {Rect: destRect, Dest: "a"}})
	if err != nil {
		t.Fatal(err)
	}
	p2, err := w.AddPage(200, 400, sampleContent(), false, []Link{{Rect: destRect, URI: "https://a.b"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []struct {
		id   string
		page Ref
		y    float64
	}{{"a", p2, 150}, {"a", p1, 1}} {
		if err := w.SetDest(d.id, d.page, 0, d.y); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(Info{}); err != nil {
		t.Fatal(err)
	}
	doc := pdftest.Validate(t, buf.Bytes())
	if len(doc.Dests) != 2 {
		t.Fatalf("%d destinations: %+v", len(doc.Dests), doc.Dests)
	}
	if d := doc.Dests["D0"]; d.Page != 1 || d.Y != 150 || d.X != 0 {
		t.Errorf("a: %+v, want page 2 at 150", d)
	}
	if d := doc.Dests["D1"]; d.Page != 0 || d.Y != 300 {
		t.Errorf("b never placed: %+v, want the top of page 1", d)
	}
	var dests []string
	for _, l := range doc.Pages[0].Links {
		dests = append(dests, l.Dest)
	}
	if strings.Join(dests, ",") != "D0,D1,D0" {
		t.Errorf("links go to %v", dests)
	}
	if l := doc.Pages[1].Links[0]; l.URI != "https://a.b" || l.Dest != "" {
		t.Errorf("uri link %+v", l)
	}
}

// 10 000 destinations are kept; the link of the 10 001st is dropped with a
// clamped warning, and its SetDest ignored.
func TestDestLimit(t *testing.T) {
	for _, n := range []int{MaxDests, MaxDests + 1} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			var buf bytes.Buffer
			w, b := destWriter(&buf)
			links := make([]Link, n)
			for i := range links {
				links[i] = Link{Rect: destRect, Dest: fmt.Sprint("id", i)}
			}
			p, err := w.AddPage(200, 300, sampleContent(), false, links)
			if err != nil {
				t.Fatal(err)
			}
			if err := w.SetDest(fmt.Sprint("id", n-1), p, 0, 10); err != nil {
				t.Fatal(err)
			}
			if err := w.Close(Info{}); err != nil {
				t.Fatal(err)
			}
			doc := pdftest.Validate(t, buf.Bytes())
			want := min(n, MaxDests)
			if got := len(doc.Pages[0].Links); got != want {
				t.Errorf("%d annotations, want %d", got, want)
			}
			warned := false
			ws, _ := b.Warnings()
			for _, w := range ws {
				warned = warned || w.Detail == "destinations"
			}
			if warned != (n > MaxDests) {
				t.Errorf("warned %v for %d targets", warned, n)
			}
		})
	}
}

func TestInvalidDestLink(t *testing.T) {
	var buf bytes.Buffer
	w, _ := destWriter(&buf)
	if _, err := w.AddPage(100, 100, sampleContent(), false, []Link{{Rect: destRect, Dest: "a", URI: "https://a.b"}}); err == nil {
		t.Error("a link with a URI and a destination")
	}
	if err := w.SetDest("a", 1, 1e30, 0); err == nil {
		t.Error("coordinate out of range")
	}
}

// Link annotations are bounded per document: past MaxLinks the links are dropped
// with a clamped warning, whatever their kind.
func TestLinkLimit(t *testing.T) {
	var buf bytes.Buffer
	w, b := destWriter(&buf)
	links := make([]Link, MaxLinks+5000)
	for i := range links {
		if i%2 == 0 {
			links[i] = Link{Rect: destRect, URI: "https://a.b/"}
		} else {
			links[i] = Link{Rect: destRect, Dest: fmt.Sprint("id", i)}
		}
	}
	if _, err := w.AddPage(200, 300, sampleContent(), false, links); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(Info{}); err != nil {
		t.Fatal(err)
	}
	doc := pdftest.Validate(t, buf.Bytes())
	if got := len(doc.Pages[0].Links); got != MaxLinks {
		t.Errorf("%d annotations, want %d", got, MaxLinks)
	}
	warned := false
	ws, _ := b.Warnings()
	for _, x := range ws {
		warned = warned || x.Detail == "links"
	}
	if !warned {
		t.Error("no clamped warning for links")
	}
}

func TestDestIDLength(t *testing.T) {
	var buf bytes.Buffer
	w, _ := destWriter(&buf)
	if _, ok := w.Dest(strings.Repeat("a", MaxDestIDBytes)); !ok {
		t.Error("an id of MaxDestIDBytes refused")
	}
	if _, ok := w.Dest(strings.Repeat("a", MaxDestIDBytes+1)); ok {
		t.Error("an id over MaxDestIDBytes accepted")
	}
}
