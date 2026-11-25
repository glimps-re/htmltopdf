package layout

import (
	"testing"

	"github.com/glimps-re/htmltopdf/internal/display"
)

func itemsOf(pages []*Page) (anchors []display.Anchor, links []display.Link) {
	for _, pg := range pages {
		for _, it := range pg.Items {
			switch v := it.(type) {
			case display.Anchor:
				anchors = append(anchors, v)
			case display.Link:
				links = append(links, v)
			}
		}
	}
	return anchors, links
}

// An internal link is a Link with the id of its target and no URI; the box it
// points to (block or inline) gets an Anchor at the top of its first fragment.
func TestInternalLinksAndAnchors(t *testing.T) {
	html := `<p><a href="#blk">to block</a> <a href="#inl">to inline</a> <a href="https://e.example/">out</a></p>
<div style="height:200px"></div><h2 id=blk>Block</h2><p>x <span id=inl>inline</span> y</p>`
	pages := newRun(t, html, opts{css: "@page{size:400px 300px;margin:0} body,h2,p{margin:0}"}).all(t)
	anchors, links := itemsOf(pages)
	if len(anchors) != 2 || anchors[0].Name != "blk" || anchors[1].Name != "inl" {
		t.Fatalf("anchors %+v", anchors)
	}
	var dests, uris int
	for _, l := range links {
		if l.Dest != "" && l.URI != "" {
			t.Errorf("link with both: %+v", l)
		}
		if l.Dest != "" {
			dests++
		} else {
			uris++
		}
	}
	if dests != 2 || uris != 1 {
		t.Errorf("%d internal and %d external links", dests, uris)
	}
	// The block anchor is at the top of the h2 box, which is on the second page here.
	blk := fragByID(t, pages, "blk")
	for _, pg := range pages {
		for _, it := range pg.Items {
			if a, ok := it.(display.Anchor); ok && a.Name == "blk" && a.At.Y != blk.rect.Y {
				t.Errorf("anchor at %v, box at %v", a.At.Y, blk.rect.Y)
			}
		}
	}
}

// A link to an id inside display: none, or to nothing, makes no link at all, and
// a box nobody links to has no anchor.
func TestLinksToNothingAreDropped(t *testing.T) {
	html := `<a href="#hidden">a</a> <a href="#nobody">b</a> <div style="display:none"><p id=hidden>h</p></div><p id=unlinked>u</p>`
	pages := newRun(t, html, opts{extra: smallPage}).all(t)
	anchors, links := itemsOf(pages)
	if len(anchors) != 0 || len(links) != 0 {
		t.Errorf("anchors %+v links %+v", anchors, links)
	}
}
