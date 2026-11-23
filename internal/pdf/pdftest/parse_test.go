package pdftest_test

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/go-text/typesetting/font"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/pdf"
	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
)

func validDoc(tb testing.TB) []byte {
	tb.Helper()
	var buf bytes.Buffer
	w := pdf.NewWriter(&buf, budget.New(context.Background(), budget.Defaults(), nil))
	c := &pdf.Content{}
	c.FillColor(pdf.Color{R: 1, A: 0.5})
	c.Rect(0, 0, 10, 10)
	c.Fill()
	links := []pdf.Link{{Rect: [4]float64{0, 0, 5, 5}, URI: "https://example.com"}}
	if _, err := w.AddPage(100, 100, c, false, links); err != nil {
		tb.Fatal(err)
	}
	if err := w.Close(pdf.Info{Title: "T"}); err != nil {
		tb.Fatal(err)
	}
	return buf.Bytes()
}

func TestValidDocument(t *testing.T) {
	doc := pdftest.Validate(t, validDoc(t))
	if len(doc.Pages) != 1 || doc.Title != "T" || len(doc.ID) != 32 || doc.Objects != 7 {
		t.Fatalf("doc = %+v", doc)
	}
}

func TestParseRejects(t *testing.T) {
	valid := validDoc(t)
	rep := func(old, new string) []byte {
		if !bytes.Contains(valid, []byte(old)) {
			t.Fatalf("fixture has no %q", old)
		}
		return bytes.Replace(valid, []byte(old), []byte(new), 1)
	}
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"garbage", []byte("hello")},
		{"truncated", valid[:len(valid)/2]},
		{"no eof marker", valid[:len(valid)-6]},
		{"wrong version", rep("%PDF-1.7", "%PDF-1.4")},
		{"count mismatch", rep("/Count 1", "/Count 2")},
		{"size mismatch", rep("/Size 8", "/Size 9")},
		{"bad xref entry", rep(" 00000 n \n", " 00001 n \n")},
		{"xref offset moved", rep("0000000015 00000 n", "0000000016 00000 n")},
		{"bad startxref", rep("startxref\n", "startxref\n9")},
		{"missing parent", rep("/Parent 2 0 R", "/Parent 3 0 R")},
		{"id differs", rep("/ID [<", "/ID [<0")},
		{"not flate", rep("/FlateDecode", "/ASCIIHexDec")},
		{"length lies", rep("/Length ", "/Length 1")},
		{"not a catalog", rep("/Catalog", "/Catalogs")},
		{"link action", rep("/S /URI", "/S /GoTo")},
		{"name escape", rep("/Producer", "/Prod#75cer")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := pdftest.Parse(tt.data); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

func assemble(tb testing.TB, content string) []byte {
	tb.Helper()
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	if _, err := zw.Write([]byte(content)); err != nil {
		tb.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		tb.Fatal(err)
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Count 1 /Kids [3 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 10 10] /Resources 4 0 R /Contents 5 0 R >>",
		"<< >>",
		fmt.Sprintf("<< /Filter /FlateDecode /Length %d >>\nstream\n%s\nendstream", z.Len(), z.String()),
		"<< >>",
	}
	var out bytes.Buffer
	out.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	at := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&out, "%010d 00000 n \n", o)
	}
	sum := sha256.Sum256(out.Bytes())
	id := hex.EncodeToString(sum[:16])
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R /Info 6 0 R /ID [<%s> <%s>] >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, id, id, at)
	return out.Bytes()
}

func TestParseContentChecks(t *testing.T) {
	tests := []struct {
		content string
		ok      bool
	}{
		{"q\n1 0 0 rg\nQ\n", true},
		{"", true},
		{"Q", false},
		{"q q Q", false},
		{"/GT1 gs", false},
		{"gs", false},
		{"1 gs", false},
	}
	for _, tt := range tests {
		_, err := pdftest.Parse(assemble(t, tt.content))
		if (err == nil) != tt.ok {
			t.Errorf("content %q: err = %v", tt.content, err)
		}
	}
}

func TestValidatePDFCPUReportsError(t *testing.T) {
	if err := pdftest.ValidatePDFCPU([]byte("not a pdf")); err == nil {
		t.Fatal("accepted garbage")
	}
}

func FuzzParse(f *testing.F) {
	f.Add(validDoc(f))
	f.Add([]byte("%PDF-1.7\n%x\nstartxref\n5\n%%EOF\n"))
	f.Fuzz(func(_ *testing.T, data []byte) {
		_, _ = pdftest.Parse(data)
	})
}

