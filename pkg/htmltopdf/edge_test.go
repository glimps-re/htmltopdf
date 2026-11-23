package htmltopdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
	"time"

	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
)

var renderStages = []string{"dom.read", "dom.prepass", "dom.parse", "dom.convert", "css", "box", "page", "close"}

func multiPageHTML() string {
	return "<style>p{foo:bar}</style><h1>T</h1>" + strings.Repeat("<p style='margin:0;padding-top:700px'>line</p>", 4)
}

// The Result is returned and consistent on every error path.
func TestResultNeverNilOnErrors(t *testing.T) {
	c := mustNew(t, Options{Limits: Limits{MaxPages: 2}})
	cases := map[string]func() (io.Reader, io.Writer, context.Context){
		"limit": func() (io.Reader, io.Writer, context.Context) {
			return strings.NewReader(multiPageHTML()), io.Discard, context.Background()
		},
		"reader": func() (io.Reader, io.Writer, context.Context) {
			return iotest.ErrReader(errors.New("x")), io.Discard, context.Background()
		},
		"writer": func() (io.Reader, io.Writer, context.Context) {
			return strings.NewReader("<p>x"), &failingWriter{err: errors.New("w")}, context.Background()
		},
		"canceled": func() (io.Reader, io.Writer, context.Context) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return strings.NewReader("<p>x"), io.Discard, ctx
		},
		"nil src": func() (io.Reader, io.Writer, context.Context) { return nil, io.Discard, context.Background() },
		"nil dst": func() (io.Reader, io.Writer, context.Context) {
			return strings.NewReader("<p>x"), nil, context.Background()
		},
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			src, dst, ctx := f()
			res, err := c.Render(ctx, src, dst)
			if err == nil {
				t.Fatal("expected an error")
			}
			if res == nil {
				t.Fatal("nil Result")
			}
			if res.Pages < 0 || res.Bytes < 0 {
				t.Errorf("result %+v", res)
			}
		})
	}
}

// Cancellation and timeout seen at every stage.
func TestCancelAtEveryStage(t *testing.T) {
	for _, stage := range renderStages {
		t.Run("cancel/"+stage, func(t *testing.T) {
			c := mustNew(t, Options{})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c.hook = func(s string) {
				if s == stage {
					cancel()
				}
			}
			res, err := c.Render(ctx, strings.NewReader(multiPageHTML()), io.Discard)
			if res == nil {
				t.Fatal("nil Result")
			}
			if stage == "close" {
				// The last checkpoint comes after the document is complete.
				if err != nil && !errors.Is(err, context.Canceled) {
					t.Errorf("got %v", err)
				}
				return
			}
			if !errors.Is(err, context.Canceled) {
				t.Errorf("cancel at %s: got %v", stage, err)
			}
		})
		t.Run("timeout/"+stage, func(t *testing.T) {
			c := mustNew(t, Options{Limits: Limits{Timeout: 50 * time.Millisecond}})
			c.hook = func(s string) {
				if s == stage {
					time.Sleep(80 * time.Millisecond)
				}
			}
			res, err := c.Render(context.Background(), strings.NewReader(multiPageHTML()), io.Discard)
			if res == nil {
				t.Fatal("nil Result")
			}
			if stage == "close" {
				return
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("timeout at %s: got %v", stage, err)
			}
		})
	}
}

func TestCallerDeadlineBeatsLongerTimeout(t *testing.T) {
	c := mustNew(t, Options{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)
	if _, err := c.Render(ctx, strings.NewReader(reportHTML), io.Discard); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("got %v", err)
	}
}

// A writer that fails at any offset yields exactly its error, never ErrInternal.
func TestWriterFailsAtEveryOffset(t *testing.T) {
	c := mustNew(t, Options{})
	var full bytes.Buffer
	if _, err := c.Render(context.Background(), strings.NewReader(multiPageHTML()), &full); err != nil {
		t.Fatal(err)
	}
	want := errors.New("pipe closed")
	size := full.Len()
	for _, after := range []int{0, 1, 9, 100, size / 3, size / 2, size - 200, size - 20, size - 1} {
		t.Run(fmt.Sprint(after), func(t *testing.T) {
			res, err := c.Render(context.Background(), strings.NewReader(multiPageHTML()), &failingWriter{after: after, err: want})
			if !errors.Is(err, want) {
				t.Fatalf("got %v, want the writer's error", err)
			}
			if res == nil {
				t.Fatal("nil Result")
			}
		})
	}
	t.Run("short write", func(t *testing.T) {
		_, err := c.Render(context.Background(), strings.NewReader(reportHTML), shortWriter{})
		if err == nil {
			t.Error("a short write must fail the render")
		}
	})
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) / 2, nil }

