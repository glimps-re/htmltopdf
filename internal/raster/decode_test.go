package raster

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

// corpusFiles returns the PNG and JPEG files under testdata/images.
func corpusFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir("../../testdata/images", func(path string, _ fs.DirEntry, err error) error {
		switch filepath.Ext(path) {
		case ".png", ".jpeg", ".jpg":
			files = append(files, path)
		}
		return err
	})
	if err != nil || len(files) == 0 {
		t.Fatalf("image corpus: %d files, %v", len(files), err)
	}
	return files
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustDecode(t *testing.T, src string) *Decoded {
	t.Helper()
	s, err := NewSet(newBudget()).Add(src)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	d, err := Decode(s, newBudget())
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	return d
}

// rgba returns the NRGBA rows of d as WriteSamples and WriteAlpha see them.
func rgba(t *testing.T, d *Decoded) []byte {
	t.Helper()
	var samples, alpha bytes.Buffer
	if err := d.WriteSamples(&samples); err != nil {
		t.Fatal(err)
	}
	if err := d.WriteAlpha(&alpha); err != nil {
		t.Fatal(err)
	}
	n := 3
	if d.Gray {
		n = 1
	}
	if samples.Len() != d.Width*d.Height*n || alpha.Len() != d.Width*d.Height {
		t.Fatalf("%d samples and %d alpha bytes for %d×%d×%d", samples.Len(), alpha.Len(), d.Width, d.Height, n)
	}
	out := make([]byte, 0, 4*d.Width*d.Height)
	s, a := samples.Bytes(), alpha.Bytes()
	for i := range a {
		if d.Gray {
			out = append(out, s[i], s[i], s[i], a[i])
		} else {
			out = append(out, s[3*i], s[3*i+1], s[3*i+2], a[i])
		}
	}
	return out
}

// genericRows converts d through the image.Image interface only.
func genericRows(d *Decoded) []byte {
	buf := make([]byte, 4*d.Width)
	var out []byte
	for y := range d.Height {
		genericRow(d.img, y, buf)
		out = append(out, buf...)
	}
	return out
}

func maxDelta(a, b []byte) int {
	m := 0
	for i := range a {
		m = max(m, abs(int(a[i])-int(b[i])))
	}
	return m
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func TestDecodeCorpus(t *testing.T) {
	for _, path := range corpusFiles(t) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data := readFile(t, path)
			d := mustDecode(t, pngURL(data))
			got := rgba(t, d)
			checkRows(t, d, got)
			if d.Alpha != hasTransparent(got) {
				t.Errorf("Alpha %v disagrees with the samples", d.Alpha)
			}
			ext := filepath.Ext(path)
			if ext == ".png" {
				return
			}
			// A progressive JPEG decodes like its baseline twin.
			twin := strings.TrimSuffix(path, ext) + ".png"
			if strings.HasSuffix(path, ".progressive.jpeg") && !strings.Contains(path, ".q50.") {
				twin = strings.TrimSuffix(path, ".progressive.jpeg") + ".png"
			}
			if _, err := os.Stat(twin); err != nil {
				return
			}
			want := rgba(t, mustDecode(t, pngURL(readFile(t, twin))))
			if len(want) != len(got) {
				t.Fatalf("twin %s has another size", twin)
			}
			// image/jpeg's own tests accept 8 levels against the libjpeg twins.
			t.Logf("compared with %s", filepath.Base(twin))
			if m := maxDelta(got, want); m > 8 {
				t.Errorf("differs from %s by %d", filepath.Base(twin), m)
			}
		})
	}
}

// checkRows compares the rows of d with an oracle: the high bytes of a
// non-premultiplied 16-bit image (no colour loss), else the image.Image conversion,
// which premultiplies 16-bit colours and so loses those of translucent pixels.
func checkRows(t *testing.T, d *Decoded, got []byte) {
	t.Helper()
	want := genericRows(d)
	if m, ok := d.img.(*image.NRGBA64); ok {
		want = want[:0]
		for y := range d.Height {
			for x := range 4 * d.Width {
				want = append(want, m.Pix[y*m.Stride+2*x])
			}
		}
	}
	if len(want) != len(got) || maxDelta(want, got) > 1 {
		t.Errorf("rows differ from the oracle by %d", maxDelta(want, got))
	}
}

