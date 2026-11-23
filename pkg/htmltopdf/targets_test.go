package htmltopdf

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/pdf"
)

// blockingReader serves head, then blocks in Read until released and returns more data.
type blockingReader struct {
	head    io.Reader
	tail    string
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingReader) Read(p []byte) (int, error) {
	if n, err := b.head.Read(p); n > 0 || (err != nil && err != io.EOF) {
		return n, err
	}
	b.once.Do(func() { close(b.started) })
	<-b.release
	if b.tail == "" {
		return 0, io.EOF
	}
	n := copy(p, b.tail)
	b.tail = b.tail[n:]
	return n, nil
}

// Slow loris: a Read that returns only after the context is over ends the
// render with the context error at the next call.
func TestSlowSourceHonorsContext(t *testing.T) {
	c := mustNew(t, Options{Limits: Limits{Timeout: 200 * time.Millisecond}})
	for _, mode := range []string{"cancel", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			src := &blockingReader{head: strings.NewReader("<p>start"), tail: strings.Repeat("<p>more</p>", 100), started: make(chan struct{}), release: make(chan struct{})}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			type out struct {
				res *Result
				err error
			}
			done := make(chan out, 1)
			go func() {
				res, err := c.Render(ctx, src, io.Discard)
				done <- out{res, err}
			}()
			<-src.started
			if mode == "cancel" {
				cancel()
			} else {
				time.Sleep(300 * time.Millisecond)
			}
			close(src.release)
			select {
			case o := <-done:
				want := context.Canceled
				if mode == "timeout" {
					want = context.DeadlineExceeded
				}
				if !errors.Is(o.err, want) || o.res == nil {
					t.Errorf("got %v, want %v", o.err, want)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("Render did not return after the blocked Read returned")
			}
		})
	}
}

type blockingWriter struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	n       int
}

func (b *blockingWriter) Write(p []byte) (int, error) {
	b.n++
	if b.n == 2 {
		b.once.Do(func() { close(b.started) })
		<-b.release
	}
	return len(p), nil
}

func TestSlowDestinationHonorsContext(t *testing.T) {
	c := mustNew(t, Options{})
	dst := &blockingWriter{started: make(chan struct{}), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := c.Render(ctx, strings.NewReader(multiPageHTML()), dst)
		done <- err
	}()
	<-dst.started
	cancel()
	close(dst.release)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Render did not return after the blocked Write returned")
	}
}

type anyPanicReader struct{ v any }

func (p anyPanicReader) Read([]byte) (int, error) { panic(p.v) }

type stringer string

func (s stringer) String() string { return string(s) }

// Panic text is bounded and single-line whatever the panic value is.
func TestPanicMessageBoundedAndSingleLine(t *testing.T) {
	huge := strings.Repeat("line\n\r\x1b[31m", 1<<17)
	values := map[string]any{
		"string 1 MB":     huge,
		"error":           errors.New(huge),
		"stringer":        stringer(huge),
		"slice of string": []string{huge},
		"nil error":       error(nil),
		"integer":         42,
	}
	c := mustNew(t, Options{})
	for name, v := range values {
		t.Run(name, func(t *testing.T) {
			res, err := c.Render(context.Background(), anyPanicReader{v}, io.Discard)
			if !errors.Is(err, ErrInternal) || res == nil {
				t.Fatalf("got %v", err)
			}
			msg := err.Error()
			if len(msg) > 200 {
				t.Errorf("message of %d bytes", len(msg))
			}
			if len(msg) > 130 {
				t.Logf("message of %d bytes (relecteur target: about 120)", len(msg))
			}
			if strings.ContainsAny(msg, "\n\r\x1b") {
				t.Errorf("control characters in %q", msg)
			}
		})
	}
}

// <style media> evaluates a media query list against the print medium.
func TestStyleMediaAttributeApplication(t *testing.T) {
	c := mustNew(t, Options{})
	tests := []struct {
		media   string
		applies bool
		warn    bool
	}{
		{"", true, false},
		{"print", true, false},
		{"all", true, false},
		{"PRINT", true, false},
		{"screen", false, false},
		{"screen, print", true, false},
		{"not print", false, false},
		{"(min-width: 99999px)", false, false},
		{"(((", false, true},
		{"print{}", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.media, func(t *testing.T) {
			html := `<style media="` + tt.media + `">p{color:#ff0000}</style><p>x</p>`
			res, data := renderBytes(t, c, html)
			applied := strings.Contains(pagesContent(t, data), "1 0 0 rg")
			if applied != tt.applies {
				t.Errorf("applied = %v, want %v", applied, tt.applies)
			}
			var invalid bool
			for _, w := range res.Warnings {
				invalid = invalid || w.Code == WarnInvalidValue
			}
			if invalid != tt.warn {
				t.Errorf("invalid-value warning = %v, want %v (%+v)", invalid, tt.warn, res.Warnings)
			}
		})
	}
}

