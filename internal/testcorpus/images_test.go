package testcorpus

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

var testImageCases = sync.OnceValue(ImageCases)

var reasons = []string{"scheme", "type", "encoding", "format", "size", "corrupt", "count", "total"}

func imageSums(t testing.TB) map[string]string {
	t.Helper()
	m := map[string]string{}
	for _, c := range testImageCases() {
		if _, dup := m["case/"+c.Name]; dup {
			t.Errorf("duplicate image case %s", c.Name)
		}
		m["case/"+c.Name] = sum([]byte(c.Src))
	}
	for _, d := range ImageDocs() {
		if _, dup := m["doc/"+d.Name]; dup {
			t.Errorf("duplicate image doc %s", d.Name)
		}
		m["doc/"+d.Name] = sum(d.HTML())
	}
	return m
}

// TestPrintImageSums prints the table of expectedImageSums after a deliberate
// change of a generator: CORPUS_PRINT_SUMS=1 go test -run TestPrintImageSums -v ./internal/testcorpus
func TestPrintImageSums(t *testing.T) {
	if os.Getenv("CORPUS_PRINT_SUMS") == "" {
		t.Skip("set CORPUS_PRINT_SUMS=1 to print the table")
	}
	m := imageSums(t)
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("\t%q: %q,\n", k, m[k])
	}
}

func TestImageSums(t *testing.T) {
	got := imageSums(t)
	for k, v := range got {
		want, ok := expectedImageSums[k]
		if !ok {
			t.Errorf("no committed SHA-256 for %s", k)
		} else if v != want {
			t.Errorf("%s: SHA-256 = %s, want %s", k, v, want)
		}
	}
	for k := range expectedImageSums {
		if _, ok := got[k]; !ok {
			t.Errorf("committed SHA-256 for unknown image fixture %s", k)
		}
	}
}

func TestImagesFitMaxInputBytes(t *testing.T) {
	limit := budget.Defaults()[budget.InputBytes]
	for _, c := range testImageCases() {
		if int64(len(c.Src)) > limit {
			t.Errorf("case %s: %d bytes > MaxInputBytes %d", c.Name, len(c.Src), limit)
		}
	}
	for _, d := range ImageDocs() {
		if n := len(d.HTML()); int64(n) > limit {
			t.Errorf("doc %s: %d bytes > MaxInputBytes %d", d.Name, n, limit)
		}
	}
}

func TestImageDocRejectsAreReasons(t *testing.T) {
	for _, d := range ImageDocs() {
		for r := range d.Rejects {
			if !slices.Contains(reasons, r) {
				t.Errorf("doc %s: %q is not a reason keyword", d.Name, r)
			}
		}
	}
}

// dataURLParts splits a data: URL into its lower-case media type and the raw
// text after ";base64,"; hasB64 is false when that marker is missing.
func dataURLParts(src string) (mediaType, enc string, hasB64 bool) {
	rest, ok := strings.CutPrefix(strings.ToLower(src), "data:")
	if !ok {
		return "", "", false
	}
	mediaType, _, _ = strings.Cut(rest, ";")
	mediaType, _, _ = strings.Cut(mediaType, ",")
	i := strings.Index(strings.ToLower(src), ";base64,")
	if i < 0 {
		return mediaType, "", false
	}
	return mediaType, src[i+len(";base64,"):], true
}

func decodeBase64(enc string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(strings.NewReplacer("\n", "", " ", "", "\t", "", "\r", "").Replace(enc))
}

// jpegStats walks the JPEG markers: the type of the first SOF marker, the
// markers before it and the number of SOS markers.
func jpegStats(d []byte) (sof byte, markers, scans int) {
	for i := 2; i+4 <= len(d); {
		if d[i] != 0xff {
			i++
			continue
		}
		m := d[i+1]
		if m == 0 || m == 0xff || (m >= 0xd0 && m <= 0xd7) {
			i++
			continue
		}
		if m >= 0xc0 && m <= 0xcf && m != 0xc4 && m != 0xc8 && m != 0xcc && sof == 0 {
			sof = m
		}
		if sof == 0 {
			markers++
		}
		if m == 0xda {
			scans++
		}
		if m == 0xd9 {
			break
		}
		i += 2 + int(d[i+2])<<8 | int(d[i+3])
	}
	return sof, markers, scans
}