func hasTransparent(nrgba []byte) bool {
	for i := 3; i < len(nrgba); i += 4 {
		if nrgba[i] != 0xff {
			return true
		}
	}
	return false
}

func TestDecodeRejectsCorruptData(t *testing.T) {
	full := encodePNG(t, gradient(64, 64, color.NRGBAModel, true))
	badCRC := bytes.Clone(full)
	badCRC[len(badCRC)-13]++ // last byte of the IDAT CRC
	for _, c := range []struct {
		name string
		src  string
	}{
		{"png truncated in IDAT", pngURL(full[:len(full)/2])},
		{"png bad IDAT crc", pngURL(badCRC)},
		{"png half the rows", pngURL(rawPNG(ihdr(16, 16, 8, 0, 0), idat(t, zeroRows(16, 8, 1, false)), iend))},
		{"png extra rows", pngURL(rawPNG(ihdr(16, 16, 8, 0, 0), idat(t, zeroRows(16, 17, 1, false)), iend))},
		{"png no IDAT", pngHeaderOnly(4, 4, 8, 6, 0)},
		{"jpeg truncated after SOS", jpegURL(synthJPEG(jpegSpec{w: 64, h: 64, nComp: 1, sof: mSOF0, truncateEnd: true}))},
		{"jpeg no scan", jpegURL(synthJPEG(jpegSpec{w: 64, h: 64, nComp: 1, sof: mSOF0, headerOnly: true, extra: segmentBytes(mAPP0, []byte("JFIF\x00"))}))},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, err := NewSet(newBudget()).Add(c.src)
			if err != nil {
				t.Fatalf("Add: %v", err)
			}
			d, err := Decode(s, newBudget())
			if reasonOf(err) != ReasonCorrupt || d != nil {
				t.Fatalf("got %v, %v; want corrupt", d, err)
			}
		})
	}
}

