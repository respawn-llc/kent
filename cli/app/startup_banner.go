package app

import (
	_ "embed"
	"strings"

	ansi "github.com/charmbracelet/x/ansi"
)

const startupBannerHorizontalPadding = 1

//go:embed assets/banner.ansi
var startupBannerANSI string

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
	return "\n" + strings.Join(out, "\n")
}
