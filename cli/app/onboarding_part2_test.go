package app

import (
	"core/shared/config"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	capabilitypb "core/shared/protoapi/gen/kent/api/capability"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestOnboardingImportDiscoveryKeepsTypedInput(t *testing.T) {
	model := newOnboardingModelForWorkspace(t.TempDir(), "", testOnboardingFlowState(t, nil))
	steps := model.workflow.visibleSteps(&model.state)
	modelStepIndex := -1
	for index, step := range steps {
		if step.id == "model" {
			modelStepIndex = index
			break
		}
	}
	if modelStepIndex < 0 {
		t.Fatal("expected model input step to be visible")
	}
	model.stepIndex = modelStepIndex
	model.syncScreen(true)
	model.input.Editor.Replace(strings.NewReplacer("\r", "", "\n", "").Replace("draft-model-alias"))
	next, _ := model.Update(onboardingImportDiscoveryDoneMsg{discovery: onboardingImportDiscovery{skillSymlinkItems: map[onboardingImportProviderID][]onboardingSkillImportItem{}}})
	updated := next.(*onboardingModel)
	if updated.currentScreen.ID != "model" {
		t.Fatalf("expected to stay on model input screen, got %q", updated.currentScreen.ID)
	}
	if got := updated.input.Editor.Text(); got != "draft-model-alias" {
		t.Fatalf("expected import discovery refresh to preserve typed input, got %q", got)
	}
}

func TestOnboardingInputUsesRealAltScreenCursorWhenAvailable(t *testing.T) {
	state := newUITerminalCursorState()
	model := newOnboardingModelForWorkspace(t.TempDir(), "", testOnboardingFlowState(t, func(cfg *config.App) { cfg.Settings.Theme = "dark" }))
	model.terminalCursor = state
	model.width = 24
	model.height = 12
	model.currentScreen = onboardingScreen{Kind: onboardingScreenInput, Title: "Enter value"}
	model.input.Editor = newSingleLineEditor("alpha beta gamma")

	view := model.View()
	placement, ok := state.Snapshot()
	if !ok {
		t.Fatalf("expected real cursor placement for onboarding input, view=%q", view)
	}
	if !placement.AltScreen {
		t.Fatalf("expected alt-screen cursor placement, got %+v", placement)
	}
	if placement.CursorCol >= model.width {
		t.Fatalf("cursor col %d outside width %d", placement.CursorCol, model.width)
	}
}

func TestOnboardingInputCursorTargetsEditedCharacter(t *testing.T) {
	for _, tc := range []struct {
		name   string
		width  int
		height int
		title  string
		body   string
		value  string
		mask   rune
	}{
		{name: "connection name", width: 80, height: 24, title: "Connection", value: "chatgpt-1"},
		{name: "wrapped input", width: 12, height: 16, title: "Connection", value: "abcdefghijklmno"},
		{name: "wrapped heading and body", width: 12, height: 20, title: "A heading that wraps", body: "Some instructions that wrap too", value: "abcdef"},
		{name: "scrolled input", width: 20, height: 10, title: "Connection", body: strings.Repeat("instructions ", 20), value: "abcdef"},
		{name: "wide characters", width: 12, height: 16, title: "Connection", value: "界🙂界🙂X"},
		{name: "masked input", width: 12, height: 16, title: "Credential", value: "sensitive-value", mask: '•'},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := newOnboardingModelForWorkspace(t.TempDir(), "", testOnboardingFlowState(t, nil))
			model.terminalCursor = newUITerminalCursorState()
			model.width, model.height = tc.width, tc.height
			model.currentScreen = onboardingScreen{Kind: onboardingScreenInput, Title: tc.title, Body: tc.body}
			model.input.Editor = newSingleLineEditor(tc.value)
			model.input.Mask = tc.mask
			model.input.Editor.SetCursor(len(tc.value) - 1)
			model.Update(tea.WindowSizeMsg{Width: tc.width, Height: tc.height})

			lines := strings.Split(model.View(), "\n")
			placement, ok := model.terminalCursor.Snapshot()
			if !ok || !placement.Visible || placement.CursorRow >= len(lines) {
				t.Fatalf("cursor unavailable: %+v, view has %d rows", placement, len(lines))
			}
			got := ansi.Cut(ansi.Strip(lines[placement.CursorRow]), placement.CursorCol, placement.CursorCol+1)
			want := tc.value[len(tc.value)-1:]
			if tc.mask != 0 {
				want = string(tc.mask)
			}
			if got != want {
				t.Fatalf("cursor targets %q, want edited character %q; placement=%+v", got, want, placement)
			}
		})
	}
}

