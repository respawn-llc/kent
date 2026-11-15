package workflow_test

import (
	"errors"
	"slices"
	"testing"

	"core/server/workflow"
)

func TestTargetAgentThinkingContractUnionsFiniteRoleCatalogs(t *testing.T) {
	contract := workflow.UnionTargetAgentThinkingCapabilities([]workflow.TargetAgentRole{
		{Thinking: workflow.ThinkingCapability{ReasoningCapable: true, Finite: true, Levels: []string{"low", "medium"}}},
		{Thinking: workflow.ThinkingCapability{ReasoningCapable: true, Finite: true, Levels: []string{"medium", "high"}}},
	})
	if !contract.Finite || !contract.ReasoningCapable || !slices.Equal(contract.Levels, []string{"high", "low", "medium"}) {
		t.Fatalf("union contract = %+v", contract)
	}
}

func TestTransitionSelectionMaterializesThinking(t *testing.T) {
	finiteEmpty := workflow.ThinkingCapability{ReasoningCapable: true, Finite: true}
	finiteOne := workflow.ThinkingCapability{ReasoningCapable: true, Finite: true, Levels: []string{"high"}}
	finiteMany := workflow.ThinkingCapability{ReasoningCapable: true, Finite: true, Levels: []string{"low", "high"}}
	open := workflow.ThinkingCapability{ReasoningCapable: true}
	for _, test := range []struct {
		name        string
		target      workflow.ThinkingCapability
		other       *workflow.ThinkingCapability
		disabled    bool
		configured  string
		submitted   string
		description string
		want        *workflow.ThinkingValue
		wantError   workflow.TargetAgentSelectionErrorCode
	}{
		{name: "configured", target: finiteMany, disabled: true, configured: "low", submitted: "high", want: thinkingValuePointer("low")},
		{name: "non reasoning uses configured", configured: "low", submitted: "ignored", want: thinkingValuePointer("low")},
		{name: "finite empty uses configured", target: finiteEmpty, configured: "medium", submitted: "ignored", want: thinkingValuePointer("medium")},
		{name: "finite empty without configured", target: finiteEmpty, submitted: "ignored"},
		{name: "finite sole automatic", target: finiteOne, submitted: "ignored", want: thinkingValuePointer("high")},
		{name: "finite sole automatic without value", target: finiteOne, want: thinkingValuePointer("high")},
		{name: "finite many", target: finiteMany, submitted: "high", want: thinkingValuePointer("high")},
		{name: "finite many missing", target: finiteMany, wantError: workflow.TargetAgentSelectionErrorThinkingRequired},
		{name: "finite many unsupported", target: finiteMany, submitted: "custom", wantError: workflow.TargetAgentSelectionErrorUnsupportedThinking},
		{name: "finite selected role mismatch", target: finiteOne, other: &finiteMany, submitted: "low", wantError: workflow.TargetAgentSelectionErrorUnsupportedThinking},
		{name: "automatic union level unsupported by selected role", target: finiteEmpty, other: &finiteOne, submitted: "ignored"},
		{name: "open requires description", target: open, submitted: "custom", wantError: workflow.TargetAgentSelectionErrorThinkingDescriptionRequired},
		{name: "open requires value", target: open, description: "Provider effort", wantError: workflow.TargetAgentSelectionErrorThinkingRequired},
		{name: "open accepts custom", target: open, submitted: "custom", description: "Provider effort", want: thinkingValuePointer("custom")},
		{name: "mixed open union validates finite role", target: finiteOne, other: &open, submitted: "custom", description: "Provider effort", wantError: workflow.TargetAgentSelectionErrorUnsupportedThinking},
		{name: "mixed open union accepts supported finite value", target: finiteOne, other: &open, submitted: "high", description: "Provider effort", want: thinkingValuePointer("high")},
	} {
		t.Run(test.name, func(t *testing.T) {
			target := workflow.TargetAgentRole{
				Identity: "target", QuestionsEnabled: true, ExplicitAgentCallable: true,
				ConfiguredThinking: test.configured, Thinking: test.target,
			}
			catalog := targetAgentCatalogStub{
				fallback:   map[string]workflow.TargetAgentRole{"target": target},
				selectable: []workflow.TargetAgentRole{target},
			}
			edge := workflow.Edge{}
			if !test.disabled {
				edge.ThinkingSelection = workflow.ThinkingSelectionPreviousNode
			}
			if test.other != nil {
				edge.AssigneeSelection = workflow.AssigneeSelectionPreviousNode
				catalog.selectable = append(catalog.selectable, workflow.TargetAgentRole{
					Identity: "other", ExplicitAgentCallable: true, Thinking: *test.other,
				})
			}
			plan, err := workflow.PlanTransitionSelection(workflow.TransitionParameterContractRequest{
				Edge: edge, SourceKind: workflow.NodeKindAgent, TargetKind: workflow.NodeKindAgent,
				TargetRole: "target", Catalog: catalog,
				Materialization: &workflow.TransitionSelectionMaterializationRequest{
					SubmittedRole: "target", SubmittedThinking: test.submitted, ThinkingDescription: test.description,
				},
			})
			if test.wantError != "" {
				var selectionErr workflow.TargetAgentSelectionError
				if !errors.As(err, &selectionErr) || selectionErr.Code != test.wantError {
					t.Fatalf("error = %v, want %s", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if plan.ExecutionSelection == nil {
				t.Fatal("missing execution selection")
			}
			got := plan.ExecutionSelection.Thinking
			if (got == nil) != (test.want == nil) || (got != nil && test.want != nil && *got != *test.want) {
				t.Fatalf("thinking = %v, want %v", got, test.want)
			}
		})
	}
}

func thinkingValuePointer(value workflow.ThinkingValue) *workflow.ThinkingValue {
	return &value
}