// decodeCost is the memory estimate of design M6 §4: pixels times the bytes of
// the decoded type, plus for a progressive JPEG pixels × components × 4.
func decodeCost(cfg image.Config, progressive bool) int {
	px := cfg.Width * cfg.Height
	bpp, comps := 1, 1
	switch m := cfg.ColorModel.(type) {
	case color.Palette:
	default:
		switch m {
		case color.GrayModel:
		case color.Gray16Model:
			bpp = 2
		case color.YCbCrModel:
			bpp, comps = 3, 3
		case color.CMYKModel:
			bpp, comps = 4, 4
		case color.RGBAModel, color.NRGBAModel:
			bpp = 4
		case color.RGBA64Model, color.NRGBA64Model:
			bpp = 8
		}
	}
	if progressive {
		return px*bpp + px*comps*4
	}
	return px * bpp
}

// TestImageCasesAgreeWithStdlib checks the premise of every Want against the
// Go decoders the raster layer is built on, without matching error texts.
func TestImageCasesAgreeWithStdlib(t *testing.T) {
	for _, c := range testImageCases() {
		t.Run(c.Name, func(t *testing.T) {
			mediaType, enc, hasB64 := dataURLParts(c.Src)
			switch c.Want {
			case "scheme":
				if strings.HasPrefix(strings.ToLower(c.Src), "data:") {
					t.Fatal("a data: URL")
				}
				return
			case "type":
				if slices.Contains([]string{"image/png", "image/jpeg", "image/jpg"}, mediaType) {
					t.Fatalf("media type %q is accepted", mediaType)
				}
				return
			case "encoding":
				if hasB64 {
					if _, err := decodeBase64(enc); err == nil {
						t.Fatal("the base64 payload decodes")
					}
				}
				return
			}
			if !hasB64 {
				t.Fatalf("Want %q for a source without ;base64,", c.Want)
			}
			data, err := decodeBase64(enc)
			if err != nil {
				t.Fatal(err)
			}
			cfg, _, cerr := image.DecodeConfig(bytes.NewReader(data))
			var sof byte
			var markers, scans int
			if bytes.HasPrefix(data, []byte{0xff, 0xd8}) {
				sof, markers, scans = jpegStats(data)
			}
			exceeds := cerr == nil && (cfg.Width > imgMaxSide || cfg.Height > imgMaxSide ||
				cfg.Width*cfg.Height > imgMaxPixels || decodeCost(cfg, sof == 0xc2) > imgMaxDecodeBytes)
			rasterFormat := cerr == nil && (cfg.Width == 0 || cfg.Height == 0 || scans > imgMaxJPEGScans || markers > imgMaxJPEGMarkers)
			switch c.Want {
			case "":
				if cerr != nil || exceeds || rasterFormat {
					t.Fatalf("drawn image: header %v, over a limit %v, raster-format %v", cerr, exceeds, rasterFormat)
				}
				if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
					t.Fatalf("Decode: %v", err)
				}
			case "corrupt":
				if cerr != nil || exceeds || rasterFormat {
					t.Fatalf("the header must be accepted: %v", cerr)
				}
				if _, _, err := image.Decode(bytes.NewReader(data)); err == nil {
					t.Fatal("Decode succeeded")
				}
			case "size":
				if cerr != nil || !exceeds {
					t.Fatalf("header %v, over a size limit %v", cerr, exceeds)
				}
			case "format":
				if cerr == nil && !rasterFormat {
					t.Fatalf("Go reads the header (%dx%d) and no raster-only rule applies", cfg.Width, cfg.Height)
				}
			default:
				t.Fatalf("unexpected Want %q", c.Want)
			}
		})
	}
}

