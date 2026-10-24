package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"core/cli/app/commands"
	"core/cli/app/internal/runtimeattach"
	"core/shared/clientui"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"

	tea "github.com/charmbracelet/bubbletea"
)

func TestInputShortcutsSelectServerAdmissionIntent(t *testing.T) {
	for _, key := range []struct {
		name   string
		msg    tea.Msg
		intent runtimeinput.PendingWorkLane
	}{
		{"Enter", tea.KeyMsg{Type: tea.KeyEnter}, runtimeinput.PendingWorkLaneSteer},
		{"Tab", tea.KeyMsg{Type: tea.KeyTab}, runtimeinput.PendingWorkLaneQueue},
		{"CtrlEnter", customKeyMsg{Kind: customKeyCtrlEnter}, runtimeinput.PendingWorkLaneQueue},
	} {
		for _, input := range []struct {
			text string
			kind runtimeinput.Kind
		}{
			{"ordinary input", runtimeinput.KindText},
			{"/prompt:check arguments", runtimeinput.KindPromptCommand},
			{"$echo ready", runtimeinput.KindShell},
		} {
			t.Run(key.name+"/"+string(input.kind), func(t *testing.T) {
				client := &runtimeControlFakeClient{}
				model := newProjectedTestUIModel(client,
					WithUIPromptCommandCatalogEntries([]commands.PromptCommandCatalogEntry{{Name: "prompt:check"}}))
				model.commandRegistry = commands.NewDefaultRegistryWithPromptCatalog(model.promptCatalogEntries)
				model.setRuntimeActivityBusyForTest(true)
				testSetMainInput(model, input.text)
				_, cmd := model.Update(key.msg)
				collectCmdMessages(t, cmd)
				if client.submitCalls != 1 || client.submitRequest.Intent != key.intent ||
					client.submitRequest.Input.Kind != input.kind || client.submitRequest.QueueItemID.IsZero() {
					t.Fatalf("submission = %+v; calls = %d", client.submitRequest, client.submitCalls)
				}
				if len(model.injectedQueue) != 1 || model.injectedQueue[0].Request.QueueItemID != client.submitRequest.QueueItemID {
					t.Fatal("admission did not retain the dispatched identity")
				}
				if model.mainEditor.Text() != "" {
					t.Fatal("input remained in the composer")
				}
			})
		}
	}
}

type inputAdmissionTestClient struct {
	runtimeControlFakeClient
	rejections map[runtimeids.QueueItemID]error
}

func (c *inputAdmissionTestClient) SubmitRuntimeInput(_ context.Context, request clientui.RuntimeSubmitRequest) (clientui.UserTurnSubmission, error) {
	c.submitCalls++
	if err := c.rejections[request.QueueItemID]; err != nil {
		return clientui.UserTurnSubmission{}, err
	}
	return clientui.UserTurnSubmission{
		ResultKind: clientui.UserTurnResultKindQueued,
		Queued:     clientui.QueuedUserMessage{ID: request.QueueItemID.String(), Text: runtimeSubmitInputText(request)},
	}, nil
}

func TestInputAdmissionsSettleIndependently(t *testing.T) {
	for _, inputs := range [][2]string{
		{"first", "second"},
		{"/prompt:check first", "/prompt:check second"},
		{"  $echo first  ", "$echo second"},
	} {
		for _, failed := range []int{0, 1} {
			for _, reverse := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/failure%d/reverse%t", inputs[0], failed, reverse), func(t *testing.T) {
					disableTransientStatusClearForTest(t)
					client := &inputAdmissionTestClient{rejections: make(map[runtimeids.QueueItemID]error)}
					model := newProjectedTestUIModel(client,
						WithUIPromptCommandCatalogEntries([]commands.PromptCommandCatalogEntry{{Name: "prompt:check"}}))
					model.commandRegistry = commands.NewDefaultRegistryWithPromptCatalog(model.promptCatalogEntries)
					testSetMainInput(model, inputs[0])
					_, first := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
					testSetMainInput(model, inputs[1])
					_, second := model.Update(tea.KeyMsg{Type: tea.KeyTab})
					if len(model.injectedQueue) != 2 {
						t.Fatal("second admission was blocked by the outstanding first request")
					}
					failedID := model.injectedQueue[failed].Request.QueueItemID
					acceptedID := model.injectedQueue[1-failed].Request.QueueItemID
					rejection := errors.New("admission rejected")
					client.rejections[failedID] = rejection
					testSetMainInput(model, "new draft")
					order := []tea.Cmd{first, second}
					if reverse {
						order = []tea.Cmd{second, first}
					}
					for _, command := range order {
						for _, message := range collectCmdMessages(t, command) {
							model = updateUIModel(t, model, message)
						}
					}
					if model.mainEditor.Text() != "new draft\n\n"+inputs[failed] {
						t.Fatalf("failed input restoration = %q", model.mainEditor.Text())
					}
					if model.transientStatus != runtimeattach.FormatSubmissionError(rejection) {
						t.Fatal("failed admission did not surface its own error")
					}
					if len(model.injectedQueue) != 1 || model.injectedQueue[0].Request.QueueItemID != acceptedID ||
						client.discardQueuedCalls != 0 || client.submitCalls != 2 {
						t.Fatal("rejection discarded or resubmitted unrelated accepted input")
					}
				})
			}
		}
	}
}

