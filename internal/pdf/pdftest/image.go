package pdftest

import (
	"errors"
	"fmt"
)

// Image is an image XObject. Data holds the decoded samples of a Flate image
// (Width x Height x 1 or 3 bytes) and the raw JPEG stream of a DCT one; Mask
// holds the decoded /SMask samples, nil without one.
type Image struct {
	Width, Height int
	Gray          bool
	DCT           bool
	Data          []byte
	Mask          []byte
}

// images reads and checks the /XObject resources, which must all be images.
func (r *reader) images(res Dict) (Dict, error) {
	v, ok := res["XObject"]
	if !ok {
		return Dict{}, nil
	}
	xo, err := r.dict(v)
	if err != nil {
		return nil, err
	}
	for name, ref := range xo {
		if _, done := r.allImages[name]; done {
			continue
		}
		img, err := r.image(ref, true)
		if err != nil {
			return nil, fmt.Errorf("XObject %s: %w", name, err)
		}
		if r.allImages == nil {
			r.allImages = map[string]*Image{}
		}
		r.allImages[name] = img
	}
	return xo, nil
}

func (r *reader) image(ref any, allowMask bool) (*Image, error) {
	o, err := r.resolve(ref)
	if err != nil {
		return nil, err
	}
	st, ok := o.(*Stream)
	if !ok {
		return nil, errors.New("not a stream")
	}
	d := st.Dict
	if d["Type"] != Name("XObject") || d["Subtype"] != Name("Image") || d["BitsPerComponent"] != float64(8) {
		return nil, errors.New("not an 8-bit image XObject")
	}
	w, okw := d["Width"].(float64)
	h, okh := d["Height"].(float64)
	if !okw || !okh || w < 1 || h < 1 || w > 16384 || h > 16384 {
		return nil, errors.New("bad image size")
	}
	img := &Image{Width: int(w), Height: int(h)}
	channels := 3
	switch d["ColorSpace"] {
	case Name("DeviceGray"):
		img.Gray, channels = true, 1
	case Name("DeviceRGB"):
	default:
		return nil, fmt.Errorf("unsupported colour space %v", d["ColorSpace"])
	}
	switch d["Filter"] {
	case Name("FlateDecode"):
		if img.Data, err = inflate(st.Raw); err != nil {
			return nil, err
		}
		if len(img.Data) != img.Width*img.Height*channels {
			return nil, fmt.Errorf("%d samples, want %d", len(img.Data), img.Width*img.Height*channels)
		}
	case Name("DCTDecode"):
		img.DCT = true
		img.Data = st.Raw
		if len(st.Raw) < 4 || st.Raw[0] != 0xff || st.Raw[1] != 0xd8 || st.Raw[len(st.Raw)-2] != 0xff || st.Raw[len(st.Raw)-1] != 0xd9 {
			return nil, errors.New("DCT stream is not SOI..EOI")
		}
	default:
		return nil, fmt.Errorf("unsupported filter %v", d["Filter"])
	}
	if m, has := d["SMask"]; has {
		if !allowMask {
			return nil, errors.New("a soft mask has a soft mask")
		}
		mask, err := r.image(m, false)
		if err != nil {
			return nil, fmt.Errorf("SMask: %w", err)
		}
		if !mask.Gray || mask.DCT || mask.Width != img.Width || mask.Height != img.Height {
			return nil, errors.New("SMask is not a gray Flate image of the same size")
		}
		img.Mask = mask.Data
	}
	return img, nil
}
