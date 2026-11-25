package htmltopdf

import (
	"bytes"
	"context"
	"errors"
	"html"
	"html/template"
	"io"
	"iter"
	"os"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	texttemplate "text/template"
	"time"

	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

func corpusTemplate(tb testing.TB, name string, size testcorpus.Size) (*template.Template, any) {
	tb.Helper()
	t, data, err := testcorpus.ReportTemplate(os.DirFS("../../testdata/templates"), name, size)
	if err != nil {
		tb.Fatal(err)
	}
	return t, data
}

type corpusCase struct {
	name string
	size testcorpus.Size
}

var corpusCases = []corpusCase{
	{"00-host-connector", testcorpus.Small},
	{"01-modern", testcorpus.Small},
	{"02-executive", testcorpus.Small},
	{"03-analyst", testcorpus.Small},
	{"00-host-connector", testcorpus.Large},
}

// executeThenRender is the reference RenderTemplate must be equivalent to.
func executeThenRender(tb testing.TB, c *Converter, t *template.Template, data any) (*Result, []byte) {
	tb.Helper()
	var html, pdf bytes.Buffer
	if err := t.Execute(&html, data); err != nil {
		tb.Fatal(err)
	}
	res, err := c.Render(context.Background(), &html, &pdf)
	if err != nil {
		tb.Fatal(err)
	}
	return res, pdf.Bytes()
}

func renderTemplateBytes(tb testing.TB, c *Converter, t *template.Template, data any) (*Result, []byte) {
	tb.Helper()
	var pdf bytes.Buffer
	res, err := c.RenderTemplate(context.Background(), t, data, &pdf)
	if err != nil {
		tb.Fatal(err)
	}
	return res, pdf.Bytes()
}

func TestRenderTemplateByteIdentity(t *testing.T) {
	c := mustNew(t, referenceOptions(t))
	for _, cc := range corpusCases {
		t.Run(testcorpus.FixtureName(cc.name, cc.size), func(t *testing.T) {
			tmpl, data := corpusTemplate(t, cc.name, cc.size)
			wantRes, want := executeThenRender(t, c, tmpl, data)
			gotRes, got := renderTemplateBytes(t, c, tmpl, data)
			if !bytes.Equal(got, want) {
				t.Errorf("PDF differs: %d bytes, want %d", len(got), len(want))
			}
			if !reflect.DeepEqual(gotRes, wantRes) {
				t.Errorf("Result = %+v, want %+v", gotRes, wantRes)
			}
			if cc.size == testcorpus.Large && gotRes.Pages < 2 {
				t.Errorf("large report has %d pages, want a multi-page document", gotRes.Pages)
			}
		})
	}
}

// waitGoroutines fails if the goroutine count does not come back to base
// within a second: RenderTemplate must not leave its goroutine behind.
func waitGoroutines(tb testing.TB, base int) {
	tb.Helper()
	deadline := time.Now().Add(time.Second)
	for runtime.NumGoroutine() > base {
		if time.Now().After(deadline) {
			tb.Errorf("goroutines: %d, baseline %d", runtime.NumGoroutine(), base)
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// endless returns a channel that yields 0, 1, 2… until stop is called; stop
// waits for the producer to exit.
func endless() (<-chan int, func()) {
	ch := make(chan int)
	quit := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		defer close(ch)
		for i := 0; ; i++ {
			select {
			case ch <- i:
			case <-quit:
				return
			}
		}
	})
	return ch, func() { close(quit); wg.Wait() }
}

var errTestFunc = errors.New("template func failed")

func TestRenderTemplateErrors(t *testing.T) {
	errDst := errors.New("dst failed")
	panicSeq := iter.Seq[int](func(func(int) bool) { panic("iterator\x00panic") })

	type check func(t *testing.T, err error)
	isExec := func(t *testing.T, err error) {
		t.Helper()
		var ee texttemplate.ExecError
		if !errors.As(err, &ee) || errors.Is(err, ErrInternal) || !strings.Contains(err.Error(), "executing template") {
			t.Errorf("err = %v, want a wrapped template.ExecError", err)
		}
	}
	isLimit := func(l Limit) check {
		return func(t *testing.T, err error) {
			t.Helper()
			var le *LimitError
			if !errors.As(err, &le) || le.Limit != l || strings.Contains(err.Error(), "executing template") {
				t.Errorf("err = %v, want *LimitError %s", err, l)
			}
		}
	}
	is := func(target error) check {
		return func(t *testing.T, err error) {
			t.Helper()
			if !errors.Is(err, target) || strings.Contains(err.Error(), "executing template") {
				t.Errorf("err = %v, want %v", err, target)
			}
		}
	}

	tests := []struct {
		name   string
		limits Limits
		src    string // parsed unless empty
		funcs  func(cancel context.CancelFunc) template.FuncMap
		data   any
		dst    io.Writer
		check  check
	}{
		{
			name: "unparsed template",
			check: func(t *testing.T, err error) {
				t.Helper()
				if err == nil || !strings.HasPrefix(err.Error(), "htmltopdf: executing template: ") || errors.Is(err, ErrInternal) {
					t.Errorf("err = %v", err)
				}
			},
		},
		{
			name: "escape error",
			src:  `<a href="{{if .}}/path/{{else}}/search?q={{end}}{{.}}">x</a>`,
			check: func(t *testing.T, err error) {
				t.Helper()
				var te *template.Error
				if !errors.As(err, &te) || te.ErrorCode != template.ErrAmbigContext || !strings.Contains(err.Error(), "executing template") {
					t.Errorf("err = %v, want *template.Error ErrAmbigContext", err)
				}
			},
		},
		{
			name: "func error mid-output",
			src:  `{{range .}}<p>row {{.}}</p>{{if eq . 5000}}{{fail}}{{end}}{{end}}`,
			funcs: func(context.CancelFunc) template.FuncMap {
				return template.FuncMap{"fail": func() (string, error) { return "", errTestFunc }}
			},
			data: seq(10000),
			check: func(t *testing.T, err error) {
				t.Helper()
				isExec(t, err)
				if !errors.Is(err, errTestFunc) {
					t.Errorf("err = %v, want it to wrap the func error", err)
				}
			},
		},
		{
			name: "func panic",
			src:  `<p>a</p>{{boom}}`,
			funcs: func(context.CancelFunc) template.FuncMap {
				return template.FuncMap{"boom": func() string { panic("boom") }}
			},
			check: isExec,
		},
		{
			name: "panic outside html/template recovery",
			src:  `<p>a</p>{{range .}}<p>{{.}}</p>{{end}}`,
			data: panicSeq,
			check: func(t *testing.T, err error) {
				t.Helper()
				if !errors.Is(err, ErrInternal) || !strings.Contains(err.Error(), "executing template") ||
					!strings.Contains(err.Error(), "iterator?panic") {
					t.Errorf("err = %v, want a sanitized ErrInternal template panic", err)
				}
			},
		},
		{
			name:   "endless output above MaxInputBytes",
			limits: Limits{MaxInputBytes: 256 << 10},
			src:    `{{range .}}<p>row {{.}}</p>{{end}}`,
			check:  isLimit(LimitInputBytes),
		},
		{
			name: "runtime.Goexit in a template func",
			src:  `<p>a</p>{{exit}}<p>b</p>`,
			funcs: func(context.CancelFunc) template.FuncMap {
				return template.FuncMap{"exit": func() string { runtime.Goexit(); return "" }}
			},
			check: func(t *testing.T, err error) {
				t.Helper()
				if !errors.Is(err, ErrInternal) || !strings.Contains(err.Error(), "executing template") {
					t.Errorf("err = %v, want a wrapped ErrInternal", err)
				}
			},
		},
		{
			name:   "MaxPages",
			limits: Limits{MaxPages: 2},
			src:    `{{range .}}<div style="break-after: page">{{.}}</div>{{end}}`,
			data:   seq(10),
			check:  isLimit(LimitPages),
		},
		{
			name:   "MaxWork",
			limits: Limits{MaxWork: 1000},
			src:    `{{range .}}<p>row {{.}}</p>{{end}}`,
			data:   seq(2000),
			check:  isLimit(LimitWork),
		},
		{
			name: "context cancelled while writing",
			src:  `<p>start</p>{{range .}}{{if eq . 1000}}{{cancel}}{{end}}<p>row {{.}}</p>{{end}}`,
			funcs: func(cancel context.CancelFunc) template.FuncMap {
				return template.FuncMap{"cancel": func() string { cancel(); return "" }}
			},
			check: is(context.Canceled),
		},
		{
			name:   "timeout",
			limits: Limits{Timeout: 50 * time.Millisecond},
			src:    `<p>start</p>{{range .}}{{if eq . 0}}{{slow}}{{end}}<p>row {{.}}</p>{{end}}`,
			funcs: func(context.CancelFunc) template.FuncMap {
				return template.FuncMap{"slow": func() string { time.Sleep(100 * time.Millisecond); return "" }}
			},
			check: is(context.DeadlineExceeded),
		},
		{
			name:  "failing dst",
			src:   `<p>{{.}}</p>`,
			data:  "x",
			dst:   &failingWriter{err: errDst},
			check: is(errDst),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := mustNew(t, Options{Limits: tt.limits})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			base := runtime.NumGoroutine()
			tmpl := template.New("t")
			if tt.funcs != nil {
				tmpl.Funcs(tt.funcs(cancel))
			}
			if tt.src != "" {
				tmpl = template.Must(tmpl.Parse(tt.src))
			}
			data, stop := tt.data, func() {}
			if data == nil {
				data, stop = endless()
			}
			dst := tt.dst
			if dst == nil {
				dst = &bytes.Buffer{}
			}
			res, err := c.RenderTemplate(ctx, tmpl, data, dst)
			stop()
			if res == nil {
				t.Fatal("nil Result")
			}
			tt.check(t, err)
			waitGoroutines(t, base)
		})
	}
}

func seq(n int) []int {
	s := make([]int, n)
	for i := range s {
		s[i] = i
	}
	return s
}

func TestRenderTemplateArguments(t *testing.T) {
	c := mustNew(t, Options{})
	var calls atomic.Int32
	tmpl := template.Must(template.New("t").Funcs(template.FuncMap{
		"rec": func() string { calls.Add(1); return "" },
	}).Parse(`{{rec}}<p>{{.}}</p>`))

	tests := []struct {
		name    string
		ctx     context.Context
		t       *template.Template
		data    any
		dst     io.Writer
		wantErr string
		calls   int32
	}{
		{name: "nil template", ctx: context.Background(), dst: &bytes.Buffer{}, wantErr: "htmltopdf: nil template"},
		{name: "nil writer", ctx: context.Background(), t: tmpl, wantErr: "htmltopdf: nil writer"},
		{name: "nil data", ctx: context.Background(), t: tmpl, dst: &bytes.Buffer{}, calls: 1},
		{name: "nil ctx", t: tmpl, data: "x", dst: &bytes.Buffer{}, calls: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls.Store(0)
			base := runtime.NumGoroutine()
			res, err := c.RenderTemplate(tt.ctx, tt.t, tt.data, tt.dst)
			if res == nil {
				t.Fatal("nil Result")
			}
			switch {
			case tt.wantErr != "":
				if err == nil || err.Error() != tt.wantErr || !reflect.DeepEqual(res, &Result{}) {
					t.Errorf("RenderTemplate = %+v, %v, want empty Result and %q", res, err, tt.wantErr)
				}
			case err != nil || res.Pages != 1:
				t.Errorf("RenderTemplate = %+v, %v, want 1 page", res, err)
			}
			if got := calls.Load(); got != tt.calls {
				t.Errorf("template executed %d times, want %d", got, tt.calls)
			}
			waitGoroutines(t, base)
		})
	}
}

