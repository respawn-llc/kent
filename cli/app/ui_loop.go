package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"core/cli/app/commands"
	"core/cli/tui/ongoing"
	"core/shared/apicontract"
	"core/shared/config"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	"core/shared/runtimeids"

	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

type uiProgramComposition struct {
	model                 *uiModel
	options               []tea.ProgramOption
	logger                uiLogger
	output                io.Writer
	close                 func()
	terminalOutput        *uiTerminalOutput
	nativeProgressEnabled bool
}

type uiLoopRequest struct {
	sessionLifecycle             apicontract.SessionLifecycleService
	newChat                      *chatpb.NewChatTarget
	newChatSettings              apicontract.ChatSettingsService
	newChatAuth                  apicontract.AuthBootstrapService
	newChatMutations             apicontract.ChatMutationService
	sessionControls              apicontract.RuntimeControlService
	createSession                func(context.Context, *chatpb.NewChatTarget, string) (*chatpb.ExistingSessionTarget, error)
	newChatLogin                 func(context.Context) error
	observeSession               func(context.Context, runtimeids.SessionID) (sessionLaunchPlan, *runtimeLaunchPlan, error)
	ctx                          context.Context
	wiring                       *runtimeWiring
	projectID                    string
	active                       config.Settings
	commandRegistry              *commands.Registry
	loadCommandCatalog           func(context.Context) uiCommandCatalog
	initialPrompt                string
	initialPromptHistoryRecorded bool
	initialInput                 string
	sessionTitle                 *string
	modelContractLocked          bool
	configuredModelName          *string
	statusConfig                 uiStatusConfig
	initialTransientStatus       *string
	promptCatalog                apicontract.PromptCommandCatalogService
	promptCatalogEntries         []commands.PromptCommandCatalogEntry
}

func runUILoop(request uiLoopRequest) (tea.Model, error) {
	composition, err := composeUIProgram(request, os.Stdout)
	if err != nil {
		return nil, err
	}
	return runUIProgram(composition, composition.model)
}

func terminalSupportsNativeProgress(output io.Writer) bool {
	return terminalOutputIsTerminal(output)
}

func terminalOutputIsTerminal(output io.Writer) bool {
	file, ok := output.(terminalCursorFile)
	return ok && term.IsTerminal(int(file.Fd()))
}

func runUIProgram(composition *uiProgramComposition, initialModel tea.Model) (tea.Model, error) {
	if composition == nil {
		return nil, errors.New("UI program composition is required")
	}
	if initialModel == nil {
		return nil, errors.New("UI program model is required")
	}
	defer composition.close()
	finalModel, runErr := tea.NewProgram(initialModel, composition.options...).Run()
	if composition.nativeProgressEnabled {
		if composition.model != nil {
			composition.model.cancelPendingNativeProgressWrite()
		}
		if composition.terminalOutput == nil {
			if composition.logger != nil {
				composition.logger.Logf("app.exit native_progress_reset_error=%q", "terminal output is required")
			}
		} else if _, err := composition.terminalOutput.Write([]byte(xansi.ResetProgressBar)); err != nil {
			if composition.logger != nil {
				composition.logger.Logf("app.exit native_progress_reset_error=%q", err.Error())
			}
		}
	}
	if runErr != nil {
		if composition.logger != nil {
			composition.logger.Logf("app.exit err=%q", runErr.Error())
		}
		return nil, runErr
	}
	if err := writeForcedLocalExitStatus(composition.output, finalModel); err != nil {
		if composition.logger != nil {
			composition.logger.Logf("app.exit fatal_status_error=%q", err.Error())
		}
		return finalModel, err
	}
	if composition.logger != nil {
		composition.logger.Logf("app.exit ok")
	}
	return finalModel, nil
}

