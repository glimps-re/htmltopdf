package htmltopdf

import (
	"strconv"
	"strings"
	"testing"
)

type h4Pattern struct{ width, on, off float64 }

// h4Strokes lists the dash patterns set in content (each with the line width in force) and counts its stroke
// operators. A line it cannot read is left out of the patterns, so the caller's counts fall short.
func h4Strokes(content string) (patterns []h4Pattern, strokes int) {
	var width float64
	for _, l := range strings.Split(content, "\n") {
		f := strings.Fields(l)
		switch {
		case len(f) == 2 && f[1] == "w":
			if w, err := strconv.ParseFloat(f[0], 64); err == nil {
				width = w
			} else {
				width = 0
			}
		case len(f) == 5 && f[0][0] == '[' && f[3] == "0" && f[4] == "d":
			on, errOn := strconv.ParseFloat(strings.TrimPrefix(f[0], "["), 64)
			off, errOff := strconv.ParseFloat(f[1], 64)
			if errOn == nil && errOff == nil && width > 0 {
				patterns = append(patterns, h4Pattern{width, on, off})
			}
		case len(f) == 1 && f[0] == "S":
			strokes++
		}
	}
	return patterns, strokes
}

// h4DocStrokes sums h4Strokes over the pages of r, and returns the largest dash count of a line.
func h4DocStrokes(r h4Run) (patterns []h4Pattern, strokes int, mostDashes float64) {
	for _, p := range r.doc.Pages {
		content := string(p.Content)
		pp, s := h4Strokes(content)
		patterns, strokes = append(patterns, pp...), strokes+s
		mostDashes = max(mostDashes, h4MaxDashes(content))
	}
	return patterns, strokes, mostDashes
}

// h4PatternShape checks that a pattern is the one of its kind: a dashed line has a dash at least twice its
// width, a dotted line a dot as long as its width.
func h4PatternShape(p h4Pattern, kind string) bool {
	if kind == "dotted" {
		return p.on > 0.99*p.width && p.on < 1.01*p.width
	}
	return p.on >= 2*p.width
}

// 10 000 two-column containers with a dashed or dotted column-rule, against the same containers with a
// solid rule: boxes and glyphs are the twin's, and so is Work (it does not see the paint); a solid rule is a fill, so
// the dashed ones are exactly n strokes, each with the dash pattern of its kind, no stroked line carries more than 4096 dashes, the
// bytes stay within a quarter of the twin's, and the text is present once.
func TestM4DashedColumnRulesTenThousand(t *testing.T) {
	n := h4Size(10000, 3000)
	for _, tc := range []struct{ kind, rule string }{{"dashed", "2px dashed #c00"}, {"dotted", "3px dotted #c00"}} {
		t.Run(tc.kind, func(t *testing.T) {
			build := func(rule string) (string, []string) {
				items, words := h4Items("DR", n, `<div class=c><p>%s</p><p>alpha</p></div>`)
				return `<style>.c{columns:2;column-rule:` + rule + `}</style>` + items, words
			}
			html, words := build(tc.rule)
			solidHTML, _ := build(strings.Fields(tc.rule)[0] + " solid #c00")
			r := h4Render(t, referenceOptions(t), html, true)
			twin := h4Render(t, referenceOptions(t), solidHTML, false)
			if r.err != nil || twin.err != nil {
				t.Fatal(r.err, twin.err)
			}
			h4TextOnce(t, r, words)
			h4WordCount(t, r, n, "alpha")
			patterns, strokes, most := h4DocStrokes(r)
			if len(patterns) != n || strokes != n || most > 4096 {
				t.Errorf("%d dash patterns and %d strokes for %d rules, %.0f dashes on the largest line", len(patterns), strokes, n, most)
			}
			for _, p := range patterns {
				if !h4PatternShape(p, tc.kind) {
					t.Fatalf("pattern on %g off %g at width %g is not %s", p.on, p.off, p.width, tc.kind)
				}
			}
			if r.boxes != twin.boxes || r.glyphs != twin.glyphs || r.work != twin.work {
				t.Errorf("work %d boxes %d glyphs %d; solid twin work %d boxes %d glyphs %d", r.work, r.boxes, r.glyphs, twin.work, twin.boxes, twin.glyphs)
			}
			if r.res.Bytes > twin.res.Bytes+twin.res.Bytes/4+64<<10 {
				t.Errorf("%d bytes against %d for the solid twin", r.res.Bytes, twin.res.Bytes)
			}
		})
	}
}

// One container 100 000 px high with a dashed rule that crosses about ninety pages: every page that carries
// column text carries a dash pattern with at most 4096 dashes on a line, the text is present once, and the
// pages and work are the solid rule's and the bytes within a quarter of its.
func TestM4DashedColumnRuleOverManyPages(t *testing.T) {
	items, words := h4Items("RP", 400, `<p style="height:240px">%s</p>`)
	doc := func(rule string) string {
		return `<div style="columns:2;column-rule:` + rule + `;height:100000px">` + items + `</div>`
	}
	r := h4Render(t, referenceOptions(t), doc("2px dashed #c00"), true)
	twin := h4Render(t, referenceOptions(t), doc("2px solid #c00"), false)
	if r.err != nil || twin.err != nil {
		t.Fatal(r.err, twin.err)
	}
	h4TextOnce(t, r, words)
	for i, p := range r.doc.Pages {
		if !strings.Contains(strings.Join(p.Text, " "), "RP") {
			continue
		}
		if patterns, _ := h4Strokes(string(p.Content)); len(patterns) == 0 {
			t.Errorf("page %d carries column text and no dash pattern", i+1)
		}
		if d := h4MaxDashes(string(p.Content)); d > 4096 {
			t.Errorf("page %d: %.0f dashes on a line", i+1, d)
		}
	}
	patterns, strokes, _ := h4DocStrokes(r)
	if len(patterns) != strokes || strokes > r.res.Pages {
		t.Errorf("%d dash patterns and %d strokes over %d pages: more than one stroke per page for the one rule", len(patterns), strokes, r.res.Pages)
	}
	if r.res.Pages != twin.res.Pages || r.work != twin.work || r.res.Bytes > twin.res.Bytes+twin.res.Bytes/4+64<<10 {
		t.Errorf("pages %d work %d bytes %d; solid twin pages %d work %d bytes %d", r.res.Pages, r.work, r.res.Bytes, twin.res.Pages, twin.work, twin.res.Bytes)
	}
}
