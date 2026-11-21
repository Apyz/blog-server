package render

import (
	"bytes"
	"html/template"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

type MarkdownRenderer struct {
	gm goldmark.Markdown
}

func NewMarkdownRenderer() *MarkdownRenderer {
	md := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.Typographer,
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			html.WithHardWraps(),
			html.WithXHTML(),
			html.WithUnsafe(), // Allows embedded HTML (videos, iframes, badges)
		),
	)

	return &MarkdownRenderer{gm: md}
}

// ToHTML converts Markdown source text into safe template.HTML.
func (r *MarkdownRenderer) ToHTML(source string) (template.HTML, error) {
	var buf bytes.Buffer
	if err := r.gm.Convert([]byte(source), &buf); err != nil {
		return "", err
	}
	return template.HTML(buf.String()), nil
}