// The font caps are checked before any font is parsed: rejection is quick.
func TestFontCapsAreQuick(t *testing.T) {
	const MiB = 1 << 20
	tests := map[string]Options{
		"65 fonts":      {Fonts: make([]Font, 65)},
		"33 MiB font":   {Fonts: []Font{{Data: make([]byte, 33*MiB), Family: "x"}}},
		"129 MiB total": {Fonts: []Font{{Data: make([]byte, 32*MiB)}, {Data: make([]byte, 32*MiB)}, {Data: make([]byte, 32*MiB)}, {Data: make([]byte, 32*MiB)}, {Data: make([]byte, 1*MiB)}}},
		"1000 empty":    {Fonts: make([]Font, 1000)},
	}
	for name, opts := range tests {
		t.Run(name, func(t *testing.T) {
			start := time.Now()
			c, err := New(opts)
			if !errors.Is(err, ErrInvalidOptions) || c != nil {
				t.Fatalf("got %v", err)
			}
			if d := time.Since(start); d > time.Second {
				t.Errorf("rejection took %v", d)
			}
		})
	}
}

// A page whose uncompressed content exceeds pdf.MaxContentBytes is a limit error.
func TestPageContentOverMaxContentBytes(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a 16 MiB content stream")
	}
	var sb strings.Builder
	sb.WriteString("<p style='font-size:8px;margin:0'>")
	sb.WriteString(strings.Repeat("abcdefgh ", 450_000))
	sb.WriteString("</p>")
	c := mustNew(t, Options{
		ExtraCSS: "@page { size: 200in 200in; margin: 0 }",
		Limits:   Limits{MaxInputBytes: 8 << 20, MaxGlyphs: 10_000_000, MaxWork: 1 << 40, MaxOutputBytes: 256 << 20, Timeout: 90 * time.Second},
	})
	res, err := c.Render(context.Background(), strings.NewReader(sb.String()), io.Discard)
	var le *LimitError
	if !errors.Is(err, ErrLimit) || !errors.As(err, &le) {
		t.Fatalf("got %v (pages %d, bytes %d), want an ErrLimit (content over %d bytes)", err, res.Pages, res.Bytes, pdf.MaxContentBytes)
	}
	if le.Limit != LimitOutputBytes || le.Max != pdf.MaxContentBytes {
		t.Errorf("limit %s (max %d), want %s with the bound reached (%d)", le.Limit, le.Max, LimitOutputBytes, pdf.MaxContentBytes)
	}
}

// Invalid ExtraCSS, each flavour, is rejected by New.
func TestExtraCSSRejectionFlavours(t *testing.T) {
	for name, src := range map[string]string{
		"typo in @page descriptor": "@page { sizee: A4 }",
		"typo in @page margin box": "@page { @bottom-lefft { content: 'x' } }",
		"dropped declaration":      "p { color: red; width: banana }",
		"page size clamp":          "@page { size: 0.5in 0.5in }",
		"page margins clamp":       "@page { size: A4; margin: 50in }",
		"unbalanced block":         "@page { size: A4",
		"declaration outside rule": "color: red;",
	} {
		t.Run(name, func(t *testing.T) {
			c, err := New(Options{ExtraCSS: src})
			if !errors.Is(err, ErrInvalidOptions) || c != nil {
				t.Fatalf("got %v", err)
			}
		})
	}
}

// Result is consistent with the output on every error path that writes something.
func TestResultConsistencyOnErrors(t *testing.T) {
	c := mustNew(t, Options{ExtraCSS: "@page { size: 400px 300px; margin: 0 }", Limits: Limits{MaxPages: 3}})
	var buf bytes.Buffer
	res, err := c.Render(context.Background(), strings.NewReader("<style>p{foo:1}</style>"+strings.Repeat("<p style='padding-top:1000px'>x</p>", 6)), &buf)
	if !errors.Is(err, ErrLimit) {
		t.Fatalf("got %v", err)
	}
	if res.Pages != 3 || res.Bytes != int64(buf.Len()) || len(res.Warnings) == 0 {
		t.Errorf("result %+v for %d bytes written", res, buf.Len())
	}
}