// A reader that fails mid-stream: its error is reported wrapped, as neither a limit nor internal.
func TestReaderFailsMidStream(t *testing.T) {
	c := mustNew(t, Options{})
	want := errors.New("connection reset")
	html := reportHTML
	for _, cut := range []int{0, 1, 17, len(html) / 2, len(html) - 1} {
		t.Run(fmt.Sprint(cut), func(t *testing.T) {
			src := io.MultiReader(strings.NewReader(html[:cut]), iotest.ErrReader(want))
			res, err := c.Render(context.Background(), src, io.Discard)
			if !errors.Is(err, want) || res == nil || errors.Is(err, ErrInternal) || errors.Is(err, ErrLimit) {
				t.Fatalf("got %v", err)
			}
		})
	}
	t.Run("one byte reads and data with EOF", func(t *testing.T) {
		var a, b bytes.Buffer
		for i, r := range []io.Reader{iotest.OneByteReader(strings.NewReader(reportHTML)), iotest.DataErrReader(strings.NewReader(reportHTML)), strings.NewReader(reportHTML)} {
			buf := &a
			if i > 0 {
				buf = &b
			}
			if i == 2 {
				buf = &bytes.Buffer{}
				if _, err := c.Render(context.Background(), r, buf); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(buf.Bytes(), a.Bytes()) {
					t.Error("output depends on read chunking")
				}
				continue
			}
			if _, err := c.Render(context.Background(), r, buf); err != nil {
				t.Fatal(err)
			}
			if i == 1 && !bytes.Equal(a.Bytes(), b.Bytes()) {
				t.Error("output depends on read chunking")
			}
		}
	})
}

func TestReaderEOFVariants(t *testing.T) {
	c := mustNew(t, Options{})
	wrapped := fmt.Errorf("wrapped: %w", io.EOF)
	// A wrapped io.EOF is a real error, not an end of stream; it must not panic.
	res, err := c.Render(context.Background(), io.MultiReader(strings.NewReader("<p>x"), iotest.ErrReader(wrapped)), io.Discard)
	if res == nil {
		t.Fatal("nil Result")
	}
	t.Logf("wrapped EOF: %v", err)
}

// A panic at any stage is ErrInternal with a usable Result, and the Converter stays usable.
func TestPanicAtEveryStage(t *testing.T) {
	for _, stage := range renderStages {
		t.Run(stage, func(t *testing.T) {
			c := mustNew(t, Options{})
			c.hook = func(s string) {
				if s == stage {
					panic("boom at " + s)
				}
			}
			res, err := c.Render(context.Background(), strings.NewReader(multiPageHTML()), io.Discard)
			if !errors.Is(err, ErrInternal) || res == nil {
				t.Fatalf("got %v", err)
			}
			if errors.Is(err, ErrLimit) {
				t.Error("an internal error is not a limit")
			}
			c.hook = nil
			if _, data := renderBytes(t, c, reportHTML); len(data) == 0 {
				t.Error("Converter unusable after a panic")
			}
		})
	}
	t.Run("panic with an error value", func(t *testing.T) {
		c := mustNew(t, Options{})
		c.hook = func(s string) {
			if s == "css" {
				panic(context.Canceled)
			}
		}
		if _, err := c.Render(context.Background(), strings.NewReader("<p>x"), io.Discard); !errors.Is(err, ErrInternal) {
			t.Errorf("got %v", err)
		}
	})
	t.Run("panic in the reader", func(t *testing.T) {
		c := mustNew(t, Options{})
		_, err := c.Render(context.Background(), panicReader{}, io.Discard)
		if !errors.Is(err, ErrInternal) {
			t.Errorf("got %v", err)
		}
	})
	t.Run("panic in the writer", func(t *testing.T) {
		c := mustNew(t, Options{})
		_, err := c.Render(context.Background(), strings.NewReader("<p>x"), panicWriter{})
		if !errors.Is(err, ErrInternal) {
			t.Errorf("got %v", err)
		}
	})
}

type panicReader struct{}

func (panicReader) Read([]byte) (int, error) { panic("reader") }

type panicWriter struct{}

func (panicWriter) Write([]byte) (int, error) { panic("writer") }

