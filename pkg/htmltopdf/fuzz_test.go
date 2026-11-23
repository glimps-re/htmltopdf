package htmltopdf

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

// FuzzRender: whatever the input, Render does not panic, returns a Result, and
// either succeeds or fails with a documented error. Limits are small so that
// each execution stays cheap.
func FuzzRender(f *testing.F) {
	for _, s := range []string{
		"", "<p>x", reportHTML, "<style>p{color:var(--a)}:root{--a:red}</style><p>x</p>",
		"<table><tr><td>a<td>b</table>", "<div style='width:1e999px;margin:-99999px'>x</div>",
		"<a href='javascript:1'>x</a><img src=x>", "<style>@page{size:1px 1px;margin:99in}</style>x",
		"<p style='font-size:0;line-height:-1;padding:1e9px'>\x00\xff\xfe</p>",
		"<title>\x00)>></title><body>\u202e\ufffe", "<div><div><div>" + string(bytes.Repeat([]byte("<b>"), 2000)),
	} {
		f.Add([]byte(s))
	}
	hc := testcorpus.Hostile(budget.Defaults())
	for i, tc := range hc {
		if i%6 == 0 {
			if h := tc.HTML(); len(h) < 64<<10 {
				f.Add(h)
			}
		}
	}
	c, err := New(Options{Limits: Limits{
		MaxInputBytes: 256 << 10, MaxNodes: 5000, MaxCSSBytes: 64 << 10, MaxSelectors: 500, MaxStyles: 2000,
		MaxBoxes: 10_000, MaxGlyphs: 100_000, MaxPages: 20, MaxOutputBytes: 8 << 20, MaxWork: 5_000_000,
		Timeout: 5 * time.Second,
	}})
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var buf bytes.Buffer
		res, err := c.Render(context.Background(), bytes.NewReader(data), &buf)
		if res == nil {
			t.Fatal("nil Result")
		}
		switch {
		case err == nil:
			if res.Pages < 1 || res.Bytes != int64(buf.Len()) || !bytes.HasPrefix(buf.Bytes(), []byte("%PDF-")) || !bytes.HasSuffix(bytes.TrimSpace(buf.Bytes()), []byte("%%EOF")) {
				t.Fatalf("bad success: %+v, %d bytes", res, buf.Len())
			}
		case errors.Is(err, ErrLimit), errors.Is(err, context.DeadlineExceeded):
		default:
			t.Fatalf("unexpected error: %v", err)
		}
		_ = io.Discard
	})
}
