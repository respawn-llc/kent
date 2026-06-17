package app

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var uiAnimationNow = time.Now

type frameAnimationClock struct {
	anchor time.Time
}

func (c *frameAnimationClock) Start(now time.Time) {
	if c == nil {
		return
	}
	if now.IsZero() {
		now = uiAnimationNow()
	}
	c.anchor = now
}

func (c *frameAnimationClock) Stop() {
	if c == nil {
		return
	}
	c.anchor = time.Time{}
}

func (c frameAnimationClock) Frame(now time.Time, frameCount int, frameDuration time.Duration) int {
	if frameCount <= 1 || frameDuration <= 0 {
		return 0
	}
	elapsed := c.Elapsed(now)
	if elapsed <= 0 {
		return 0
	}
	return int(elapsed/frameDuration) % frameCount
}

func (c frameAnimationClock) NextDelay(now time.Time, frameDuration time.Duration) time.Duration {
	if frameDuration <= 0 {
		return time.Millisecond
	}
	elapsed := c.Elapsed(now)
	remainder := elapsed % frameDuration
	if remainder == 0 {
		return frameDuration
	}
	return frameDuration - remainder
}

func (c frameAnimationClock) Elapsed(now time.Time) time.Duration {
	if c.anchor.IsZero() {
		return 0
	}
	if now.IsZero() {
		now = uiAnimationNow()
	}
	if now.Before(c.anchor) {
		return 0
	}
	return now.Sub(c.anchor)
}

func pendingToolSpinnerFrame(frame int) string {
	if len(pendingToolSpinner.Frames) == 0 {
		return ""
	}
	index := frame % len(pendingToolSpinner.Frames)
	if index < 0 {
		index = 0
	}
	return pendingToolSpinner.Frames[index]
}

func pendingToolSpinnerWidth() int {
	width := 0
	for _, frame := range pendingToolSpinner.Frames {
		if frameWidth := lipgloss.Width(frame); frameWidth > width {
			width = frameWidth
		}
	}
	if width < 1 {
		return 1
	}
	return width
}

func padSpinnerIndicator(indicator string) string {
	targetWidth := pendingToolSpinnerWidth()
	currentWidth := lipgloss.Width(indicator)
	if currentWidth >= targetWidth {
		return indicator
	}
	return indicator + strings.Repeat(" ", targetWidth-currentWidth)
}