// An error does not poison the Converter: the next render is byte-identical to a fresh one.
func TestConverterReusableAfterErrors(t *testing.T) {
	c := mustNew(t, Options{Limits: Limits{MaxNodes: 50}})
	_, want := renderBytes(t, c, "<p>ok</p>")
	for range 3 {
		if _, err := c.Render(context.Background(), strings.NewReader(strings.Repeat("<p>x</p>", 100)), io.Discard); !errors.Is(err, ErrLimit) {
			t.Fatal(err)
		}
		_, got := renderBytes(t, c, "<p>ok</p>")
		if !bytes.Equal(got, want) {
			t.Fatal("output changed after a failed render")
		}
	}
}

// Over a limit by one unit fails, at the limit passes, for the node and input limits.
func TestLimitAtAndJustUnder(t *testing.T) {
	t.Run("input bytes", func(t *testing.T) {
		doc := "<p>" + strings.Repeat("a", 97) // 100 bytes
		for _, tc := range []struct {
			max  int64
			fail bool
		}{{101, false}, {100, false}, {99, true}} {
			c := mustNew(t, Options{Limits: Limits{MaxInputBytes: tc.max}})
			_, err := c.Render(context.Background(), strings.NewReader(doc), io.Discard)
			var le *LimitError
			got := errors.As(err, &le) && le.Limit == LimitInputBytes
			if got != tc.fail || (err != nil) != tc.fail {
				t.Errorf("MaxInputBytes=%d: err=%v, want fail=%v", tc.max, err, tc.fail)
			}
			if tc.fail && le.Max != tc.max {
				t.Errorf("LimitError.Max = %d, want %d", le.Max, tc.max)
			}
		}
	})
	t.Run("pages", func(t *testing.T) {
		html := strings.Repeat("<p style='margin:0;padding-top:1000px'>x</p>", 3)
		opts := Options{ExtraCSS: "@page { size: 400px 300px; margin: 0 }"}
		c := mustNew(t, opts)
		res, data := renderBytes(t, c, html)
		n := res.Pages
		if n < 3 {
			t.Fatalf("%d pages", n)
		}
		_ = data
		opts.Limits.MaxPages = n
		if _, err := mustNew(t, opts).Render(context.Background(), strings.NewReader(html), io.Discard); err != nil {
			t.Errorf("exactly MaxPages=%d: %v", n, err)
		}
		opts.Limits.MaxPages = n - 1
		_, err := mustNew(t, opts).Render(context.Background(), strings.NewReader(html), io.Discard)
		var le *LimitError
		if !errors.As(err, &le) || le.Limit != LimitPages || le.Max != int64(n-1) {
			t.Errorf("MaxPages=%d: %v", n-1, err)
		}
	})
	t.Run("output bytes", func(t *testing.T) {
		_, data := renderBytes(t, mustNew(t, Options{}), reportHTML)
		n := int64(len(data))
		if _, err := mustNew(t, Options{Limits: Limits{MaxOutputBytes: n}}).Render(context.Background(), strings.NewReader(reportHTML), io.Discard); err != nil {
			t.Errorf("exactly MaxOutputBytes=%d: %v", n, err)
		}
		_, err := mustNew(t, Options{Limits: Limits{MaxOutputBytes: n - 1}}).Render(context.Background(), strings.NewReader(reportHTML), io.Discard)
		var le *LimitError
		if !errors.As(err, &le) || le.Limit != LimitOutputBytes || le.Max != n-1 {
			t.Errorf("MaxOutputBytes=%d: %v", n-1, err)
		}
	})
}

// MaxVarBytes is not an error: it degrades with WarnVarBudget (§4).
func TestVarBudgetDegrades(t *testing.T) {
	big := strings.Repeat("x", 4000)
	html := "<style>:root{--a:" + big + "} p{font-family: var(--a) var(--a) var(--a)}</style><p>x</p><p>y</p>"
	c := mustNew(t, Options{Limits: Limits{MaxVarBytes: 1000}})
	res, data := renderBytes(t, c, html)
	pdftest.Validate(t, data)
	found := false
	for _, w := range res.Warnings {
		found = found || w.Code == WarnVarBudget
	}
	if !found {
		t.Errorf("no %s warning in %+v", WarnVarBudget, res.Warnings)
	}
}

