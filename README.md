# htmltopdf

A pure-Go engine that renders HTML and CSS to PDF, with no browser and no external
binary. API reference: `go doc github.com/glimps-re/htmltopdf/pkg/htmltopdf`;
supported CSS: [docs/css-support.md](docs/css-support.md).

## Example

```go
conv, err := htmltopdf.New(htmltopdf.Options{
	ExtraCSS: "@page { size: A4; margin: 20mm }",
})
if err != nil {
	log.Fatal(err)
}
var pdf bytes.Buffer
res, err := conv.Render(context.Background(),
	strings.NewReader("<h1>Report</h1><p>Hello, <b>world</b>.</p>"), &pdf)
if err != nil {
	log.Fatal(err)
}
fmt.Println(res.Pages, "page")
```

## CI

`.github/workflows/go.yml` is the source of truth for triggers and versions. CI runs
`go test ./...` without the race detector and needs no secret. The race suite
(`go test -race ./...`) is a local gate: it takes minutes per package, so give it
`-timeout 30m`.