func TestOnboardingEditorFieldDeleteCurrentLineUsesAppKeyAdapter(t *testing.T) {
	cases := []struct {
		name string
		key  tea.KeyMsg
	}{
		{name: "ctrl-backspace-csi", key: tea.KeyMsg{Type: keyTypeCtrlBackspaceCSI}},
		{name: "super-backspace-csi", key: tea.KeyMsg{Type: keyTypeSuperBackspaceCSI}},
	}
	if runtime.GOOS == "darwin" {
		cases = append(cases, struct {
			name string
			key  tea.KeyMsg
		}{name: "darwin-ctrl-u", key: tea.KeyMsg{Type: tea.KeyCtrlU}})
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			model := newOnboardingModelForWorkspace(t.TempDir(), "", testOnboardingFlowState(t, func(cfg *config.App) { cfg.Settings.Theme = "dark" }))
			model.currentScreen = onboardingScreen{Kind: onboardingScreenInput, Title: "Enter value"}
			model.input.Editor = newSingleLineEditor("project name")
			model.input.Editor.SetCursor(byteOffsetForRuneCursor(model.input.Editor.Text(), len([]rune("project"))))

			next, _ := model.Update(tt.key)
			updated := next.(*onboardingModel)
			if got := updated.input.Editor.Text(); got != "" {
				t.Fatalf("value after delete-current-line key = %q, want empty", got)
			}
			if got := runeOffsetForByteCursor(updated.input.Editor.Text(), updated.input.Editor.Cursor()); got != 0 {
				t.Fatalf("cursor after delete-current-line key = %d, want 0", got)
			}
		})
	}
}

func newOnboardingModelAtModelInput(t *testing.T) *onboardingModel {
	t.Helper()
	model := newOnboardingModelForWorkspace(t.TempDir(), "", testOnboardingFlowState(t, func(cfg *config.App) { cfg.Settings.Theme = "dark" }))
	steps := model.workflow.visibleSteps(&model.state)
	modelStepIndex := -1
	for index, step := range steps {
		if step.id == "model" {
			modelStepIndex = index
			break
		}
	}
	if modelStepIndex < 1 {
		t.Fatalf("model input step index = %d, want non-initial input step", modelStepIndex)
	}
	model.stepIndex = modelStepIndex
	model.syncScreen(true)
	if model.currentScreen.Kind != onboardingScreenInput {
		t.Fatalf("screen kind = %q, want input", model.currentScreen.Kind)
	}
	return model
}

func TestOnboardingInputWordNavigationStaysInField(t *testing.T) {
	cases := []struct {
		name          string
		key           tea.KeyMsg
		initialCursor func(string) int
		wantCursor    int
	}{
		{
			name:          "alt-left",
			key:           tea.KeyMsg{Type: tea.KeyLeft, Alt: true},
			initialCursor: func(text string) int { return len([]rune(text)) },
			wantCursor:    len([]rune("alpha beta ")),
		},
		{
			name:          "alt-right",
			key:           tea.KeyMsg{Type: tea.KeyRight, Alt: true},
			initialCursor: func(string) int { return 0 },
			wantCursor:    len([]rune("alpha")),
		},
		{
			name:          "alt-b",
			key:           tea.KeyMsg{Type: tea.KeyRunes, Alt: true, Runes: []rune{'b'}},
			initialCursor: func(text string) int { return len([]rune(text)) },
			wantCursor:    len([]rune("alpha beta ")),
		},
		{
			name:          "alt-f",
			key:           tea.KeyMsg{Type: tea.KeyRunes, Alt: true, Runes: []rune{'f'}},
			initialCursor: func(string) int { return 0 },
			wantCursor:    len([]rune("alpha")),
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			model := newOnboardingModelAtModelInput(t)
			const input = "alpha beta gamma"
			model.input.Editor = newSingleLineEditor(input)
			initialCursor := tt.initialCursor(input)
			model.input.Editor.SetCursor(byteOffsetForRuneCursor(input, initialCursor))
			screenID := model.currentScreen.ID

			next, _ := model.Update(tt.key)
			updated := next.(*onboardingModel)
			if got := updated.currentScreen.ID; got != screenID {
				t.Fatalf("screen after %s = %q, want %q", tt.name, got, screenID)
			}
			if got := updated.input.Editor.Text(); got != input {
				t.Fatalf("input after %s = %q, want %q", tt.name, got, input)
			}
			if got, want := runeOffsetForByteCursor(updated.input.Editor.Text(), updated.input.Editor.Cursor()), tt.wantCursor; got != want {
				t.Fatalf("cursor after %s = %d, want %d", tt.name, got, want)
			}
		})
	}
}