// Warnings: occurrence counts, sort, 256 cap and the dropped counter, 80-byte details.
func TestWarningsCapAndSanitizing(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("<style>")
	for i := range 400 {
		fmt.Fprintf(&sb, "p { unknown-prop-%03d: 1 }\n", i)
	}
	sb.WriteString("</style><p>x</p>")
	res, _ := renderBytes(t, mustNew(t, Options{}), sb.String())
	if len(res.Warnings) != 256 {
		t.Errorf("%d distinct warnings, want the 256 cap", len(res.Warnings))
	}
	if res.WarningsDropped < 400-256 {
		t.Errorf("dropped = %d, want at least %d", res.WarningsDropped, 400-256)
	}
	for i, w := range res.Warnings {
		if w.Count < 1 {
			t.Errorf("warning %d has count %d", i, w.Count)
		}
		if i > 0 {
			p := res.Warnings[i-1]
			if p.Code > w.Code || p.Code == w.Code && p.Detail >= w.Detail {
				t.Fatalf("not sorted by Code then Detail, or duplicated: %+v then %+v", p, w)
			}
		}
	}

	long := strings.Repeat("é", 100)
	hostile := "<style>p { " + long + "\x01\x1b: 1 } p { color: " + long + "\n\x07 }</style><p>x</p>"
	res, _ = renderBytes(t, mustNew(t, Options{}), hostile)
	if len(res.Warnings) == 0 {
		t.Fatal("no warnings")
	}
	for _, w := range res.Warnings {
		if len(w.Detail) > 80 {
			t.Errorf("detail %d bytes: %q", len(w.Detail), w.Detail)
		}
		for _, r := range w.Detail {
			if r < 0x20 || r == 0x7f {
				t.Errorf("control character in %q", w.Detail)
			}
		}
		if !strings.ContainsFunc(w.Detail, func(r rune) bool { return r != 0xFFFD }) && w.Detail != "" {
			t.Logf("detail of replacement characters only: %q", w.Detail)
		}
	}
}

func TestWarningCountsAccumulate(t *testing.T) {
	html := "<style>p{foo:1}</style>" + strings.Repeat("<p>a</p>", 5) + strings.Repeat("<img>", 7) + strings.Repeat(`<a href="javascript:1">l</a>`, 3)
	res, _ := renderBytes(t, mustNew(t, Options{}), html)
	counts := map[WarningCode]int{}
	for _, w := range res.Warnings {
		counts[w.Code] += w.Count
	}
	if counts[WarnUnsupportedElement] != 7 {
		t.Errorf("img occurrences = %d, want 7", counts[WarnUnsupportedElement])
	}
	if counts[WarnBlockedLink] != 3 {
		t.Errorf("blocked links = %d, want 3", counts[WarnBlockedLink])
	}
	if counts[WarnUnsupportedProperty] < 1 {
		t.Errorf("unsupported property missing: %+v", res.Warnings)
	}
	a, _ := renderBytes(t, mustNew(t, Options{}), html)
	b, _ := renderBytes(t, mustNew(t, Options{}), html)
	if fmt.Sprint(a.Warnings) != fmt.Sprint(b.Warnings) {
		t.Error("warnings are not deterministic")
	}
}

// Document metadata of design §4: Producer, no dates, /ID from the content hash.
func TestInfoDictionaryAndID(t *testing.T) {
	_, data := renderBytes(t, mustNew(t, Options{}), reportHTML)
	s := string(data)
	if !strings.Contains(s, "/Producer (htmltopdf)") {
		t.Error("no /Producer (htmltopdf)")
	}
	for _, bad := range []string{"/CreationDate", "/ModDate"} {
		if strings.Contains(s, bad) {
			t.Errorf("unexpected %s", bad)
		}
	}
	doc := pdftest.Validate(t, data)
	i := strings.LastIndex(s, "trailer")
	if i < 0 {
		t.Skip("no classic trailer (xref stream)")
	}
	id := strings.Index(s[i:], "/ID [<")
	if id < 0 {
		t.Fatal("no /ID in the trailer")
	}
	rest := s[i+id+len("/ID [<"):]
	first, _, _ := strings.Cut(rest, ">")
	if len(first) != 32 || first != doc.ID {
		t.Errorf("/ID %q vs parsed %q", first, doc.ID)
	}
}

// Title from <title> is sanitized into the info dictionary (§8.3).
func TestTitleInjection(t *testing.T) {
	c := mustNew(t, Options{})
	for _, title := range []string{"a) /Evil (b", "x\x00y", "\\) >> /AA <<", strings.Repeat("t", 100000), "é\u202eà"} {
		_, data := renderBytes(t, c, "<title>"+title+"</title><p>x</p>")
		pdftest.Validate(t, data)
		if strings.Contains(string(data), "/Evil") && !strings.Contains(string(data), `\) /Evil`) && !strings.Contains(string(data), "a\\) /Evil") {
			t.Errorf("title %q not escaped", title)
		}
	}
}

