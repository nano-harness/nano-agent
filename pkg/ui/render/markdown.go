package render

import (
	"strings"

	"github.com/charmbracelet/glamour"
)

type MarkdownOptions struct {
	Width int
	Style string
}

func Markdown(input string, opts MarkdownOptions) (string, error) {
	width := opts.Width
	if width <= 0 {
		width = 100
	}
	style := strings.TrimSpace(opts.Style)
	if style == "" {
		style = "dark"
	}
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(style),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return "", err
	}
	return renderer.Render(input)
}
