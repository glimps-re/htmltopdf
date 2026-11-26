package box

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/raster"
)

// KindImage is a replaced box like KindSVG; its dump shows the image size.
func TestKindImage(t *testing.T) {
	for _, tc := range []struct {
		kind     Kind
		replaced bool
	}{
		{KindBlock, false},
		{KindInline, false},
		{KindText, false},
		{KindLineBreak, false},
		{KindInlineBlock, false},
		{KindSVG, true},
		{KindImage, true},
	} {
		if got := (&Box{Kind: tc.kind}).Replaced(); got != tc.replaced {
			t.Errorf("%v.Replaced() = %v", tc.kind, got)
		}
	}
	if KindImage.String() != "img" {
		t.Errorf("KindImage.String() = %q", KindImage.String())
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 8, 4))); err != nil {
		t.Fatal(err)
	}
	src, err := raster.NewSet(budget.New(t.Context(), budget.Defaults(), nil)).Add(
		"data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		img  *Img
		want string
	}{
		{&Img{}, "img\n"},
		{&Img{Src: src}, "img 8x4\n"},
	} {
		var sb strings.Builder
		if err := Dump(&sb, &Box{Kind: KindImage, Img: tc.img}); err != nil {
			t.Fatal(err)
		}
		if sb.String() != tc.want {
			t.Errorf("dump %q, want %q", sb.String(), tc.want)
		}
	}
}