func TestInputAdmissionSurvivesSameSessionHydration(t *testing.T) {
	for _, previouslyHydrated := range []bool{false, true} {
		for _, accepted := range []bool{false, true} {
			t.Run(fmt.Sprintf("hydrated=%t/accepted=%t", previouslyHydrated, accepted), func(t *testing.T) {
				disableTransientStatusClearForTest(t)
				model := newProjectedTestUIModel(&runtimeControlFakeClient{}, WithUISessionID(ongoingTestSessionID().String()))
				if previouslyHydrated {
					model.applyTranscriptHydration(ongoingHydrationMessage(1).Event.GetHydration(), runtimeTupleMergeResult{})
				}
				model.enqueueInput("invocation", runtimeinput.Text("invocation"), runtimeinput.PendingWorkLaneQueue)
				pending := model.injectedQueue[0]
				testSetMainInput(model, "new draft")
				model.applyTranscriptHydration(ongoingHydrationMessage(2).Event.GetHydration(), runtimeTupleMergeResult{})
				problem := errors.New("admission diagnostic")
				result := injectedQueueCreateDoneMsg{pending: pending}
				if accepted {
					child := runtimeids.NewSessionID().String()
					result.selectedSessionID = &child
					result.item = clientui.QueuedUserMessage{ID: pending.Request.QueueItemID.String()}
					result.diagnostic = problem
				} else {
					result.err = problem
				}
				_, command := model.inputController().handleInjectedQueueCreateDone(result)
				if accepted {
					if model.nextSessionID != *result.selectedSessionID || model.exitAction != UIActionOpenSession || command == nil {
						t.Fatal("hydration dropped accepted Session handoff or diagnostic")
					}
				} else if model.mainEditor.Text() != "new draft\n\ninvocation" ||
					model.transientStatus != runtimeattach.FormatSubmissionError(problem) {
					t.Fatal("hydration dropped rejection restoration or diagnostic")
				}
				if len(model.injectedQueue) != 0 {
					t.Fatal("settled admission remained outstanding")
				}
			})
		}
	}
}

func TestRejectedChildAdmissionRestoresInvocationWithoutChangingRunningActivity(t *testing.T) {
	disableTransientStatusClearForTest(t)
	model := newProjectedTestUIModel(&runtimeControlFakeClient{})
	model.applyTranscriptHydration(ongoingHydrationMessage(1).Event.GetHydration(), runtimeTupleMergeResult{})
	model.setRuntimeActivityBusyForTest(true)
	beforeActivity := model.activity
	model.enqueueInput("/review", runtimeinput.BuiltinCommand(runtimeinput.BuiltinPromptCommandReview, ""), runtimeinput.PendingWorkLaneQueue)
	pending := model.injectedQueue[0]
	testSetMainInput(model, "new draft")
	child := runtimeids.NewSessionID().String()
	problem := errors.New("child admission rejected")
	model.inputController().handleInjectedQueueCreateDone(injectedQueueCreateDoneMsg{
		pending: pending, selectedSessionID: &child, err: problem,
	})
	if model.mainEditor.Text() != "new draft\n\n/review" ||
		model.transientStatus != runtimeattach.FormatSubmissionError(problem) {
		t.Fatal("rejected child hid the original invocation or diagnostic")
	}
	if model.exitAction == UIActionOpenSession || model.nextSessionID == child {
		t.Fatal("rejected admission took the accepted Session handoff")
	}
	if model.activity != beforeActivity {
		t.Fatal("admission rejection replaced server-authoritative running activity")
	}
	if len(model.injectedQueue) != 0 {
		t.Fatal("rejection remained outstanding")
	}
}

func TestQueueShortcutKeepsCompactionOnDedicatedOperation(t *testing.T) {
	client := &runtimeControlFakeClient{}
	model := newProjectedTestUIModel(client)
	model.setRuntimeActivityBusyForTest(true)
	testSetMainInput(model, "/compact keep details")
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	program := tea.NewProgram(&compactionAdmissionTestModel{ui: model, command: command},
		tea.WithContext(ctx), tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutRenderer())
	if _, err := program.Run(); err != nil {
		t.Fatal(err)
	}
	if client.submitCalls != 0 || len(model.injectedQueue) != 0 ||
		client.compactRequest.Admission.CanonicalInput() != "/compact keep details" {
		t.Fatalf("Queue direct operation: input calls=%d pending=%d compaction=%+v draft=%q", client.submitCalls, len(model.injectedQueue), client.compactRequest, model.mainEditor.Text())
	}
}

type compactionAdmissionTestModel struct {
	ui      *uiModel
	command tea.Cmd
}

func (m *compactionAdmissionTestModel) Init() tea.Cmd { return m.command }
func (*compactionAdmissionTestModel) View() string    { return "" }
func (m *compactionAdmissionTestModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	next, command := m.ui.Update(message)
	m.ui = next.(*uiModel)
	if _, complete := message.(compactDoneMsg); complete {
		return m, tea.Quit
	}
	return m, command
}