func TestOnboardingInputPlainArrowsMoveCursorWithoutChangingStep(t *testing.T) {
	cases := []struct {
		name   string
		key    tea.KeyMsg
		cursor int
	}{
		{name: "left", key: tea.KeyMsg{Type: tea.KeyLeft}, cursor: 0},
		{name: "right", key: tea.KeyMsg{Type: tea.KeyRight}, cursor: 2},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			model := newOnboardingModelAtModelInput(t)
			initialStepIndex := model.stepIndex
			const input = "abc"
			model.input.Editor = newSingleLineEditor(input)
			model.input.Editor.SetCursor(1)

			next, _ := model.Update(tt.key)
			updated := next.(*onboardingModel)
			if got, want := updated.stepIndex, initialStepIndex; got != want {
				t.Fatalf("step index after plain %s = %d, want %d", tt.name, got, want)
			}
			if got := updated.input.Editor.Cursor(); got != tt.cursor {
				t.Fatalf("cursor after %s = %d, want %d", tt.name, got, tt.cursor)
			}
			if got := updated.input.Editor.Text(); got != input {
				t.Fatalf("input after %s = %q, want %q", tt.name, got, input)
			}
		})
	}
}

func TestOnboardingInputVerticalArrowsMoveWithinWrappedField(t *testing.T) {
	for _, mask := range []rune{0, '*'} {
		model := newOnboardingModelAtModelInput(t)
		model.width = 8
		model.height = 24
		model.input.Editor = newSingleLineEditor("abcdefghijklmno")
		model.input.Mask = mask
		model.input.Editor.SetCursor(12)
		screenID := model.currentScreen.ID
		for _, step := range []struct {
			key    tea.KeyType
			cursor int
		}{
			{tea.KeyUp, 4},
			{tea.KeyDown, 12},
			{tea.KeyUp, 4},
			{tea.KeyUp, 0},
		} {
			model.Update(tea.KeyMsg{Type: step.key})
			if model.currentScreen.ID != screenID {
				t.Fatalf("%v left the input screen", step.key)
			}
			if got := model.input.Editor.Cursor(); got != step.cursor {
				t.Fatalf("mask=%q key=%v cursor=%d, want %d", mask, step.key, got, step.cursor)
			}
		}
	}
}

func TestOnboardingTabNavigation(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyTab, tea.KeyEnter} {
		model := newOnboardingModelAtModelInput(t)
		inputScreen := model.currentScreen.ID
		model.Update(tea.KeyMsg{Type: key})
		if model.currentScreen.ID == inputScreen {
			t.Fatalf("%v did not advance from input", key)
		}
		model.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
		if model.currentScreen.ID != inputScreen {
			t.Fatalf("shift+tab returned to %q, want %q", model.currentScreen.ID, inputScreen)
		}
		model.input.Editor.Replace("")
		model.Update(tea.KeyMsg{Type: key})
		if model.currentScreen.ID != inputScreen || model.currentScreen.ErrorText == "" {
			t.Fatalf("%v did not block invalid input: %+v", key, model.currentScreen)
		}
	}
}

func TestOnboardingConnectionTabNavigation(t *testing.T) {
	_, model, err := newConnectionFormModel("dark", &authpb.ConnectionCatalog{}, true)
	if err != nil {
		t.Fatal(err)
	}
	model.Update(tea.KeyMsg{Type: tea.KeyTab})
	if model.currentScreen.ID != connectionStepTemplate {
		t.Fatalf("tab from theme opened %q", model.currentScreen.ID)
	}
	for _, key := range []tea.KeyType{tea.KeyLeft, tea.KeyRight} {
		model.Update(tea.KeyMsg{Type: key})
		if model.currentScreen.ID != connectionStepTemplate {
			t.Fatalf("%v switched a choice screen", key)
		}
	}
	model.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if model.currentScreen.ID != onboardingStepTheme {
		t.Fatalf("shift+tab from provider choice opened %q", model.currentScreen.ID)
	}
	model.Update(tea.KeyMsg{Type: tea.KeyTab})
	for index, option := range model.currentScreen.Options {
		if option.ID == string(connectionTemplateAPI) {
			model.cursor = index
		}
	}
	model.Update(tea.KeyMsg{Type: tea.KeyTab})
	if model.currentScreen.ID != connectionStepID {
		t.Fatalf("tab from provider choice opened %q", model.currentScreen.ID)
	}
	model.Update(tea.KeyMsg{Type: tea.KeyTab})
	if model.currentScreen.ID != connectionStepEndpoint {
		t.Fatalf("tab from provider input opened %q", model.currentScreen.ID)
	}
	model.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if model.currentScreen.ID != connectionStepID {
		t.Fatalf("shift+tab from endpoint opened %q", model.currentScreen.ID)
	}
}

