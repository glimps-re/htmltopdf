package htmltopdf

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"html/template"
	"io"
)

// templateBufSize batches the many small writes of Execute before they cross
// the pipe.
const templateBufSize = 32 << 10

// errRenderStopped closes the read side once Render has returned, so that the
// next flush of the template output fails and Execute returns.
var errRenderStopped = errors.New("htmltopdf: render stopped reading the template output")

// errTemplateExited is what the template goroutine reports when it ends without
// Execute returning: runtime.Goexit in a template function runs the defers with
// no panic to recover, and the truncated output must not be taken as complete.
var errTemplateExited = fmt.Errorf("%w: template goroutine exited", ErrInternal)

// RenderTemplate executes t with data and renders its output as Render would:
// the PDF and the Result are those of t.Execute to a buffer followed by Render.
// The output of t is streamed into Render without an intermediate copy in the
// caller; Render holds at most Limits.MaxInputBytes of it, as for any src, and
// every Limits field applies to it as to Render's src.
//
// t and its functions are trusted operator code; data may be hostile and is
// contextually escaped by html/template. Values typed as template.HTML, CSS, JS,
// JSStr, URL, Srcset or HTMLAttr bypass escaping and are the caller's
// responsibility. As with Execute, t can no longer be parsed or cloned afterwards.
//
// A nil ctx means context.Background(). A nil t or dst returns an error before
// anything runs; data may be nil. The Result is never nil. Errors: a failure of
// t (escape error, exec error, or a panic, which also matches ErrInternal) that
// stopped the render is returned wrapped as "executing template", reachable with
// errors.As; otherwise Render's own errors are returned unchanged, and a
// template failure caused by the render stopping is discarded.
//
// t runs in one goroutine that has exited when RenderTemplate returns. ctx and
// Limits.Timeout are observed only when the output of t reaches Render (every
// 32 KiB, and at the end): a template that blocks or loops without producing
// output is not interrupted, like a blocked src.Read. RenderTemplate is safe for
// concurrent use with the same t.
func (c *Converter) RenderTemplate(ctx context.Context, t *template.Template, data any, dst io.Writer) (*Result, error) {
	if t == nil {
		return &Result{}, errors.New("htmltopdf: nil template")
	}
	if dst == nil {
		return &Result{}, errors.New("htmltopdf: nil writer")
	}
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go executeTemplate(t, data, pw, done)

	res, rerr := c.Render(ctx, pr, dst)
	// CloseWithError on a pipe always returns nil.
	_ = pr.CloseWithError(errRenderStopped)
	terr := <-done
	// The pipe hands terr itself to the reader: Render failed because of the
	// template only if its read error is terr.
	if rerr != nil && terr != nil && errors.Is(rerr, terr) {
		return res, fmt.Errorf("htmltopdf: executing template: %w", terr)
	}
	return res, rerr
}

// executeTemplate sends exactly one error on done, also when a panic or
// runtime.Goexit ends it early.
func executeTemplate(t *template.Template, data any, pw *io.PipeWriter, done chan<- error) {
	err := errTemplateExited
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: template panic (%T): %s", ErrInternal, r, panicDetail(r))
		}
		// A nil err closes with io.EOF; CloseWithError on a pipe always returns nil.
		_ = pw.CloseWithError(err)
		done <- err
	}()
	bw := bufio.NewWriterSize(pw, templateBufSize)
	err = t.Execute(bw, data)
	if err == nil {
		err = bw.Flush()
	}
}