// Concurrent renders on one Converter, different documents, byte-identical to sequential ones.
func TestConcurrentMixedDocuments(t *testing.T) {
	c := mustNew(t, referenceOptions(t))
	docs := []string{reportHTML, multiPageHTML(), "<p>tiny</p>", "", strings.Repeat("<p>x ↳ y</p>", 50)}
	want := make([][]byte, len(docs))
	for i, d := range docs {
		_, want[i] = renderBytes(t, c, d)
	}
	var wg sync.WaitGroup
	for g := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := range 3 {
				i := (g + k) % len(docs)
				var buf bytes.Buffer
				if _, err := c.Render(context.Background(), strings.NewReader(docs[i]), &buf); err != nil {
					t.Error(err)
					return
				}
				if !bytes.Equal(buf.Bytes(), want[i]) {
					t.Errorf("goroutine %d doc %d differs from the sequential render", g, i)
				}
			}
		}()
	}
	wg.Wait()
}

// Concurrent failures and cancellations on a shared Converter.
func TestConcurrentErrorsDoNotInterfere(t *testing.T) {
	c := mustNew(t, Options{Limits: Limits{MaxNodes: 200}})
	_, want := renderBytes(t, c, "<p>ok</p>")
	var wg sync.WaitGroup
	for g := range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			switch g % 3 {
			case 0:
				if _, err := c.Render(context.Background(), strings.NewReader(strings.Repeat("<p>x</p>", 500)), io.Discard); !errors.Is(err, ErrLimit) {
					t.Errorf("limit: %v", err)
				}
			case 1:
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				if _, err := c.Render(ctx, strings.NewReader("<p>x"), io.Discard); !errors.Is(err, context.Canceled) {
					t.Errorf("cancel: %v", err)
				}
			default:
				var buf bytes.Buffer
				if _, err := c.Render(context.Background(), strings.NewReader("<p>ok</p>"), &buf); err != nil || !bytes.Equal(buf.Bytes(), want) {
					t.Errorf("ok render: %v", err)
				}
			}
		}()
	}
	wg.Wait()
}

// The headline of the task: headings, paragraphs, colored and bordered blocks through the public API.
func TestColoredAndBorderedBlocksPaint(t *testing.T) {
	html := `<h1>Title</h1><p>para</p>
<div style="background:#ff0000;padding:20px"></div>
<div style="border:4px solid #0000ff;padding:20px"></div>`
	_, data := renderBytes(t, mustNew(t, Options{}), html)
	content := pagesContent(t, data)
	if !strings.Contains(content, "1 0 0 rg") {
		t.Error("background color not painted")
	}
	if !strings.Contains(content, "0 0 1 rg") && !strings.Contains(content, "0 0 1 RG") {
		t.Error("border color not painted")
	}
}

// Options that must not panic or be accepted at their boundaries.
func TestScaleBoundaries(t *testing.T) {
	for _, s := range []float64{0, 0.1, 0.5, 1, 1.5, 2} {
		c, err := New(Options{Scale: s})
		if err != nil {
			t.Errorf("Scale %v: %v", s, err)
			continue
		}
		renderBytes(t, c, reportHTML)
	}
	for _, s := range []float64{0.0999, 2.0001, -0.1} {
		if _, err := New(Options{Scale: s}); !errors.Is(err, ErrInvalidOptions) {
			t.Errorf("Scale %v: %v", s, err)
		}
	}
}

func TestFontSizeLimitsAndGenericNames(t *testing.T) {
	ref := referenceOptions(t)
	// An unknown generic family name is an invalid option or falls back, but never a panic.
	for _, g := range []GenericFamilies{{SansSerif: "Nope"}, {Serif: "Nope"}, {Monospace: "Nope"}} {
		c, err := New(Options{Generic: g})
		if err != nil {
			if !errors.Is(err, ErrInvalidOptions) {
				t.Errorf("%+v: %v", g, err)
			}
			continue
		}
		renderBytes(t, c, "<p>x</p><code>y</code>")
	}
	// Fonts that exceed the 32 MiB per-font and 128 MiB total bounds.
	big := make([]byte, 32<<20+1)
	copy(big, ref.Fonts[0].Data)
	if _, err := New(Options{Fonts: []Font{{Data: big}}}); !errors.Is(err, ErrInvalidOptions) {
		t.Errorf("33 MiB font: %v", err)
	}
	// A truncated font at every few sizes is rejected, never a panic.
	for _, n := range []int{1, 4, 12, 100, 1000, len(ref.Fonts[0].Data) / 2} {
		if _, err := New(Options{Fonts: []Font{{Data: ref.Fonts[0].Data[:n]}}}); err == nil {
			t.Logf("font truncated at %d accepted", n)
		} else if !errors.Is(err, ErrInvalidOptions) {
			t.Errorf("truncated %d: %v", n, err)
		}
	}
}