func TestRenderTemplateHostileData(t *testing.T) {
	c := mustNew(t, Options{})
	textTmpl := template.Must(template.New("t").Parse(`<table><tr><td>{{.}}</td><td>end</td></tr></table>`))
	tests := []struct {
		name string
		data string
	}{
		{"close cell and hidden block", `</td><div style="position:absolute;left:-9999px">X</div>`},
		{"script", `<script>document.write("pwned")</script>`},
		{"style", `<style>td { display: none }</style>`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The escaped data must render exactly as the same text written by
			// hand into the cell: one cell, no extra element.
			ref := `<table><tr><td>` + html.EscapeString(tt.data) + `</td><td>end</td></tr></table>`
			_, want := renderBytes(t, c, ref)
			_, got := renderTemplateBytes(t, c, textTmpl, tt.data)
			if !bytes.Equal(got, want) {
				t.Error("output differs from the hand-escaped document")
			}
			text := strings.Join(strings.Fields(pagesText(pdftest.Validate(t, got))), "")
			if !strings.Contains(text, strings.Join(strings.Fields(tt.data), "")) {
				t.Errorf("text %q does not hold the literal data", text)
			}
		})
	}

	t.Run("attribute", func(t *testing.T) {
		tmpl := template.Must(template.New("t").Parse(`<table><tr><td title="{{.}}">cell</td></tr></table>`))
		data := `" onmouseover="x" style="display:none`
		_, want := renderBytes(t, c, `<table><tr><td title="`+html.EscapeString(data)+`">cell</td></tr></table>`)
		_, got := renderTemplateBytes(t, c, tmpl, data)
		if !bytes.Equal(got, want) {
			t.Error("output differs from the hand-escaped document")
		}
		if text := pagesText(pdftest.Validate(t, got)); !strings.Contains(text, "cell") {
			t.Errorf("text %q, want the cell kept visible", text)
		}
	})

	t.Run("javascript href", func(t *testing.T) {
		tmpl := template.Must(template.New("t").Parse(`<p><a href="{{.}}">link</a></p>`))
		_, got := renderTemplateBytes(t, c, tmpl, "javascript:alert(1)")
		_, want := renderBytes(t, c, `<p><a href="#ZgotmplZ">link</a></p>`)
		if !bytes.Equal(got, want) {
			t.Error("output differs from the #ZgotmplZ document")
		}
		for _, p := range pdftest.Validate(t, got).Pages {
			for _, l := range p.Links {
				if strings.Contains(strings.ToLower(l.URI), "javascript") {
					t.Errorf("link URI %q", l.URI)
				}
			}
		}
	})
}

