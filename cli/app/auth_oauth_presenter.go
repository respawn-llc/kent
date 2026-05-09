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
	lines = append(lines, authMetaStyle(p.theme).Render("Waiting for browser callback..."))
	p.print(authMethodDisplayTitle(authMethodChoiceBrowserAuto), lines)
}

func (p interactiveAuthOAuthPresenter) ShowBrowserPaste(session oauthadapter.BrowserAuthSession, openErr error) {
	lines := p.browserLines(session, openErr)
	lines = append(lines, authMetaStyle(p.theme).Render("After sign-in, paste the full callback URL or just the code below."))
	p.print(authMethodDisplayTitle(authMethodChoiceBrowserPaste), lines)
}

func (p interactiveAuthOAuthPresenter) ShowDeviceCode(code oauthadapter.DeviceCode) {
	p.print(authMethodDisplayTitle(authMethodChoiceDevice), []string{
		authURLStyle(p.theme).Render(code.VerificationURL),
		authBodyStyle(p.theme).Render("Code: ") + authCodeStyle(p.theme).Render(code.UserCode),
		authMetaStyle(p.theme).Render("Waiting for authorization..."),
	})
}

func (p interactiveAuthOAuthPresenter) browserLines(session oauthadapter.BrowserAuthSession, openErr error) []string {
	lines := []string{authURLStyle(p.theme).Render(session.AuthorizeURL)}
	if openErr != nil {
		lines = append(lines, authMetaStyle(p.theme).Render(fmt.Sprintf("Builder could not open your browser automatically (%v). Open the URL manually.", openErr)))
	} else {
		lines = append(lines, authMetaStyle(p.theme).Render("Builder opened your default browser. If nothing appeared, open the URL manually."))
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
	out.WriteString(authTitleStyle(theme).Render(title))
	out.WriteByte('\n')
	for idx, line := range lines {
		if idx > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(line)
	}
	out.WriteString("\n\n")
	fprintf(i.stderrOrDiscard(), "%s", out.String())
}

func authTitleStyle(theme string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(uiPalette(theme).primary).Bold(true)
}

func authBodyStyle(theme string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(uiPalette(theme).foreground)
}

func authMetaStyle(theme string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(uiPalette(theme).muted).Faint(true)
}

func authPromptStyle(theme string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(uiPalette(theme).primary).Bold(true)
}

func authURLStyle(theme string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(uiPalette(theme).primary).Underline(true)
}

func authCodeStyle(theme string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(uiPalette(theme).secondary).Bold(true)
}

func (i *interactiveAuthInteractor) prompt(label string) (string, error) {
	if i.stdin == nil {
		return "", errors.New("auth prompt input is required")
	}
	fprintf(i.stderrOrDiscard(), "%s", label)
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

func fprintf(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}
