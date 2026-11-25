package svg

import (
	"math"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/dom"
)

func FuzzSVGPath(f *testing.F) {
	for _, s := range []string{
		"", "M0 0", "M12 2 3.5 5.5v6.2c0 5 3.6 9.1 8.5 10.3 4.9-1.2 8.5-5.3 8.5-10.3V5.5L12 2Z",
		"M5 9v10a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V9M10 13h4", "m0 0a1 1 0 0110 10", "M0 0A1e30 1e-30 360 1 1 1e30 -1e30",
		"M0 0 Q1 1 2 2 T3 3 t1 1 S1 1 2 2 s3 3 4 4 Z z l1 1", "M1e999 -1e999 L.e5 5.", "M0 0 H V h v",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, d string) {
		segs, ok := ParsePath(d)
		if !ok {
			if len(d) <= MaxPathBytes {
				t.Fatal("rejected a short path")
			}
			return
		}
		if len(segs) > 5*len(d)+1 {
			t.Fatalf("%d segments for %d bytes", len(segs), len(d))
		}
		for i, s := range segs {
			if i == 0 && s.Op != MoveTo {
				t.Fatalf("first segment is %d", s.Op)
			}
			n := map[Op]int{MoveTo: 1, LineTo: 1, CubicTo: 3, Close: 0}[s.Op]
			for k := range n {
				p := s.P[k]
				if math.IsNaN(p.X) || math.IsNaN(p.Y) || math.Abs(p.X) > MaxCoord || math.Abs(p.Y) > MaxCoord {
					t.Fatalf("segment %d: %+v", i, p)
				}
			}
		}
	})
}

func FuzzSVGShapes(f *testing.F) {
	for _, s := range []string{
		`<rect x="1e300" y="-1e300" width="1e300" height="1e300" rx="1e300"/>`, `<circle cx="1e308" cy="-1e308" r="1e308"/>`,
		`<ellipse rx="3" cx="1e7" cy="1e7"/>`, `<polyline points="0,0 1,1 2,2e999 x"/>`, `<line x1="1e400" y2="-5"/>`, `<polygon points="1 2 3"/>`,
		`<rect width="5" height="5" rx="2" ry="1e-300"/>`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, el string) {
		d, err := dom.Parse(strings.NewReader("<svg>"+el+"</svg>"), newBudget(nil))
		if err != nil {
			return
		}
		for i := range d.Elements {
			if d.Elements[i].NS != dom.NSSVG || elementName(&d.Elements[i]) != "svg" {
				continue
			}
			img, err := Parse(d, int32(i), newBudget(nil))
			if err != nil {
				return
			}
			for _, sh := range img.Shapes {
				for _, sg := range sh.Segs {
					n := map[Op]int{MoveTo: 1, LineTo: 1, CubicTo: 3, Close: 0}[sg.Op]
					for k := range n {
						if p := sg.P[k]; math.IsNaN(p.X) || math.IsNaN(p.Y) || math.Abs(p.X) > MaxCoord || math.Abs(p.Y) > MaxCoord {
							t.Fatalf("%+v", p)
						}
					}
				}
			}
		}
	})
}