// zlibZeros returns n MiB of zeros compressed by zlib: about 1 KiB per MiB.
func zlibZeros(t *testing.T, n int) []byte {
	t.Helper()
	var z bytes.Buffer
	zw, err := zlib.NewWriterLevel(&z, zlib.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	zeros := make([]byte, 1<<20)
	for range n {
		if _, err := zw.Write(zeros); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return z.Bytes()
}

// image/png inflates only the rows of the header and fails on the first
// extra byte; it skips iCCP and zTXt chunks without inflating them.
func TestDecodeDeflateBombs(t *testing.T) {
	bomb := zlibZeros(t, 64)
	for _, c := range []struct {
		name string
		png  []byte
		want Reason
	}{
		{"IDAT", rawPNG(ihdr(1, 1, 8, 6, 0), pngChunk("IDAT", bomb), iend), ReasonCorrupt},
		{"iCCP", rawPNG(ihdr(1, 1, 8, 6, 0), pngChunk("iCCP", append([]byte("p\x00\x00"), bomb...)),
			idat(t, make([]byte, 5)), iend), 0},
		{"zTXt", rawPNG(ihdr(1, 1, 8, 6, 0), idat(t, make([]byte, 5)),
			pngChunk("zTXt", append([]byte("k\x00\x00"), bomb...)), iend), 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, err := NewSet(newBudget()).Add(pngURL(c.png))
			if err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			alloc := totalAlloc(func() { _, err = Decode(s, newBudget()) })
			elapsed := time.Since(start)
			if reasonOf(err) != c.want {
				t.Fatalf("got %v, want %v", err, c.want)
			}
			if alloc > 4<<20 {
				t.Errorf("allocated %d bytes", alloc)
			}
			if elapsed > 50*time.Millisecond && !raceEnabled {
				t.Errorf("took %v", elapsed)
			}
		})
	}
}

func TestDecodeRecoversPanics(t *testing.T) {
	saved := decoders[PNG]
	t.Cleanup(func() { decoders[PNG] = saved })
	s, err := NewSet(newBudget()).Add(pngURL(encodePNG(t, gradient(2, 2, color.GrayModel, false))))
	if err != nil {
		t.Fatal(err)
	}
	for _, fake := range []func(io.Reader) (image.Image, error){
		func(io.Reader) (image.Image, error) { panic("decoder bug") },
		func(io.Reader) (image.Image, error) { var m *image.Gray; return m.SubImage(m.Rect), nil },
		func(io.Reader) (image.Image, error) { return image.NewGray(image.Rect(0, 0, 2, 3)), nil },
		func(io.Reader) (image.Image, error) { return image.NewGray(image.Rect(1, 1, 3, 3)), nil },
		func(io.Reader) (image.Image, error) { return nil, nil },
	} {
		decoders[PNG] = fake
		if d, err := Decode(s, newBudget()); reasonOf(err) != ReasonCorrupt || d != nil {
			t.Errorf("got %v, %v; want corrupt", d, err)
		}
	}
}

func TestDecodeChargesWorkFirst(t *testing.T) {
	saved := decoders[JPEG]
	t.Cleanup(func() { decoders[JPEG] = saved })
	calls := 0
	decoders[JPEG] = func(r io.Reader) (image.Image, error) { calls++; return saved(r) }
	s, err := NewSet(newBudget()).Add(jpegURL(synthJPEG(jpegSpec{w: 40, h: 30, nComp: 3, sof: mSOF2, acBands: 4})))
	if err != nil {
		t.Fatal(err)
	}
	if s.scans != 13 || s.work() < 40*32*WorkPerPixel*4 {
		t.Fatalf("scans %d, work %d", s.scans, s.work())
	}
	l := budget.Defaults()
	l[budget.Work] = s.work() - 1
	_, err = Decode(s, budget.New(context.Background(), l, nil))
	var le *budget.LimitError
	if !errors.As(err, &le) || le.Kind != budget.Work || calls != 0 {
		t.Fatalf("got %v after %d decodes, want a Work LimitError before decoding", err, calls)
	}
	l[budget.Work] = s.work()
	b := budget.New(context.Background(), l, nil)
	if _, err := Decode(s, b); err != nil || calls != 1 || b.Used(budget.Work) != s.work() {
		t.Fatalf("at the limit: %v, %d decodes, %d Work", err, calls, b.Used(budget.Work))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Decode(s, budget.New(ctx, budget.Defaults(), nil)); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("cancelled: %v after %d decodes", err, calls)
	}
}

func TestWorkFactors(t *testing.T) {
	for _, c := range []struct {
		s    Source
		want int64
	}{
		{Source{Format: PNG, Width: 10, Height: 10}, 100*WorkPerPixel + 10*rowWork},
		{Source{Format: PNG, Width: 10, Height: 10, deep: true}, 200*WorkPerPixel + 10*rowWork},
		{Source{Format: PNG, Width: 10, Height: 10, payload: "abc"}, 100*WorkPerPixel + 10*rowWork + 3},
		{Source{Format: JPEG, Width: 10, Height: 10}, 100*WorkPerPixel + 10*rowWork},
		{Source{Format: JPEG, Width: 10, Height: 10, scans: 4}, 100*WorkPerPixel + 10*rowWork},
		{Source{Format: JPEG, Width: 10, Height: 10, scans: 5}, 200*WorkPerPixel + 10*rowWork},
		{Source{Format: JPEG, Width: MaxSide, Height: 1, area: MaxSide * 16}, MaxSide*16*WorkPerPixel + rowWork},
		{Source{Format: JPEG, Width: MaxSide, Height: MaxPixels / MaxSide, scans: MaxJPEGScans}, MaxPixels*WorkPerPixel*8 + MaxPixels/MaxSide*rowWork},
	} {
		if got := c.s.work(); got != c.want {
			t.Errorf("%+v: %d, want %d", c.s, got, c.want)
		}
	}
	// The default MaxWork is at least 20 times the Work of the largest report
	// of the corpus: that share stays free for the text of a document whose
	// images reach every cap.
	l := budget.Defaults()
	worst := MaxTotalPixels*WorkPerPixel + l[budget.InputBytes]/payloadBytesPerWork + MaxImages*MaxSide*rowWork
	if worst+l[budget.Work]/20 > l[budget.Work] {
		t.Errorf("a document at every image cap needs %d Work: no longer renders under the default MaxWork", worst)
	}
}

func TestAlpha(t *testing.T) {
	opaque := gradient(5, 5, color.NRGBAModel, false)
	if mustDecode(t, pngURL(encodePNG(t, opaque))).Alpha {
		t.Error("opaque NRGBA has alpha")
	}
	one := image.NewNRGBA(image.Rect(0, 0, 5, 5))
	for i := range one.Pix {
		one.Pix[i] = 0xff
	}
	one.Pix[len(one.Pix)-1] = 0xfe
	if !mustDecode(t, pngURL(encodePNG(t, one))).Alpha {
		t.Error("one translucent pixel missed")
	}
	pal := image.NewPaletted(image.Rect(0, 0, 4, 1), color.Palette{color.NRGBA{9, 9, 9, 0xff}, color.NRGBA{1, 2, 3, 0}})
	if mustDecode(t, pngURL(encodePNG(t, pal))).Alpha {
		t.Error("unused transparent palette entry counted")
	}
	pal.Pix[3] = 1
	if !mustDecode(t, pngURL(encodePNG(t, pal))).Alpha {
		t.Error("transparent palette entry missed")
	}
	gray := pngURL(rawPNG(ihdr(2, 1, 8, pngGray, 0), pngChunk("tRNS", []byte{0, 7}), idat(t, []byte{0, 7, 8}), iend))
	d := mustDecode(t, gray)
	if !d.Gray || !d.Alpha || !bytes.Equal(rgba(t, d), []byte{7, 7, 7, 0, 8, 8, 8, 0xff}) {
		t.Errorf("gray with tRNS: gray %v alpha %v %v", d.Gray, d.Alpha, rgba(t, d))
	}
}

func TestRowConversions(t *testing.T) {
	r := image.Rect(0, 0, 2, 1)
	pal := image.NewPaletted(r, color.Palette{color.RGBA{10, 20, 30, 255}})
	pal.Pix[1] = 200 // past the palette: opaque black
	cmyk := image.NewCMYK(r)
	copy(cmyk.Pix, []byte{0, 0, 0, 0, 255, 0, 128, 64})
	ycc := image.NewYCbCr(r, image.YCbCrSubsampleRatio420)
	ycc.Y[0], ycc.Y[1] = 0, 255
	for i := range ycc.Cb {
		ycc.Cb[i], ycc.Cr[i] = 128, 128
	}
	premul := &image.RGBA{Pix: []byte{0x80, 0x40, 0, 0x80, 0, 0, 0, 0}, Stride: 8, Rect: r}
	rgba64 := &image.RGBA64{Pix: []byte{0x80, 0, 0x40, 0, 0, 0, 0x80, 0, 0xff, 0xff, 0, 0, 0, 0, 0xff, 0xff}, Stride: 16, Rect: r}
	g16 := &image.Gray16{Pix: []byte{0x12, 0x34, 0xff, 0x00}, Stride: 4, Rect: r}
	n64 := &image.NRGBA64{Pix: []byte{1, 0, 2, 0, 3, 0, 4, 0, 5, 9, 6, 9, 7, 9, 0xff, 0xff}, Stride: 16, Rect: r}
	r0, g0, b0 := color.CMYKToRGB(255, 0, 128, 64)
	for _, c := range []struct {
		name string
		img  image.Image
		want []byte
	}{
		{"paletted", pal, []byte{10, 20, 30, 255, 0, 0, 0, 255}},
		{"cmyk", cmyk, []byte{255, 255, 255, 255, r0, g0, b0, 255}},
		{"ycbcr", ycc, []byte{0, 0, 0, 255, 255, 255, 255, 255}},
		{"rgba premultiplied", premul, []byte{0xff, 0x7f, 0, 0x80, 0, 0, 0, 0}},
		{"rgba64 premultiplied", rgba64, []byte{0xff, 0x7f, 0, 0x80, 0xff, 0, 0, 0xff}},
		{"rgba saturates", &image.RGBA{Pix: []byte{0xff, 0, 0, 0x10, 0, 0, 0, 0xff}, Stride: 8, Rect: r}, []byte{0xff, 0, 0, 0x10, 0, 0, 0, 0xff}},
		{"gray16 high byte", g16, []byte{0x12, 0x12, 0x12, 255, 0xff, 0xff, 0xff, 255}},
		{"nrgba64 high byte", n64, []byte{1, 2, 3, 4, 5, 6, 7, 0xff}},
		{"generic", &image.Alpha{Pix: []byte{4, 0xff}, Stride: 2, Rect: r}, []byte{0xff, 0xff, 0xff, 4, 0xff, 0xff, 0xff, 0xff}},
	} {
		t.Run(c.name, func(t *testing.T) {
			img := c.img
			if m, ok := img.(*image.CMYK); ok {
				img = cmykToRGBA(m)
			}
			d := &Decoded{Width: 2, Height: 1, img: img, Alpha: hasAlpha(img)}
			if got := rgba(t, d); !bytes.Equal(got, c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
			if d.Alpha != hasTransparent(c.want) {
				t.Errorf("Alpha %v", d.Alpha)
			}
		})
	}
}

func TestWriteJPEG(t *testing.T) {
	for _, c := range []struct {
		name  string
		src   string
		comps int
	}{
		{"ycbcr", jpegURL(encodeJPEG(t, gradient(33, 17, color.NRGBAModel, false))), 3},
		{"gray", jpegURL(encodeJPEG(t, gradient(33, 17, color.GrayModel, false))), 1},
		{"cmyk", jpegURL(synthJPEG(jpegSpec{w: 33, h: 17, nComp: 4, sof: mSOF0, extra: adobeAPP14(2)})), 3},
		{"rgb", jpegURL(synthJPEG(jpegSpec{w: 33, h: 17, nComp: 3, sof: mSOF2, acBands: 1, extra: adobeAPP14(0)})), 3},
	} {
		t.Run(c.name, func(t *testing.T) {
			d := mustDecode(t, c.src)
			var buf bytes.Buffer
			if err := d.WriteJPEG(&buf); err != nil {
				t.Fatal(err)
			}
			cfg, err := jpeg.DecodeConfig(bytes.NewReader(buf.Bytes()))
			if err != nil || cfg.Width != 33 || cfg.Height != 17 {
				t.Fatalf("%v %+v", err, cfg)
			}
			if gray := cfg.ColorModel == color.GrayModel; gray != (c.comps == 1) || d.Gray != gray {
				t.Errorf("colour model %v, Gray %v", cfg.ColorModel, d.Gray)
			}
			re, err := jpeg.Decode(bytes.NewReader(buf.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			if m := maxDelta(rgba(t, &Decoded{Width: 33, Height: 17, Gray: d.Gray, img: re}), rgba(t, d)); m > 48 {
				t.Errorf("re-encoded samples differ by %d", m)
			}
		})
	}
	d := &Decoded{Width: 1, Height: 1, Gray: true, img: image.NewNRGBA(image.Rect(0, 0, 1, 1))}
	if err := d.WriteJPEG(io.Discard); !errors.Is(err, errGrayJPEG) {
		t.Errorf("gray NRGBA: %v", err)
	}
}

type failWriter struct{ after int }

var errWrite = errors.New("write failed")

func (w *failWriter) Write(p []byte) (int, error) {
	if w.after <= 0 {
		return 0, errWrite
	}
	w.after--
	return len(p), nil
}

func TestWriteErrorsPropagate(t *testing.T) {
	d := mustDecode(t, pngURL(encodePNG(t, gradient(4, 4, color.NRGBAModel, true))))
	for name, f := range map[string]func(io.Writer) error{"samples": d.WriteSamples, "alpha": d.WriteAlpha, "jpeg": d.WriteJPEG} {
		if err := f(&failWriter{}); !errors.Is(err, errWrite) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestWritersAllocateOneRow(t *testing.T) {
	const w, h = 1000, 200
	for _, m := range []image.Image{gradient(w, h, color.NRGBAModel, true), gradient(w, h, color.NRGBA64Model, true)} {
		d := mustDecode(t, pngURL(encodePNG(t, m)))
		for name, f := range map[string]func(io.Writer) error{"samples": d.WriteSamples, "alpha": d.WriteAlpha} {
			var err error
			if a := totalAlloc(func() { err = f(io.Discard) }); a > 4*w+1024 || err != nil {
				t.Errorf("%T %s: allocated %d bytes, %v", d.img, name, a, err)
			}
		}
	}
}

func TestDecodeDeterministic(t *testing.T) {
	src := jpegURL(encodeJPEG(t, gradient(64, 48, color.NRGBAModel, false)))
	var first []byte
	for i := range 5 {
		var buf bytes.Buffer
		if err := mustDecode(t, src).WriteJPEG(&buf); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = buf.Bytes()
		} else if !bytes.Equal(first, buf.Bytes()) {
			t.Fatal("re-encoded JPEG differs between runs")
		}
	}
}
