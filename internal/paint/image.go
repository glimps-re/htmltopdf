package paint

import (
	"errors"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/pdf"
	"github.com/glimps-re/htmltopdf/internal/raster"
)

// imageState is what painting a Source produced: its XObject, or a failure that
// is not retried.
type imageState struct {
	id     pdf.ImageID
	failed bool
}

// image draws it: the first draw of its Source decodes it and writes its
// XObject, then drops its pixels, so one image at most is decoded at a time; a
// Source that failed to decode draws nothing.
func (s *pageState) image(it display.Image) {
	if it.Src == nil || !(it.Opacity > 0) || !positive(it.Rect.W, it.Rect.H) {
		return
	}
	// An image beyond ±MaxCoordPt is clamped like any item, so cm squashes it;
	// layout bounds sizes far below that.
	r, ok := s.rect(it.Rect)
	if !ok || !positive(r.w, r.h) {
		return
	}
	id, ok, err := s.p.imageID(it.Src)
	if err != nil {
		s.err = err
		return
	}
	if !ok {
		return
	}
	s.c.Save()
	// The fill opacity (ca) also applies to image XObjects. It is set even when
	// opaque, as the item before may have left another one.
	s.c.FillColor(pdf.Color{A: it.Opacity})
	s.c.DrawImage(id, r.x, r.y, r.w, r.h)
	s.c.Restore()
}

// imageID returns the XObject of src, writing it on first use. ok is false for
// an image rejected now or before: it is warned once and never decoded again.
// The error is a budget, context or writer error, which fails the render.
func (p *Painter) imageID(src *raster.Source) (pdf.ImageID, bool, error) {
	if st, ok := p.images[src]; ok {
		return st.id, !st.failed, nil
	}
	id, err := p.addImage(src)
	if err != nil {
		reason, ok := rejectReason(err)
		if !ok {
			return pdf.ImageID{}, false, err
		}
		p.images[src] = imageState{failed: true}
		p.b.Warn(budget.WarnImageRejected, reason)
		return pdf.ImageID{}, false, nil
	}
	p.images[src] = imageState{id: id}
	return id, true, nil
}

// rejectReason is the warning detail of an error of addImage that rejects the
// image; ok is false for an error that fails the render.
func rejectReason(err error) (reason string, ok bool) {
	var rej *raster.RejectError
	switch {
	case errors.As(err, &rej):
		return rej.Reason.String(), true
	case errors.Is(err, pdf.ErrImage):
		// The writer's own pixel cap refuses the image before writing a byte,
		// which leaves the writer usable.
		return raster.ReasonSize.String(), true
	case errors.Is(err, pdf.ErrTooManyImages):
		// Refused before writing a byte too; raster caps the count first today.
		return raster.ReasonCount.String(), true
	}
	return "", false
}

// addImage decodes src and writes it as an XObject. The decoded pixels are
// dropped on return, so one image at most is decoded at a time.
func (p *Painter) addImage(src *raster.Source) (pdf.ImageID, error) {
	d, err := raster.Decode(src, p.b)
	if err != nil {
		return pdf.ImageID{}, err
	}
	img := pdf.Image{Width: d.Width, Height: d.Height, Gray: d.Gray}
	if src.Format == raster.JPEG {
		// ImageDCT writes Data's bytes as given: they must come from WriteJPEG,
		// never from the source, so no attacker byte reaches the output.
		img.Filter, img.Data = pdf.ImageDCT, d.WriteJPEG
	} else {
		img.Filter, img.Data = pdf.ImageFlate, d.WriteSamples
	}
	if d.Alpha {
		img.Alpha = d.WriteAlpha
	}
	return p.w.AddImage(img)
}
