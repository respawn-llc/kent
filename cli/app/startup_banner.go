package app

import (
	_ "embed"
	"strings"

	ansi "github.com/charmbracelet/x/ansi"
)

const startupBannerHorizontalPadding = 2

//go:embed assets/builder-big-money-nw-block-full-builder-gradient.ansi
var builderStartupBannerANSI string

func renderStartupBanner(raw string) string {
	banner := strings.TrimRight(raw, "\n")
	if strings.TrimSpace(ansi.Strip(banner)) == "" {
		return ""
	}
	lines := strings.Split(banner, "\n")
	prefix := strings.Repeat(" ", startupBannerHorizontalPadding)
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, prefix+line)
	}
	return strings.Join(out, "\n")
}

func startupBannerLineCount(raw string) int {
	rendered := renderStartupBanner(raw)
	if rendered == "" {
		return 0
	}
	return renderedLineCount(rendered)
}