// TestPNGVariantsDecodeToExpectedModels: each generated variant decodes into
// the colour model of its PNG type and depth.
func TestPNGVariantsDecodeToExpectedModels(t *testing.T) {
	const (
		opaque = iota // no pixel is transparent
		some          // at least one pixel is not opaque
		mixed         // at least one pixel is transparent and one is opaque
	)
	tests := []struct {
		name  string
		model string
		alpha int
	}{
		{"png_gray1_33x31", "*image.Gray", opaque},
		{"png_gray2_33x31", "*image.Gray", opaque},
		{"png_gray4_33x31", "*image.Gray", opaque},
		{"png_gray8_33x31", "*image.Gray", opaque},
		{"png_gray16_33x31", "*image.Gray16", opaque},
		{"png_rgb8_33x31", "*image.RGBA", opaque},
		{"png_rgb16_33x31", "*image.RGBA64", opaque},
		{"png_graya8_33x31", "*image.NRGBA", some},
		{"png_graya16_33x31", "*image.NRGBA64", some},
		{"png_rgba8_33x31", "*image.NRGBA", mixed},
		{"png_rgba16_33x31", "*image.NRGBA64", mixed},
		{"png_pal1_33x31", "*image.Paletted", opaque},
		{"png_pal2_33x31", "*image.Paletted", opaque},
		{"png_pal4_33x31", "*image.Paletted", opaque},
		{"png_pal8_33x31", "*image.Paletted", opaque},
		{"png_pal8_trns_33x31", "*image.Paletted", some},
		{"png_pal4_adam7_31x31", "*image.Paletted", opaque},
		{"png_gray2_trns_33x31", "*image.NRGBA", mixed},
		{"png_rgb16_trns_33x31", "*image.NRGBA64", mixed},
		{"png_rgba16_37x23", "*image.NRGBA64", mixed},
		{"png_rgba16_37x23_adam7", "*image.NRGBA64", mixed},
	}
	byName := map[string]string{}
	for _, c := range pngVariants() {
		byName[c.Name] = c.Src
	}
	if len(byName) != len(tests) {
		t.Errorf("%d variants, %d rows in the table", len(byName), len(tests))
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src, ok := byName[tc.name]
			if !ok {
				t.Fatal("no such variant")
			}
			_, enc, _ := dataURLParts(src)
			data, err := decodeBase64(enc)
			if err != nil {
				t.Fatal(err)
			}
			img, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			if got := fmt.Sprintf("%T", img); got != tc.model {
				t.Fatalf("decoded as %s, want %s", got, tc.model)
			}
			var clear, solid bool
			for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
				for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
					switch _, _, _, a := img.At(x, y).RGBA(); a {
					case 0xffff:
						solid = true
					default:
						clear = true
					}
				}
			}
			if clear != (tc.alpha != opaque) || (tc.alpha == mixed && !solid) {
				t.Fatalf("a pixel is not opaque: %v, a pixel is opaque: %v, want alpha class %d", clear, solid, tc.alpha)
			}
		})
	}
}

// TestInterlacedTwinDecodesToSamePixels: the Adam7 case holds the pixels of
// the plain one.
func TestInterlacedTwinDecodesToSamePixels(t *testing.T) {
	byName := map[string]string{}
	for _, c := range testImageCases() {
		byName[c.Name] = c.Src
	}
	decode := func(name string) image.Image {
		_, enc, _ := dataURLParts(byName[name])
		data, err := decodeBase64(enc)
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		return img
	}
	a, b := decode("png_rgba16_37x23"), decode("png_rgba16_37x23_adam7")
	if a.Bounds() != b.Bounds() {
		t.Fatalf("bounds %v vs %v", a.Bounds(), b.Bounds())
	}
	for y := range 23 {
		for x := range 37 {
			if a.At(x, y) != b.At(x, y) {
				t.Fatalf("pixel %d,%d differs", x, y)
			}
		}
	}
}

