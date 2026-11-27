// Package testcorpus generates the deterministic test fixtures of the
// converter: the report templates filled with fake data, hostile documents and
// the host-connector reference configuration. Nothing is read from the network
// and no generated file is committed; the SHA-256 of every fixture is pinned by
// the package tests.
//
// It is test support: only _test.go files and benchmarks may import it.
package testcorpus

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"math"
	"path"
	"sort"
	"strings"
)

// Size selects the amount of fake data of a report fixture.
type Size string

// Report sizes. Large is about 250 files and 10 archives.
const (
	Small Size = "small"
	Large Size = "large"
)

// HostConnectorPageCSS is the print stylesheet of the host-connector reference
// configuration. It is only used as Options.ExtraCSS: production HTML carries no
// @page rule and the report fixtures are the template output as is.
//
// Measured: the margins are what Chrome makes of the host's 1/0.3/1.3/0.3 in
// margins, floored to whole CSS px (probe in design 4.1): do not turn them back
// into inches. The paper is not here: it is Options.PageSize (see PaperCSS for
// the tests below the public API).
//
// Fitted: the footer reproduces the footerTemplate Chrome draws (DejaVu Sans
// Bold 10px, TLP:AMBER in #FFC000 at x = 22.5 pt, "Page N of M" ending at the
// right margin, text boxes at 808-817 pt from the top); its paddings (2px,
// 1.9px, 33.5px) are fitted to the Chrome references, to within 0.01 pt.
const HostConnectorPageCSS = `@page {
            margin: 96px 28px 124px 28px;
            @bottom-left { content: "TLP:AMBER"; color: #FFC000; font-family: "DejaVu Sans"; font-weight: bold; font-size: 10px; vertical-align: bottom; padding: 0 0 33.5px 2px; }
            @bottom-right { content: "Page " counter(page) " of " counter(pages); font-family: "DejaVu Sans"; font-weight: bold; font-size: 10px; vertical-align: bottom; padding: 0 1.9px 33.5px 0; }
        }`

type file struct {
	Name, SHA256, Size, MitigationReason, QuarantineLocation, GMalwareURL string
	Malwares                                                              []string
	ExtractedFiles                                                        []file
}

type archive struct {
	Name, SHA256, Size, QuarantineLocation string
	FileCount, MoreExtracted               int
	ExtractedFiles                         []file
}

type (
	notAnalyzed struct{ Name, Size, Reason, Origin string }
	errorFile   struct{ Name, Error string }
)

func sha(i int) string { return fmt.Sprintf("%064x", uint64(i&math.MaxInt32)*0x9e3779b97f4a7c15) }

func mkFile(i int) file {
	f := file{
		Name:             fmt.Sprintf("C:\\Users\\victim\\Downloads\\invoice_%04d_très_urgent_à_ouvrir.pdf.exe", i),
		SHA256:           sha(i),
		Size:             fmt.Sprintf("%d.%d MiB", i%50, i%10),
		Malwares:         []string{"Trojan.GenericKD.4821", "Win32/Emotet.AB"},
		MitigationReason: "malware détecté",
	}
	if i%3 != 0 {
		f.GMalwareURL = "https://gmalware.example/analysis/" + sha(i)[:16]
	}
	return f
}

const archiveMembers = 4

func reportData(nFiles, nArchives int) map[string]any {
	files := make([]file, nFiles)
	for i := range files {
		files[i] = mkFile(i)
	}
	archives := make([]archive, nArchives)
	for a := range archives {
		members := make([]file, archiveMembers)
		for m := range members {
			members[m] = mkFile(1000*a + m)
			if m == 0 {
				members[m].ExtractedFiles = []file{mkFile(9999), mkFile(9998)}
				members[m].ExtractedFiles[0].ExtractedFiles = []file{mkFile(9997)}
			}
		}
		archives[a] = archive{
			Name: fmt.Sprintf("dump_%d.zip", a), SHA256: sha(a + 77), Size: "12 MiB",
			QuarantineLocation: "/var/quarantine/" + sha(a)[:12], FileCount: archiveMembers, ExtractedFiles: members, MoreExtracted: 3,
		}
	}
	return map[string]any{
		"ScanID": "a1b2c3d4-e5f6", "NbFileSubmitted": 1234, "NbFileAnalyzed": 1200, "NbFileNotAnalyzed": 4,
		"FileVolume": "3.2 GiB", "AnalyzedVolume": "3.0 GiB", "FilteredVolume": "200 MiB",
		"TotalMitigated": nFiles, "TotalMalware": nFiles, "TotalErrors": 2,
		"ScanStartTime": "2026-11-23 10:42:00", "Duration": "4m12s",
		"MitigatedFiles": files, "MoreMitigatedFiles": 12,
		"MitigatedArchives": archives, "MoreMitigatedArchives": 1,
		"NotAnalyzedFiles": []notAnalyzed{
			{"secret.7z", "1 MiB", "chiffré", "dump_0.zip"},
			{"x.bin", "0 B", "vide", ""},
			{"y.iso", "4 GiB", "trop gros", ""},
			{"z.rar", "2 MiB", "corrompu", "dump_1.zip"},
		},
		"MoreNotAnalyzed": 0,
		"ErrorFiles":      []errorFile{{"a.doc", "timeout"}, {"b.xls", "parse error: unexpected EOF"}},
		"MoreErrorFiles":  0,
	}
}

var templateFuncs = template.FuncMap{
	"mergeArgsIntoSlice": func(a ...any) []any { return a },
	"others": func(n int, s, p string) string {
		if n == 1 {
			return "1 autre" + s
		}
		return fmt.Sprintf("%d autres%s", n, p)
	},
	"add": func(a, b int) int { return a + b },
	"mul": func(a, b int) int { return a * b },
	"min": func(a, b int) int { return min(a, b) },
}

