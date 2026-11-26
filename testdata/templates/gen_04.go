//go:build ignore

// Command gen_04 writes testdata/templates/04-images.html from
// testdata/templates/04-images.skeleton.html: it replaces the {{NAME}} markers
// with data: URLs of deterministic generated PNGs and of two files of
// testdata/images. Run it from the repository root:
//
//	go run testdata/templates/gen_04.go
package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
)

func enc(img image.Image) string {
	var b bytes.Buffer
	e := png.Encoder{CompressionLevel: png.BestCompression}
	if err := e.Encode(&b, img); err != nil {
		panic(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes())
}

func logo() string {
	img := image.NewNRGBA(image.Rect(0, 0, 240, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 240; x++ {
			switch {
			case x < 64 && (x-32)*(x-32)+(y-32)*(y-32) < 30*30:
				img.SetNRGBA(x, y, color.NRGBA{234, 91, 12, 255})
			case x >= 80 && x < 232 && y%16 < 5 && y > 12 && y < 52:
				img.SetNRGBA(x, y, color.NRGBA{13, 17, 23, uint8(255 - (x-80)/2)})
			}
		}
	}
	return enc(img)
}

func screenshot(w, h int, seed int) string {
	pal := color.Palette{color.NRGBA{255, 255, 255, 255}, color.NRGBA{13, 17, 23, 255}, color.NRGBA{234, 91, 12, 255}, color.NRGBA{208, 215, 222, 255}, color.NRGBA{26, 127, 55, 255}, color.NRGBA{209, 36, 47, 255}}
	img := image.NewPaletted(image.Rect(0, 0, w, h), pal)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := uint8(0)
			switch {
			case y < 28:
				c = 1
			case x < 120 && y%24 < 3:
				c = 3
			case x >= 140 && (y/24+seed)%3 == 0 && y%24 > 6 && y%24 < 16 && x < w-20-(y*7+seed*13)%90:
				c = uint8(2 + (y/24+seed)%4)
			case x == 120:
				c = 3
			}
			img.SetColorIndex(x, y, c)
		}
	}
	return enc(img)
}

func chart() string {
	img := image.NewNRGBA(image.Rect(0, 0, 640, 320))
	for x := 0; x < 640; x++ {
		for y := 0; y < 320; y++ {
			bar := x / 80
			top := 40 + (bar*53)%200
			switch {
			case x%80 > 12 && x%80 < 68 && y > top && y < 300:
				img.SetNRGBA(x, y, color.NRGBA{234, 91, 12, uint8(90 + bar*20)})
			case y == 300 || x == 4:
				img.SetNRGBA(x, y, color.NRGBA{13, 17, 23, 255})
			}
		}
	}
	return enc(img)
}

func thumb(i int) string {
	img := image.NewNRGBA(image.Rect(0, 0, 48, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 48; x++ {
			img.SetNRGBA(x, y, color.NRGBA{uint8(x*5 + i*40), uint8(y*5 + i*20), uint8(120 + i*30), 255})
		}
	}
	return enc(img)
}

func file(name string) string {
	b, err := os.ReadFile("testdata/images/" + name)
	if err != nil {
		panic(err)
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(b)
}

func main() {
	sk, err := os.ReadFile("testdata/templates/04-images.skeleton.html")
	if err != nil {
		panic(err)
	}
	s := string(sk)
	rep := map[string]string{
		"{{LOGO}}": logo(), "{{SHOT1}}": screenshot(720, 400, 0), "{{SHOT2}}": screenshot(560, 300, 1),
		"{{CHART}}": chart(), "{{CMYK}}": file("video-001.cmyk.jpeg"), "{{PHOTO}}": file("video-001.jpeg"),
	}
	for i := 0; i < 4; i++ {
		rep[fmt.Sprintf("{{T%d}}", i)] = thumb(i)
	}
	for k, v := range rep {
		s = strings.ReplaceAll(s, k, v)
	}
	if err := os.WriteFile("testdata/templates/04-images.html", []byte(s), 0o600); err != nil {
		panic(err)
	}
}
