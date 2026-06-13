package app

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"builder/cli/app/internal/oauthadapter"

	"github.com/charmbracelet/lipgloss"
)

type interactiveAuthOAuthPresenter struct {
	interactor *interactiveAuthInteractor
	theme      string
}

func (p interactiveAuthOAuthPresenter) ShowBrowserAuto(session oauthadapter.BrowserAuthSession, openErr error) {
	lines := p.browserLines(session, openErr)
	lines = append(lines, lipgloss.NewStyle().Foreground(uiPalette(p.theme).muted).Faint(true).Render("Waiting for browser callback..."))
	p.print(authMethodDisplayTitle(authMethodChoiceBrowserAuto), lines)
}

func (p interactiveAuthOAuthPresenter) ShowBrowserPaste(session oauthadapter.BrowserAuthSession, openErr error) {
	lines := p.browserLines(session, openErr)
	lines = append(lines, lipgloss.NewStyle().Foreground(uiPalette(p.theme).muted).Faint(true).Render("After sign-in, paste the full callback URL or just the code below."))
	p.print(authMethodDisplayTitle(authMethodChoiceBrowserPaste), lines)
}

func (p interactiveAuthOAuthPresenter) ShowDeviceCode(code oauthadapter.DeviceCode) {
	p.print(authMethodDisplayTitle(authMethodChoiceDevice), []string{
		lipgloss.NewStyle().Foreground(uiPalette(p.theme).primary).Underline(true).Render(code.VerificationURL),
		lipgloss.NewStyle().Foreground(uiPalette(p.theme).foreground).Render("Code: ") + lipgloss.NewStyle().Foreground(uiPalette(p.theme).secondary).Bold(true).Render(code.UserCode),
		lipgloss.NewStyle().Foreground(uiPalette(p.theme).muted).Faint(true).Render("Waiting for authorization..."),
	})
}

func (p interactiveAuthOAuthPresenter) browserLines(session oauthadapter.BrowserAuthSession, openErr error) []string {
	lines := []string{lipgloss.NewStyle().Foreground(uiPalette(p.theme).primary).Underline(true).Render(session.AuthorizeURL)}
	if openErr != nil {
		lines = append(lines, lipgloss.NewStyle().Foreground(uiPalette(p.theme).muted).Faint(true).Render(fmt.Sprintf("Kent could not open your browser automatically (%v). Open the URL manually.", openErr)))
	} else {
		lines = append(lines, lipgloss.NewStyle().Foreground(uiPalette(p.theme).muted).Faint(true).Render("Kent opened your default browser. If nothing appeared, open the URL manually."))
	}
	return lines
}

func (p interactiveAuthOAuthPresenter) print(title string, lines []string) {
	if p.interactor == nil {
		return
	}
	p.interactor.printAuthSection(p.theme, title, lines)
}

func (i *interactiveAuthInteractor) printAuthSection(theme, title string, lines []string) {
	if len(lines) == 0 {
		return
	}
	var out strings.Builder
	out.WriteByte('\n')
	out.WriteString(lipgloss.NewStyle().Foreground(uiPalette(theme).primary).Bold(true).Render(title))
	out.WriteByte('\n')
	for idx, line := range lines {
		if idx > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(line)
	}
	out.WriteString("\n\n")
	_, _ = fmt.Fprintf(i.stderrOrDiscard(), "%s", out.String())
}

func (i *interactiveAuthInteractor) prompt(label string) (string, error) {
	if i.stdin == nil {
		return "", errors.New("auth prompt input is required")
	}
	_, _ = fmt.Fprintf(i.stderrOrDiscard(), "%s", label)
	if i.promptReader == nil {
		i.promptReader = bufio.NewReader(i.stdin)
	}
	line, err := i.promptReader.ReadString('\n')
	if err != nil {
		if errors.Is(err, os.ErrClosed) {
			return "", err
		}
		if len(line) == 0 {
			return "", err
		}
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func (i *interactiveAuthInteractor) stderrOrDiscard() io.Writer {
	if i == nil || i.stderr == nil {
		return io.Discard
	}
	return i.stderr
}
