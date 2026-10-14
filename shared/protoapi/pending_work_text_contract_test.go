package protoapi_test

import (
	"testing"

	"core/shared/protoapi"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	runpromptpb "core/shared/protoapi/gen/kent/api/run_prompt"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
)

func TestPendingWorkMessageFEFFBlankContract(t *testing.T) {
	for _, tc := range []struct {
		text  string
		valid bool
	}{
		{"ordinary 日本語 text", true},
		{"\uFEFFsubstantive\uFEFF", true},
		{"\uFEFF \t\u0085", false},
		{"\u0085", false},
	} {
		err := protoapi.Validate(&runtimepb.PendingWorkMessage{Text: tc.text})
		if (err == nil) != tc.valid {
			t.Fatalf("text %q validation = %v, want valid=%t", tc.text, err, tc.valid)
		}
	}
}

func TestWorktreeEnterSelectorAcceptsNormalizableFEFFButRejectsBlank(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	request := &worktreepb.EnterRequest{
		OperationId:     id,
		SessionId:       &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: id}},
		TargetWorkspace: &worktreepb.TransitionWorkspace{WorkspaceId: "workspace", WorkspaceRoot: "/workspace"},
		Selector:        "\uFEFFfeature\uFEFF branch\uFEFF",
	}
	if err := protoapi.Validate(request); err != nil {
		t.Fatalf("normalizable selector rejected: %v", err)
	}
	request.Selector = "\uFEFF \t"
	if err := protoapi.Validate(request); err == nil {
		t.Fatal("normalized-blank selector accepted")
	}
}

func TestRunPromptFEFFBlankRestrictionDoesNotAffectCallerBearingPrompt(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	request := &runpromptpb.Request{
		Intent: &sessionlaunchpb.SessionLaunchRequestIntent{Intent: &sessionlaunchpb.SessionLaunchRequestIntent_OpenExistingSessionId{
			OpenExistingSessionId: &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: id}},
		}},
		Prompt:          "\uFEFF \t",
		CallerSessionId: &id,
	}
	if err := protoapi.Validate(request); err != nil {
		t.Fatalf("caller-bearing prompt rejected: %v", err)
	}
	request.CallerSessionId = nil
	if err := protoapi.Validate(request); err == nil {
		t.Fatal("caller-absent FEFF-only prompt accepted")
	}
}

func TestPendingWorkCompactionGuidanceCanonicalSpacing(t *testing.T) {
	for _, tc := range []struct {
		guidance string
		valid    bool
	}{
		{"keep 日本語 details", true},
		{"keep  details", false},
		{"keep\uFEFFdetails", false},
		{"keep\u0085details", false},
	} {
		err := protoapi.Validate(&runtimepb.ManualCompactionAdmission{Guidance: &tc.guidance})
		if (err == nil) != tc.valid {
			t.Fatalf("guidance %q validation = %v, want valid=%t", tc.guidance, err, tc.valid)
		}
	}
}

func TestPendingWorkSelectorCanonicalSpacing(t *testing.T) {
	for _, tc := range []struct {
		selector string
		valid    bool
	}{
		{"feature 日本語", true},
		{"feature  branch", false},
		{"feature\uFEFFbranch", false},
		{"feature\u0085branch", false},
	} {
		t.Run(tc.selector, func(t *testing.T) {
			err := protoapi.Validate(&runtimepb.PendingWorkWorktreeTransition{
				Transition: runtimepb.PendingWorkWorktreeTransitionKind_PENDING_WORK_WORKTREE_TRANSITION_KIND_ENTER,
				Selector:   &tc.selector,
			})
			if (err == nil) != tc.valid {
				t.Fatalf("validation = %v, want valid=%t", err, tc.valid)
			}
		})
	}
}

func TestPendingWorkSourceTextRejectsFEFFBlank(t *testing.T) {
	text := "\uFEFF \t"
	for _, source := range []struct {
		name     string
		validate func() error
	}{
		{"UserTurn", func() error {
			return protoapi.Validate(&runtimepb.UserTurnInput{Input: &runtimepb.UserTurnInput_Text{Text: text}})
		}},
		{"Chat", func() error { return protoapi.Validate(&chatpb.Activation{Input: &chatpb.Activation_Text{Text: text}}) }},
	} {
		t.Run(source.name, func(t *testing.T) {
			if err := source.validate(); err == nil {
				t.Fatal("FEFF-only source text accepted")
			}
		})
	}
}

func TestPendingWorkRestorationFEFFBlankContract(t *testing.T) {
	for _, tc := range []struct {
		text  string
		valid bool
	}{
		{"\uFEFFsubstantive\uFEFF", true},
		{"\uFEFF \t", false},
	} {
		err := protoapi.Validate(&runtimepb.PendingWorkRestoration{
			Kind:           runtimepb.PendingWorkItemKind_PENDING_WORK_ITEM_KIND_MESSAGE,
			CanonicalInput: tc.text,
		})
		if (err == nil) != tc.valid {
			t.Fatalf("text %q validation = %v, want valid=%t", tc.text, err, tc.valid)
		}
	}
}