func TestZlibRepeatMatchesItsPattern(t *testing.T) {
	for _, tc := range []struct{ period, total int }{{1, 1}, {1, 2}, {1, 259}, {2, 3}, {3, 7}, {4, 0}, {5, 5000}, {300, 1000}, {16385, 200000}, {32768, 100000}} {
		pat := make([]byte, tc.period)
		for i := range pat {
			pat[i] = byte(i * 7 & 0xff)
		}
		want := make([]byte, tc.total)
		for i := range want {
			want[i] = pat[i%tc.period]
		}
		r, err := zlib.NewReader(bytes.NewReader(zlibRepeat(pat, tc.total)))
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if _, err := io.Copy(&out, io.LimitReader(r, int64(tc.total)+1)); err != nil {
			t.Fatalf("period %d total %d: %v", tc.period, tc.total, err)
		}
		if !bytes.Equal(out.Bytes(), want) {
			t.Fatalf("period %d total %d: inflated bytes differ", tc.period, tc.total)
		}
	}
}

func TestJPEGGeneratorsDecode(t *testing.T) {
	img, err := jpeg.Decode(bytes.NewReader(jpegBaseline(40, 24, 3)))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 40 || img.Bounds().Dy() != 24 {
		t.Fatalf("bounds %v", img.Bounds())
	}
}

const imagesRoot = "../../testdata/images"

var imageHashLine = regexp.MustCompile("^\\| `([^`]+)` \\| `([0-9a-f]{64})` \\|")

func readBounded(t *testing.T, name string) []byte {
	t.Helper()
	f, err := os.Open(filepath.Clean(name))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	var b bytes.Buffer
	if _, err := b.ReadFrom(io.LimitReader(f, 1<<20)); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// imageFiles lists the .png and .jpeg files of testdata/images (slash paths
// relative to it) and fails on any other image extension or a file over 64 KiB.
func imageFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(imagesRoot, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel := filepath.ToSlash(strings.TrimPrefix(p, imagesRoot+"/"))
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > 64<<10 {
			t.Errorf("%s is %d bytes, over 64 KiB", rel, info.Size())
		}
		switch ext := strings.ToLower(filepath.Ext(rel)); ext {
		case ".png", ".jpeg":
			files = append(files, rel)
		case ".jpg", ".gif", ".webp", ".bmp", ".tif", ".tiff":
			t.Errorf("%s: image format outside the corpus", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// TestImageFilesMatchReadme: every image of testdata/images has its SHA-256 in
// README.md and the hash is right.
func TestImageFilesMatchReadme(t *testing.T) {
	listed := map[string]string{}
	for _, l := range strings.Split(string(readBounded(t, imagesRoot+"/README.md")), "\n") {
		if m := imageHashLine.FindStringSubmatch(l); m != nil {
			if _, dup := listed[m[1]]; dup {
				t.Errorf("%s listed twice in README.md", m[1])
			}
			listed[m[1]] = m[2]
		}
	}
	files := imageFiles(t)
	for _, rel := range files {
		want, ok := listed[rel]
		if !ok {
			t.Errorf("%s has no SHA-256 in README.md", rel)
		} else if got := sum(readBounded(t, filepath.Join(imagesRoot, rel))); got != want {
			t.Errorf("%s: SHA-256 %s, README.md says %s", rel, got, want)
		}
	}
	for rel := range listed {
		if !slices.Contains(files, rel) {
			t.Errorf("README.md lists %s, which is not in testdata/images", rel)
		}
	}
}

// TestStdlibImagesDecode covers the two progressive.truncated files, which Go
// decodes to a partial image without an error.
func TestStdlibImagesDecode(t *testing.T) {
	for _, rel := range imageFiles(t) {
		if _, _, err := image.Decode(bytes.NewReader(readBounded(t, filepath.Join(imagesRoot, rel)))); err != nil {
			t.Errorf("%s: %v", rel, err)
		}
	}
}
