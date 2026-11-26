# Test images

Real-world test images for the raster layer (design M6 §9): the JPEG and PNG images of Go's
`image` package, copied **unmodified** from the local Go 1.27.4 installation
(`$GOROOT/src/image/testdata`, `GOROOT` = `/home/user/sdk/go1.27.4`), never downloaded.
None is malware. They are BSD-3-Clause (The Go Authors, `LICENSE-Go`), used by tests only and
never embedded in the library (see `LICENSES.md`, test-only section).

Every other image is generated, not committed: `internal/testcorpus/images.go` builds the
hostile and boundary images and one image of each PNG variant (user decision D-047), with
their SHA-256 pinned in `images_sums_test.go`.

`video-*.png` are the decoded twins of the `.jpeg` files of the same name (within 8/255 per
channel of Go's decoder output, the tolerance of Go's own `decode_test.go`), usable for
colour checks. The two `progressive.truncated` files decode without error in Go (the
partial image is returned).

`TestImageFilesMatchReadme` checks every hash below and that no file exceeds 64 KiB.

| File | SHA-256 |
|---|---|
| `video-001.cmyk.jpeg` | `323ac0779011f0e5581af8622898d7300d65746061292718ccb6c00685e5e0b5` |
| `video-001.cmyk.png` | `2609252996b30a7bf212afb2ebcd36b96a6efffb0df72436c1d8e7aa0a8daab2` |
| `video-001.jpeg` | `cf03dbf986e29acf2f1ad7a0628667dc2c48f0b16ea14127f731819c7d2037d3` |
| `video-001.png` | `e3ad8f29d2adf538bc077fcdb6528d76c36e70b238ee32b5982273eeb65ddc36` |
| `video-001.progressive.jpeg` | `25bf79171c63cb86856a922450750dcba3a9b93c5f512a3a9a2219af5726c6c2` |
| `video-001.progressive.truncated.jpeg` | `5a5003b7e17dec1e73552fec006dcc3367b94598e53315f78536bc877151339c` |
| `video-001.progressive.truncated.png` | `ae19fa6f83a6213002f1a5d5e874a9464a775d50890e0f3431dc870a8598cb58` |
| `video-001.q50.410.jpeg` | `1fb2b10f3bdfc6156b651b4fc24e44a9e434a7a6d49cb5962b92af59f6d4e412` |
| `video-001.q50.420.progressive.jpeg` | `d9b218232d5ec6f2c3f4dfcd058eaf87ba7f8e37cb6899c82f5253ed12a13053` |
| `video-001.rgb.jpeg` | `6cf420c1da0cbcb9b81fd318bed089c1e4262a4092c0261aed4fdcb8b5c1dd83` |
| `video-001.rgb.png` | `f9d8d5b5880c0985a830992356cb9d4bc3ca1963fd9c5edf8161faefcaaa4ac9` |
| `video-005.gray.jpeg` | `29e745eabbc435987127ae43f2e4af059f844d017224bf54f02d750073cd912e` |
| `video-005.gray.png` | `012957b0febfa58d128b89f934cb3d22a36feded8bf271b36db40c447ad51cd3` |
| `video-005.gray.q50.progressive.jpeg` | `a22ad1a99e6f3d7778cc0dcf0d146de7e2cb8bfc7d8c793f4b7d8eb316f29e5c` |

All files above are copied from `$GOROOT/src/image/testdata/<file>`.

## Coverage

| Variant | Where |
|---|---|
| JPEG baseline YCbCr / RGB / Gray | `video-001.jpeg`, `video-001.rgb.jpeg`, `video-005.gray.jpeg` |
| JPEG progressive (full, truncated, gray, 4:2:0) | `video-001.progressive.jpeg`, `video-001.progressive.truncated.jpeg`, `video-005.gray.q50.progressive.jpeg`, `video-001.q50.420.progressive.jpeg` |
| JPEG 4:1:0 subsampling | `video-001.q50.410.jpeg` |
| JPEG CMYK (Adobe) | `video-001.cmyk.jpeg` (twin `video-001.cmyk.png`) |
| PNG gray 1/2/4/8/16 bit, truecolour 8/16, palette 1/2/4/8, gray+alpha 8/16, RGBA 8/16, filters 0-4 | generated `png_gray*`, `png_rgb*`, `png_pal*`, `png_graya*`, `png_rgba*` (`_33x31`) |
| PNG tRNS on palette, gray and truecolour 16 | generated `png_pal8_trns_33x31`, `png_gray2_trns_33x31`, `png_rgb16_trns_33x31` |
| PNG interlaced (Adam7): palette 4-bit 31x31, RGBA 16-bit 37x23 | generated `png_pal4_adam7_31x31`, `png_rgba16_37x23_adam7` (twin `png_rgba16_37x23`) |