func composeUIProgram(request uiLoopRequest, output io.Writer) (*uiProgramComposition, error) {
	terminalCursor := newUITerminalCursorState()
	rendererOutputGate := newUIRendererOutputGateState()
	// Preserve terminal-file identity (Fd/Read/Close) so Bubble Tea can detect
	// the real terminal and emit WindowSizeMsg.
	terminalOutput := newUITerminalOutputFile(output)
	keyboardReporting := newUITerminalKeyboardReportingOwner(
		terminalOutput.uiTerminalOutput,
		terminalOutputIsTerminal(output),
	)
	terminalOutput.keyboardReporting = keyboardReporting
	terminalCapabilities := currentTerminalCapabilities()
	ongoingSurface := ongoing.NewSurfaceWithOptions(
		terminalOutput,
		ongoing.SurfaceOptions{
			TerminalResize: terminalCapabilities.ResizePolicy,
			MarkdownLinks:  terminalCapabilities.MarkdownLinks,
		},
	)
	options := mainUIProgramOptionsWithOutput(
		request.active,
		terminalCursor,
		rendererOutputGate,
		terminalOutput,
	)
	programContext := request.ctx
	if programContext == nil {
		programContext = context.Background()
	}
	options = append(options, tea.WithContext(programContext))
	tuiLogger, _ := newRollingTUILogger(request.statusConfig.PersistenceRoot)
	uiLogger := newMultiUILogger(tuiLogger)
	runtimeClient := request.wiring.runtimeClient
	if runtimeClient == nil && request.newChat == nil {
		if tuiLogger != nil {
			_ = tuiLogger.Close()
		}
		return nil, errors.New("runtime client is required")
	}
	if runtimeClient != nil && (request.wiring.eventDispatcher == nil || request.wiring.eventDispatcher.transcriptEvents == nil) {
		if tuiLogger != nil {
			_ = tuiLogger.Close()
		}
		return nil, errors.New("transcript event stream is required")
	}
	nativeProgressEnabled := request.active.TUINativeProgressBar && terminalSupportsNativeProgress(output)
	var nativeProgressOutput *uiTerminalOutput
	if nativeProgressEnabled {
		nativeProgressOutput = terminalOutput.uiTerminalOutput
	}
	// The first renderer write occurs only after Bubble Tea owns terminal mode.
	// Queue the native-cursor signal there, so it is a real input-ready boundary.
	if err := terminalOutput.AnnounceInputReady(); err != nil {
		return nil, fmt.Errorf("announce terminal input readiness: %w", err)
	}
	sessionID := ""
	if runtimeClient != nil {
		sessionID = runtimeClient.MainView().Session.SessionId
	}

	uiOptions := []UIOption{
		WithUILogger(uiLogger),
		WithUIModelName(request.active.Model),
		WithUIConfiguredModelName(request.configuredModelName),
		WithUIThinkingLevel(request.active.ThinkingLevel),
		WithUIModelContractLocked(request.modelContractLocked),
		WithUITheme(request.active.Theme),
		WithUINativeProgressBar(nativeProgressEnabled),
		WithUITerminalOutput(nativeProgressOutput),
		WithUIMarkdownLinkPresentation(terminalCapabilities.MarkdownLinks),
		WithUIDebug(request.active.Debug),
		WithUICommandRegistry(request.commandRegistry),
		WithUIPromptCommandCatalog(request.promptCatalog),
		WithUIPromptCommandCatalogEntries(request.promptCatalogEntries),
		WithUIProcessClient(newUIProcessClientWithReads(request.projectID, request.wiring.processViews, request.wiring.processControls)),
		WithUIStartupSubmit(request.initialPrompt),
		WithUIStartupSubmitPromptHistoryRecorded(request.initialPromptHistoryRecorded),
		WithUIInitialInput(request.initialInput),
		WithUITerminalCursorState(terminalCursor),
		WithUIRendererOutputGateState(rendererOutputGate),
		WithUIOngoingSurface(ongoingSurface),
	}
	if runtimeClient != nil {
		uiOptions = append(uiOptions, WithUISessionID(sessionID), withUISessionObservation(request.wiring, sessionLaunchPlan{StatusConfig: request.statusConfig}, programContext))
	} else {
		uiOptions = append(uiOptions, WithUIWorktreeClient(request.wiring.worktrees), WithUIStatusConfig(request.statusConfig))
	}
	if request.sessionTitle != nil {
		uiOptions = append(uiOptions, WithUISessionName(request.sessionTitle))
	}
	rawModel := NewProjectedUIModel(nil, uiOptions...)
	model, ok := rawModel.(*uiModel)
	if !ok {
		if tuiLogger != nil {
			_ = tuiLogger.Close()
		}
		return nil, errors.New("projected UI model has unexpected type")
	}
	model.keyboardReporting = keyboardReporting
	model.chatMutations = request.newChatMutations
	model.chatSettings = request.newChatSettings
	model.sessionControls = request.sessionControls
	model.createSession = request.createSession
	var initialDraft *draftContent
	if target, known := model.presentationTarget.(uiKnownSessionTarget); known {
		initialDraft = &draftContent{sessionID: target.id, input: model.mainEditor.Text()}
	}
	model.drafts = newUIDraftPersistence(request.sessionLifecycle, initialDraft)
	model.observeSession = request.observeSession
	model.observationContext = programContext
	if request.loadCommandCatalog != nil {
		load := request.loadCommandCatalog
		model.startupCmds = append(model.startupCmds, func() tea.Msg {
			return initialCommandCatalogDoneMsg{owner: model, catalog: load(programContext)}
		})
	}
	if request.newChat != nil {
		model.presentationTarget = &uiNewSessionTarget{
			creation: request.newChat,
			auth:     request.newChatAuth,
			login:    request.newChatLogin,
		}
		if request.newChatSettings != nil {
			model.startupCmds = append(model.startupCmds, model.loadNewSessionSettingsCmd(nil))
		}
	}
	if request.initialTransientStatus != nil {
		model.startupCmds = append(model.startupCmds, model.showTransientStatusNotice(uiStatusNotice{
			Text:     *request.initialTransientStatus,
			Kind:     uiStatusNoticeError,
			Duration: transientStatusDuration,
		}))
	}
	return &uiProgramComposition{
		model:                 model,
		options:               options,
		logger:                uiLogger,
		output:                output,
		terminalOutput:        nativeProgressOutput,
		nativeProgressEnabled: nativeProgressEnabled,
		close: func() {
			if err := keyboardReporting.Close(); err != nil && uiLogger != nil {
				uiLogger.Logf("app.exit keyboard_reporting_reset_error=%q", err.Error())
			}
			model.Close()
			if tuiLogger != nil {
				_ = tuiLogger.Close()
			}
		},
	}, nil
}

