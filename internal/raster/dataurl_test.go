package raster

import (
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestParseDataURL(t *testing.T) {
	for _, c := range []struct {
		src, payload string
		want         Reason
	}{
		{"data:image/png;base64,AAAA", "AAAA", 0},
		{"  DATA:Image/PNG;BASE64,AAAA \n", "AAAA", 0},
		{"data:image/jpeg;base64,", "", 0},
		{"data:image/jpg;base64,AA", "AA", 0},
		{"data: image/png ;charset=x;name=a.png; base64 ,A A", "A A", 0},
		{"data:image/png;base64,AA,AA", "AA,AA", 0},
		{"http://example.com/a.png", "", ReasonScheme},
		{"https://example.com/a.png", "", ReasonScheme},
		{"file:///etc/passwd", "", ReasonScheme},
		{"//host/a.png", "", ReasonScheme},
		{"a.png", "", ReasonScheme},
		{"", "", ReasonScheme},
		{"data", "", ReasonScheme},
		{" data:image/png;base64,AAAA", "", ReasonScheme},
		{"data:image/gif;base64,AAAA", "", ReasonType},
		{"data:image/svg+xml;base64,AAAA", "", ReasonType},
		{"data:;base64,AAAA", "", ReasonType},
		{"data:,AAAA", "", ReasonType},
		{"data:image/pngx;base64,AAAA", "", ReasonType},
		{"data:image/png,AAAA", "", ReasonEncoding},
		{"data:image/png;base64", "", ReasonEncoding},
		{"data:image/png;base64x,AAAA", "", ReasonEncoding},
		{"data:image/png;xbase64,AAAA", "", ReasonEncoding},
		{"data:image/png;charset=base64,AAAA", "", ReasonEncoding},
	} {
		payload, got := parseDataURL(c.src)
		if got != c.want || payload != c.payload {
			t.Errorf("%q: got %q, %v; want %q, %v", c.src, payload, got, c.payload, c.want)
		}
	}
}

func TestCheckBase64(t *testing.T) {
	for _, c := range []struct {
		p   string
		pad int
		ok  bool
	}{
		{"", 0, true},
		{"AAAA", 0, true},
		{"AAA", 1, true},
		{"AA", 2, true},
		{"AAA=", 0, true},
		{"AA==", 0, true},
		{" A A\tA\n=\r\f", 0, true},
		{"A", 0, false},
		{"AAAAA", 0, false},
		{"AA=", 0, false},
		{"A===", 0, false},
		{"=", 0, false},
		{"AA=A", 0, false},
		{"AA==AAAA", 0, false},
		{"AA%3D", 0, false},
		{"AA-_", 0, false},
		{"AA\x00A", 0, false},
		{"AA\xc3\xa9A", 0, false},
	} {
		pad, ok := checkBase64(c.p)
		if pad != c.pad || ok != c.ok {
			t.Errorf("%q: got %d, %v; want %d, %v", c.p, pad, ok, c.pad, c.ok)
		}
	}
}

func TestPayloadReaderDecodes(t *testing.T) {
	want := []byte("any carnal pleasure.")
	enc := base64.RawStdEncoding.EncodeToString(want)
	spaced := strings.Join(strings.Split(enc, ""), " \n\t")
	for _, p := range []string{enc, spaced, " " + base64.StdEncoding.EncodeToString(want) + " "} {
		pad, ok := checkBase64(p)
		if !ok {
			t.Fatalf("%q rejected", p)
		}
		s := &Source{payload: p, pad: pad}
		var got strings.Builder
		if _, err := io.Copy(&got, s.open()); err != nil {
			t.Fatal(err)
		}
		if got.String() != string(want) {
			t.Errorf("%q: got %q", p, got.String())
		}
	}
}

func TestPayloadReaderSmallReads(t *testing.T) {
	r := &payloadReader{s: " A \n B  C", pad: 1}
	var got []byte
	var b [1]byte
	for {
		n, err := r.Read(b[:])
		got = append(got, b[:n]...)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if string(got) != "ABC=" {
		t.Errorf("got %q", got)
	}
}
