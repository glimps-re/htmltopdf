package dom

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

// noLenReader hides the Len method of the wrapped reader.
type noLenReader struct{ r io.Reader }

func (n noLenReader) Read(p []byte) (int, error) { return n.r.Read(p) }

// lyingLenReader reports len as its Len but delivers all of r.
type lyingLenReader struct {
	r   io.Reader
	len int
}

func (l *lyingLenReader) Read(p []byte) (int, error) { return l.r.Read(p) }
func (l *lyingLenReader) Len() int                   { return l.len }

// readerKinds builds the reader variants readBounded must treat alike.
var readerKinds = map[string]func(data []byte) io.Reader{
	"len":       func(d []byte) io.Reader { return bytes.NewReader(d) },
	"no-len":    func(d []byte) io.Reader { return noLenReader{bytes.NewReader(d)} },
	"len-under": func(d []byte) io.Reader { return &lyingLenReader{r: bytes.NewReader(d), len: len(d) / 3} },
	"len-zero":  func(d []byte) io.Reader { return &lyingLenReader{r: bytes.NewReader(d)} },
	"len-neg":   func(d []byte) io.Reader { return &lyingLenReader{r: bytes.NewReader(d), len: -1} },
	"len-over":  func(d []byte) io.Reader { return &lyingLenReader{r: bytes.NewReader(d), len: 4 * len(d)} },
	"one-byte":  func(d []byte) io.Reader { return iotest.OneByteReader(bytes.NewReader(d)) },
	"data-err":  func(d []byte) io.Reader { return iotest.DataErrReader(bytes.NewReader(d)) },
}

func inputBudget(limit int64) *budget.Budget {
	l := budget.Defaults()
	l[budget.InputBytes] = limit
	return newBudget(l)
}

func TestReadBoundedLimit(t *testing.T) {
	sizes := []struct {
		name  string
		limit int64
	}{
		{"zero", 0},
		{"one", 1},
		{"small", 100},
		{"chunk-1", 64<<10 - 1},
		{"chunk", 64 << 10},
		{"chunk+1", 64<<10 + 1},
		{"large", 300 << 10},
	}
	for kind, mk := range readerKinds {
		for _, s := range sizes {
			t.Run(kind+"/"+s.name, func(t *testing.T) {
				data := bytes.Repeat([]byte{'x'}, int(s.limit)+1)
				got, err := readBounded(mk(data[:s.limit]), inputBudget(s.limit))
				if err != nil {
					t.Fatalf("at limit: %v", err)
				}
				if !bytes.Equal(got, data[:s.limit]) {
					t.Fatalf("at limit: got %d bytes, want %d", len(got), s.limit)
				}
				_, err = readBounded(mk(data), inputBudget(s.limit))
				var le *budget.LimitError
				if !errors.As(err, &le) || le.Kind != budget.InputBytes || le.Max != s.limit {
					t.Fatalf("limit+1: got %v", err)
				}
			})
		}
	}
}

func TestReadBoundedExtremes(t *testing.T) {
	cases := []struct {
		name    string
		src     io.Reader
		limit   int64
		want    string
		wantErr bool
	}{
		{"len-maxint", &lyingLenReader{r: strings.NewReader("<p>x"), len: math.MaxInt}, 100, "<p>x", false},
		{"len-maxint-over", &lyingLenReader{r: bytes.NewReader(make([]byte, 101)), len: math.MaxInt}, 100, "", true},
		{"negative-limit", strings.NewReader("x"), -1, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := readBounded(c.src, inputBudget(c.limit))
			var le *budget.LimitError
			if c.wantErr && !errors.As(err, &le) {
				t.Fatalf("got %v, want a LimitError", err)
			}
			if !c.wantErr && (err != nil || string(got) != c.want) {
				t.Fatalf("got %q, %v; want %q", got, err, c.want)
			}
		})
	}
}

func TestReadBoundedReadError(t *testing.T) {
	want := errors.New("boom")
	_, err := readBounded(iotest.ErrReader(want), inputBudget(1<<20))
	if !errors.Is(err, want) || !strings.HasPrefix(err.Error(), "dom: reading input: ") {
		t.Fatalf("got %v", err)
	}
}

// shortReader claims to have filled more than the buffer it was given.
type shortReader struct{}

func (shortReader) Read(p []byte) (int, error) { return len(p) + 1, nil }

func TestReadBoundedInvalidRead(t *testing.T) {
	for name, r := range map[string]io.Reader{
		"too-many": shortReader{},
		"negative": negativeReader{},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := readBounded(r, inputBudget(1<<20)); !errors.Is(err, errInvalidRead) {
				t.Fatalf("got %v, want errInvalidRead", err)
			}
		})
	}
}

type negativeReader struct{}

func (negativeReader) Read([]byte) (int, error) { return -1, nil }

func BenchmarkReadBounded(b *testing.B) {
	data := bytes.Repeat([]byte{'x'}, 70<<10)
	for _, kind := range []string{"len", "no-len"} {
		for _, limit := range []int64{2 << 20, 512 << 20} {
			b.Run(fmt.Sprintf("%s/%dMiB", kind, limit>>20), func(b *testing.B) {
				b.SetBytes(int64(len(data)))
				b.ReportAllocs()
				for b.Loop() {
					if _, err := readBounded(readerKinds[kind](data), inputBudget(limit)); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