func TestOnboardingSpinnerSchedulingTracksScreenState(t *testing.T) {
	tests := []struct {
		name        string
		configure   func(*onboardingModel)
		reschedules bool
	}{
		{name: "idle", configure: func(model *onboardingModel) {
			model.state.imports.pending = false
			model.syncScreen(true)
		}},
		{name: "loading", configure: func(model *onboardingModel) {
			model.currentScreen = onboardingScreen{Kind: onboardingScreenLoading}
		}, reschedules: true},
		{name: "finalizing", configure: func(model *onboardingModel) {
			model.state.imports.pending = false
			model.syncScreen(true)
			model.finalizing = true
		}, reschedules: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := newOnboardingModelForWorkspace(t.TempDir(), "", testOnboardingFlowState(t, func(cfg *config.App) { cfg.Settings.Theme = "dark" }))
			tt.configure(model)
			next, cmd := model.Update(onboardingSpinnerTickMsg{at: model.spinnerClock.anchor.Add(spinnerTickInterval)})
			if next.(*onboardingModel).spinnerFrame == 0 {
				t.Fatal("spinner tick did not advance")
			}
			if got := cmd != nil; got != tt.reschedules {
				t.Fatalf("spinner rescheduled = %t, want %t", got, tt.reschedules)
			}
		})
	}
}

func TestApplyOnboardingModelUpdatesKnownContextWindow(t *testing.T) {
	state := testOnboardingFlowStatePtr(t, func(cfg *config.App) {
		cfg.Settings.Model = "gpt-6-luna"
	})
	if err := state.submitPrimaryModel("gpt-6-sol"); err != nil {
		t.Fatalf("apply onboarding model: %v", err)
	}
	if state.selections.contextWindow.kind != onboardingContextDefault {
		t.Fatalf("expected gpt-6-sol default context window, got %+v", state.selections.contextWindow)
	}
	if state.selections.reviewerModelValue() != "gpt-6-sol" {
		t.Fatalf("expected reviewer model to follow main model, got %q", state.selections.reviewerModelValue())
	}
	if state.selections.reviewerThinkingValue() != "medium" {
		t.Fatalf("expected reviewer thinking to follow main thinking, got %q", state.selections.reviewerThinkingValue())
	}
}

func TestApplyOnboardingModelResetsUnknownModelContextWindowToBaseline(t *testing.T) {
	state := testOnboardingFlowStatePtr(t, nil)
	if err := state.submitPrimaryModel("my-team-alias"); err != nil {
		t.Fatalf("apply onboarding model: %v", err)
	}
	if state.selections.contextWindow.kind != onboardingContextCustom || state.selections.contextWindow.tokens != 272_000 {
		t.Fatalf("expected unknown model context window to reset to onboarding baseline, got %+v", state.selections.contextWindow)
	}
}

func TestMainThinkingChoiceSynchronizesReviewerThinking(t *testing.T) {
	state := testOnboardingFlowStatePtr(t, nil)
	if err := findWorkflowStep(t, state, "thinking").apply(state, "high"); err != nil {
		t.Fatalf("apply thinking choice: %v", err)
	}
	if state.selections.reviewerThinkingValue() != "high" {
		t.Fatalf("expected reviewer thinking to track updated main thinking, got %q", state.selections.reviewerThinkingValue())
	}
}

func TestMainThinkingChoicePreservesCustomReviewerThinking(t *testing.T) {
	state := testOnboardingFlowStatePtr(t, func(cfg *config.App) {
		cfg.Settings.Reviewer.ThinkingLevel = "low"
		cfg.Source.Sources["reviewer.thinking_level"] = config.Origin{Kind: config.SourceInput, Property: config.PropertyAddress{Key: "reviewer.thinking_level"}}

	})
	if err := findWorkflowStep(t, state, "thinking").apply(state, "high"); err != nil {
		t.Fatalf("apply thinking choice: %v", err)
	}
	if state.selections.reviewerThinkingValue() != "low" {
		t.Fatalf("expected custom reviewer thinking to be preserved, got %q", state.selections.reviewerThinkingValue())
	}
}