func TestRenderTemplateConcurrent(t *testing.T) {
	c := mustNew(t, referenceOptions(t))
	ref, data := corpusTemplate(t, "00-host-connector", testcorpus.Small)
	_, want := executeThenRender(t, c, ref, data)
	// A template never executed yet: the workers race its first escaping.
	tmpl, _ := corpusTemplate(t, "00-host-connector", testcorpus.Small)
	const workers = 8
	outs := make([][]byte, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Go(func() {
			var buf bytes.Buffer
			_, errs[i] = c.RenderTemplate(context.Background(), tmpl, data, &buf)
			outs[i] = buf.Bytes()
		})
	}
	wg.Wait()
	for i := range workers {
		if errs[i] != nil || !bytes.Equal(outs[i], want) {
			t.Errorf("worker %d: err %v, identical %t", i, errs[i], bytes.Equal(outs[i], want))
		}
	}
}

// BenchmarkRenderTemplate and BenchmarkRenderTemplateBaseline compare the
// streamed helper with Execute to a buffer then Render, on the large reference
// report.
func BenchmarkRenderTemplate(b *testing.B) {
	benchmarkTemplate(b, func(c *Converter, t *template.Template, data any, dst io.Writer) error {
		_, err := c.RenderTemplate(context.Background(), t, data, dst)
		return err
	})
}

func BenchmarkRenderTemplateBaseline(b *testing.B) {
	benchmarkTemplate(b, func(c *Converter, t *template.Template, data any, dst io.Writer) error {
		var html bytes.Buffer
		if err := t.Execute(&html, data); err != nil {
			return err
		}
		_, err := c.Render(context.Background(), &html, dst)
		return err
	})
}

func benchmarkTemplate(b *testing.B, run func(*Converter, *template.Template, any, io.Writer) error) {
	c := mustNew(b, referenceOptions(b))
	tmpl, data := corpusTemplate(b, "00-host-connector", testcorpus.Large)
	b.ReportAllocs()
	for b.Loop() {
		var pdf bytes.Buffer
		if err := run(c, tmpl, data, &pdf); err != nil {
			b.Fatal(err)
		}
	}
}