func writeForcedLocalExitStatus(output io.Writer, finalModel tea.Model) error {
	if output == nil {
		return nil
	}
	model, ok := finalModel.(*uiModel)
	if !ok || model == nil || !model.forcedLocalExit {
		return nil
	}
	message := strings.TrimSpace(model.transientStatus)
	if message == "" {
		return nil
	}
	if _, err := fmt.Fprintln(output, message); err != nil {
		return fmt.Errorf("write fatal UI status after terminal restoration: %w", err)
	}
	return nil
}

func mainUIProgramOptionsWithOutput(
	active config.Settings,
	terminalCursor *uiTerminalCursorState,
	rendererOutputGate *uiRendererOutputGateState,
	output io.Writer,
) []tea.ProgramOption {
	options := []tea.ProgramOption{
		tea.WithFilter(terminalCursorProgramFilter(terminalCursor)),
		tea.WithReportFocus(),
	}
	rendererOutput := output
	if terminalCursor != nil {
		rendererOutput = newUITerminalCursorWriter(rendererOutput, terminalCursor)
	}
	if rendererOutputGate != nil {
		rendererOutput = newUIRendererOutputGateWriter(rendererOutput, rendererOutputGate)
	}
	if rendererOutput != nil && rendererOutput != output {
		options = append(options, tea.WithOutput(rendererOutput))
	}
	return options
}

func extractUITransition(model tea.Model) UITransition {
	if model == nil {
		return UITransition{Action: UIActionNone}
	}
	typed, ok := model.(*uiModel)
	if !ok {
		return UITransition{Action: UIActionNone}
	}
	return typed.Transition()
}