func TestParseRejectsHostileNumbers(t *testing.T) {
	valid := validDoc(t)
	for _, tt := range []struct{ old, new string }{
		{"/Length ", "/Length 1e300 /X "},
		{"/Length ", "/Length 9223372036854775807 /X "},
		{"/Length ", "/Length -1 /X "},
		{"/Length ", "/Length 1.5 /X "},
		{"/Length ", "/Length -9223372036854775808 /X "},
		{"xref\n0 8\n", "xref\n0 9999999999\n"},
		{"xref\n0 8\n", "xref\n0 9223372036854775807\n"},
		{"xref\n0 8\n", "xref\n0 -5\n"},
	} {
		doc := bytes.Replace(valid, []byte(tt.old), []byte(tt.new), 1)
		if bytes.Equal(doc, valid) {
			t.Fatalf("fixture has no %q", tt.old)
		}
		if _, err := pdftest.Parse(doc); err == nil {
			t.Errorf("accepted %q", tt.new)
		}
	}
}

func TestParseChecksID(t *testing.T) {
	valid := validDoc(t)
	i := bytes.Index(valid, []byte("/ID [<")) + len("/ID [<")
	doc := bytes.Clone(valid)
	copy(doc[i:], bytes.Repeat([]byte("0"), 32))
	copy(doc[i+32+3:], bytes.Repeat([]byte("0"), 32))
	if _, err := pdftest.Parse(doc); err == nil {
		t.Fatal("constant /ID accepted")
	}
}

func TestParseTrailerNotDict(t *testing.T) {
	valid := validDoc(t)
	i := bytes.Index(valid, []byte("trailer\n")) + len("trailer\n")
	doc := bytes.Clone(valid)
	copy(doc[i:], "[ ")
	if _, err := pdftest.Parse(doc); err == nil || !strings.Contains(err.Error(), "xref") {
		t.Fatalf("err = %v", err)
	}
}

func fontDoc(tb testing.TB) []byte {
	tb.Helper()
	root, err := os.OpenRoot("../../../testdata/fonts")
	if err != nil {
		tb.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			tb.Error(err)
		}
	}()
	data, err := fs.ReadFile(root.FS(), "LiberationSans-Regular.ttf")
	if err != nil {
		tb.Fatal(err)
	}
	face, err := font.ParseTTF(bytes.NewReader(data))
	if err != nil {
		tb.Fatal(err)
	}
	var buf bytes.Buffer
	w := pdf.NewWriter(&buf, budget.New(context.Background(), budget.Defaults(), nil))
	id, err := w.AddFont(&pdf.FontProgram{Data: data, Name: "LiberationSans-Regular"})
	if err != nil {
		tb.Fatal(err)
	}
	var run []pdf.Glyph
	for _, r := range "Bonjour" {
		gid, _ := face.NominalGlyph(r)
		run = append(run, pdf.Glyph{ID: uint16(min(uint32(gid), math.MaxUint16)), Advance: 6, Text: string(r)})
	}
	c := &pdf.Content{}
	c.Glyphs(id, 10, 0, 0, run)
	if _, err := w.AddPage(100, 100, c, false, nil); err != nil {
		tb.Fatal(err)
	}
	if err := w.Close(pdf.Info{}); err != nil {
		tb.Fatal(err)
	}
	return buf.Bytes()
}

func TestFontDocument(t *testing.T) {
	doc := pdftest.Validate(t, fontDoc(t))
	f := doc.Fonts["F1"]
	if f == nil || f.BaseFont[6] != '+' || len(doc.Pages[0].Text) != 1 || doc.Pages[0].Text[0] != "Bonjour" || len(f.Widths) != 6 {
		t.Fatalf("doc = %+v, text %q", f, doc.Pages[0].Text)
	}
	got, err := pdftest.PDFCPUContent(fontDoc(t))
	if err != nil || !bytes.Equal(got[1], doc.Pages[0].Content) {
		t.Fatalf("pdfcpu content: %v", err)
	}
	if _, err := pdftest.PDFCPUContent([]byte("junk")); err == nil {
		t.Fatal("accepted junk")
	}
}

func TestFontStructureIsChecked(t *testing.T) {
	valid := fontDoc(t)
	rep := func(old, new string) []byte {
		if len(old) != len(new) || !bytes.Contains(valid, []byte(old)) {
			t.Fatalf("bad fixture edit %q", old)
		}
		return bytes.Replace(valid, []byte(old), []byte(new), 1)
	}
	// Edits that keep the length keep the xref valid; the ones above that
	// change it are rejected earlier, which is the point too.
	for name, doc := range map[string][]byte{
		"vertical encoding": rep("/Identity-H", "/Identity-V"),
		"not a CID type 2":  rep("/CIDFontType2", "/CIDFontType0"),
		"cid to gid map":    rep("/CIDToGIDMap /Identity", "/CIDToGIDMap /Identiti"),
		"descriptor type":   rep("/Type /FontDescriptor", "/Type /FontDescriptoR"),
		"length1":           rep("/Length1 ", "/Length2 "),
		"no subset tag":     rep("+LiberationSans-Regular", "-LiberationSans-Regular"),
		"type0 subtype":     rep("/Subtype /Type0", "/Subtype /Type1"),
		"no embedded font":  rep("/FontFile2", "/FontFile3"),
		"widths not array":  rep("/W [", "/W <"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := pdftest.Parse(doc); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}
