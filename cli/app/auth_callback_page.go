package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"core/cli/app/internal/authui"

	tea "github.com/charmbracelet/bubbletea"
)

const authCallbackErrorDuration = 3 * time.Second

type authCallbackPageData struct {
	Theme        string
	AuthorizeURL string
	OpenErr      error
}

type authCallbackPageResult struct {
	CallbackInput string
	Canceled      bool
	Err           error
}

// Browser sign-in uses the same framed input, layout, cursor and navigation
// presentation as the rest of onboarding.
type authCallbackPageModel struct {
	*onboardingModel
	ctx          context.Context
	waitCallback func(context.Context) (authui.OAuthBrowserCallback, error)
	complete     func(context.Context, string) error
	result       authCallbackPageResult
	errorToken   uint64
}

type authCallbackPageErrorClearMsg struct{ token uint64 }
type authCallbackPageBrowserDoneMsg struct {
	callback authui.OAuthBrowserCallback
	err      error
}
type authCallbackPageCompleteDoneMsg struct {
	input string
	err   error
}

func newAuthCallbackPageModel(data authCallbackPageData) (*authCallbackPageModel, error) {
	theme, err := seedThemeSelection(data.Theme)
	if err != nil {
		return nil, err
	}
	body := "Complete sign-in in your browser, or paste the callback URL/code below."
	if data.AuthorizeURL != "" {
		body += fmt.Sprintf("\n\n[Open sign-in page](%s)", data.AuthorizeURL)
	}
	helper := "Waiting for browser callback..."
	if data.OpenErr != nil {
		helper = "Browser did not open automatically: " + data.OpenErr.Error()
	}
	model := newOnboardingFormModel(onboardingFlowState{selections: onboardingSelections{theme: theme}}, onboardingWorkflow{
		steps: []onboardingStepDefinition{{id: connectionStepBrowserAuth, build: func(*onboardingFlowState) onboardingScreen {
			return onboardingScreen{
				ID: connectionStepBrowserAuth, Kind: onboardingScreenInput, Title: "Complete sign-in",
				Body:        newStartupMarkdownRendererWithWordWrap(data.Theme).Render(body, defaultPickerWidth),
				Placeholder: "Paste callback URL or code", Helper: helper,
			}
		}}},
	})
	return &authCallbackPageModel{onboardingModel: model}, nil
}

func (m *authCallbackPageModel) Init() tea.Cmd {
	if m.waitCallback == nil {
		return nil
	}
	return func() tea.Msg {
		callback, err := m.waitCallback(m.ctx)
		return authCallbackPageBrowserDoneMsg{callback: callback, err: err}
	}
}

func (m *authCallbackPageModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEsc, tea.KeyShiftTab:
			m.result = authCallbackPageResult{Err: ErrAuthBack}
			return m, tea.Quit
		case tea.KeyCtrlC:
			m.result = authCallbackPageResult{Canceled: true}
			return m, tea.Quit
		case tea.KeyEnter, tea.KeyTab:
			input := strings.TrimSpace(m.input.Editor.Text())
			if input == "" {
				return m, m.showError("Paste the callback URL or code first.")
			}
			return m, m.completeInput(input)
		}
	case authCallbackPageErrorClearMsg:
		if msg.token == m.errorToken {
			m.currentScreen.ErrorText = ""
		}
		return m, nil
	case authCallbackPageBrowserDoneMsg:
		if msg.err != nil {
			if errors.Is(msg.err, context.Canceled) {
				m.result = authCallbackPageResult{Err: msg.err}
				return m, tea.Quit
			}
			return m, m.showError("Browser callback failed: " + msg.err.Error() + ". Paste the callback URL or code.")
		}
		return m, m.completeInput(browserCallbackInput(msg.callback))
	case authCallbackPageCompleteDoneMsg:
		if msg.err != nil {
			return m, m.showError("Invalid callback: " + msg.err.Error())
		}
		m.result = authCallbackPageResult{CallbackInput: msg.input}
		return m, tea.Quit
	}
	_, command := m.onboardingModel.Update(msg)
	return m, command
}

func (m *authCallbackPageModel) showError(text string) tea.Cmd {
	m.errorToken++
	m.currentScreen.ErrorText = text
	token := m.errorToken
	return tea.Tick(authCallbackErrorDuration, func(time.Time) tea.Msg {
		return authCallbackPageErrorClearMsg{token: token}
	})
}

func (m *authCallbackPageModel) completeInput(input string) tea.Cmd {
	complete, ctx := m.complete, m.ctx
	return func() tea.Msg {
		if complete == nil {
			return authCallbackPageCompleteDoneMsg{err: errors.New("auth callback completion is required")}
		}
		if ctx == nil {
			ctx = context.Background()
		}
		return authCallbackPageCompleteDoneMsg{input: input, err: complete(ctx, input)}
	}
}

var runAuthCallbackPage = func(ctx context.Context, data authCallbackPageData, waitCallback func(context.Context) (authui.OAuthBrowserCallback, error), complete func(context.Context, string) error) (authCallbackPageResult, error) {
	model, err := newAuthCallbackPageModel(data)
	if err != nil {
		return authCallbackPageResult{}, err
	}
	waitCtx, cancelWait := context.WithCancel(ctx)
	defer cancelWait()
	model.ctx, model.complete, model.waitCallback = waitCtx, complete, waitCallback
	cursor := newUITerminalCursorState()
	model.terminalCursor = cursor
	_, err = runStartupAlternateScreen(waitCtx, model, newUITerminalCursorWriter(os.Stdout, cursor))
	if err != nil {
		return authCallbackPageResult{}, err
	}
	return model.result, nil
}

func browserCallbackInput(callback authui.OAuthBrowserCallback) string {
	query := url.Values{"code": {callback.Code}, "state": {callback.State}}
	return query.Encode()
}