func TestApplyOnboardingModelPreservesCustomReviewerOverrides(t *testing.T) {
	state := testOnboardingFlowStatePtr(t, func(cfg *config.App) {
		cfg.Settings.Reviewer.Model = "gpt-6-astra"
		cfg.Settings.Reviewer.ThinkingLevel = "low"
		cfg.Source.Sources["reviewer.model"] = config.Origin{Kind: config.SourceInput, Property: config.PropertyAddress{Key: "reviewer.model"}}

		cfg.Source.Sources["reviewer.thinking_level"] = config.Origin{Kind: config.SourceInput, Property: config.PropertyAddress{Key: "reviewer.thinking_level"}}

	})
	if err := state.submitPrimaryModel("gpt-6-luna"); err != nil {
		t.Fatalf("apply onboarding model: %v", err)
	}
	if state.selections.reviewerModelValue() != "gpt-6-astra" {
		t.Fatalf("expected custom reviewer model to be preserved, got %q", state.selections.reviewerModelValue())
	}
	if state.selections.reviewerThinkingValue() != "low" {
		t.Fatalf("expected custom reviewer thinking to be preserved, got %q", state.selections.reviewerThinkingValue())
	}
}

func findWorkflowStep(t *testing.T, state *onboardingFlowState, id onboardingStepID) onboardingStepDefinition {
	t.Helper()
	if len(state.facts.Models.KnownModels) == 0 && !state.facts.Models.UnknownFallback.SupportsThinking {
		state.facts = testOnboardingCapabilityFacts()
	}
	for _, step := range newOnboardingWorkflow(state).visibleSteps(state) {
		if step.id == id {
			return step
		}
	}
	t.Fatalf("expected workflow step %q", id)
	return onboardingStepDefinition{}
}

func testOnboardingCapabilityFacts() *capabilitypb.Facts {
	contextWindow := uint32(272_000)
	models := []*capabilitypb.ModelFact{
		{ModelId: ptrString("gpt-6.1-sol"), Known: true, ContextWindowTokens: &contextWindow, LargeWindow: &capabilitypb.ModelLargeWindowFact{Tokens: 1_050_000}, SupportsThinking: true, SupportedThinkingLevels: []string{"low", "medium", "high", "xhigh", "max"}, Verbosity: &capabilitypb.ModelVerbosityFact{Supported: true, Source: "catalog", Levels: []string{"low", "medium", "high"}}},
		{ModelId: ptrString("gpt-6-sol"), Known: true, ContextWindowTokens: &contextWindow, LargeWindow: &capabilitypb.ModelLargeWindowFact{Tokens: 400_000}, SupportsThinking: true, SupportedThinkingLevels: []string{"low", "medium", "high"}, Verbosity: &capabilitypb.ModelVerbosityFact{Supported: true, Source: "catalog", Levels: []string{"low", "medium", "high"}}},
		{ModelId: ptrString("gpt-6-luna"), Known: true, ContextWindowTokens: &contextWindow, SupportsThinking: true, SupportedThinkingLevels: []string{"low", "medium", "high"}, Verbosity: &capabilitypb.ModelVerbosityFact{Supported: true, Source: "catalog", Levels: []string{"low", "medium", "high"}}},
		{ModelId: ptrString("gpt-6-astra"), Known: true, ContextWindowTokens: &contextWindow, SupportsThinking: true, SupportedThinkingLevels: []string{"low", "medium", "high"}, Verbosity: &capabilitypb.ModelVerbosityFact{Supported: true, Source: "catalog", Levels: []string{"low", "medium", "high"}}},
	}
	facts := emptyOnboardingCapabilityFacts()
	facts.Models = &capabilitypb.ModelFacts{
		KnownModels: models,
		UnknownFallback: &capabilitypb.ModelFact{
			Known:                   false,
			SupportsThinking:        true,
			SupportedThinkingLevels: []string{"low", "medium", "high"},
			Verbosity:               &capabilitypb.ModelVerbosityFact{Supported: true, Source: "catalog", Levels: []string{"low", "medium", "high"}},
		},
	}
	facts.Providers.CurrentEffective = &capabilitypb.ProviderFact{
		LlmProviderId:            "openai",
		Role:                     "main",
		SupportsNativeCompaction: true,
	}
	return facts
}

func ptrString(value string) *string {
	return &value
}

func workflowIncludesStep(steps []onboardingStepDefinition, id onboardingStepID) bool {
	for _, step := range steps {
		if step.id == id {
			return true
		}
	}
	return false
}