// ReportNames returns the sorted names of the report templates found in
// templates, a directory holding files named <name>.html.tmpl.
func ReportNames(templates fs.FS) ([]string, error) {
	paths, err := fs.Glob(templates, "*.html.tmpl")
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, errors.New("testcorpus: no *.html.tmpl template found")
	}
	names := make([]string, len(paths))
	for i, p := range paths {
		names[i] = strings.TrimSuffix(path.Base(p), ".html.tmpl")
	}
	sort.Strings(names)
	return names, nil
}

// ReportTemplate parses the template name, with the functions of the corpus,
// and returns it with the fake data of size: executing one with the other gives
// the bytes of Report.
func ReportTemplate(templates fs.FS, name string, size Size) (*template.Template, any, error) {
	var data map[string]any
	switch size {
	case Small:
		data = reportData(5, 1)
	case Large:
		data = reportData(250, 10)
	default:
		return nil, nil, fmt.Errorf("testcorpus: unknown size %q", size)
	}
	raw, err := readCapped(templates, name+".html.tmpl", maxTemplateBytes)
	if err != nil {
		return nil, nil, err
	}
	t, err := template.New(name).Funcs(templateFuncs).Parse(string(raw))
	if err != nil {
		return nil, nil, err
	}
	return t, data, nil
}

// Report executes the template name with the fake data of size (ReportTemplate
// then Execute).
func Report(templates fs.FS, name string, size Size) ([]byte, error) {
	t, data, err := ReportTemplate(templates, name, size)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// FixtureName returns the stable fixture name of a report, e.g.
// "report_00-host-connector_small".
func FixtureName(name string, size Size) string {
	return "report_" + name + "_" + string(size)
}

// Read caps: templates are small; fonts follow the 32 MiB cap of design §6.1.
const (
	maxTemplateBytes = 1 << 20
	maxFontBytes     = 32 << 20
)

func readCapped(fsys fs.FS, name string, limit int64) ([]byte, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	_, err = buf.ReadFrom(io.LimitReader(f, limit+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, err
	}
	if int64(buf.Len()) > limit {
		return nil, fmt.Errorf("testcorpus: %s exceeds %d bytes", name, limit)
	}
	return buf.Bytes(), nil
}

// FontFile names a font file and its family.
type FontFile struct{ Name, Family string }

// HostConnectorFontFiles returns, relative to the test font directory, the
// explicit font file list of the host-connector reference configuration with
// the family each file belongs to.
func HostConnectorFontFiles() []FontFile {
	return []FontFile{
		{"LiberationSans-Regular.ttf", "Liberation Sans"},
		{"LiberationSans-Bold.ttf", "Liberation Sans"},
		{"LiberationSans-Italic.ttf", "Liberation Sans"},
		{"LiberationSans-BoldItalic.ttf", "Liberation Sans"},
		{"DejaVuSans.ttf", "DejaVu Sans"},
		{"DejaVuSans-Bold.ttf", "DejaVu Sans"},
		{"DejaVuSansMono.ttf", "DejaVu Sans Mono"},
		{"DejaVuSerif.ttf", "DejaVu Serif"},
		{"DejaVuSerif-Bold.ttf", "DejaVu Serif"},
		{"DejaVuSerif-Italic.ttf", "DejaVu Serif"},
	}
}

// HostConnectorConfig is the reference configuration of design §4.1 expressed
// without the public types, which this package cannot import (cycle with
// in-package tests). The public-API tests map it to htmltopdf.Options.
type HostConnectorConfig struct {
	SansSerif, Monospace string
	Fallback             []string
	Scale                float64
	ExtraCSS             string
	// PageSize is the paper in CSS px, width then height (Options.PageSize).
	PageSize [2]float64
}

// PaperWidth and PaperHeight are the paper of the reference configuration in CSS
// px: what Chrome makes of 8.27 x 11.69 in (design 4.1).
const (
	PaperWidth  = 794.56
	PaperHeight = 1122.56
)

// PaperCSS sets that paper from a stylesheet, for the tests below the public API,
// which cannot set Options.PageSize.
const PaperCSS = "@page { size: 794.56px 1122.56px }"

// HostConnector returns a fresh copy of the reference configuration.
func HostConnector() HostConnectorConfig {
	return HostConnectorConfig{
		SansSerif: "Liberation Sans",
		Monospace: "DejaVu Sans Mono",
		Fallback:  []string{"DejaVu Sans"},
		Scale:     0.9,
		ExtraCSS:  HostConnectorPageCSS,
		PageSize:  [2]float64{PaperWidth, PaperHeight},
	}
}

// ChromePaper returns the reference configuration of templates 01-03: the
// host-connector fonts, scale and paper, and no ExtraCSS. Chrome prints them
// with the caller's paper but keeps the margins and margin boxes of their own
// @page rule, which HostConnectorPageCSS would override.
func ChromePaper() HostConnectorConfig {
	hc := HostConnector()
	hc.ExtraCSS = ""
	return hc
}

// LoadedFont is the bytes of a reference font and its family.
type LoadedFont struct {
	Data   []byte
	Family string
}

// ReadHostConnectorFonts reads the reference font files from fonts, a
// directory holding the test fonts.
func ReadHostConnectorFonts(fonts fs.FS) ([]LoadedFont, error) {
	files := HostConnectorFontFiles()
	out := make([]LoadedFont, 0, len(files))
	for _, f := range files {
		data, err := readCapped(fonts, f.Name, maxFontBytes)
		if err != nil {
			return nil, err
		}
		out = append(out, LoadedFont{Data: data, Family: f.Family})
	}
	return out, nil
}
