package workflow_test

import (
	"maps"
	"testing"

	"core/server/workflow"
)

func TestResumePromptParametersUseEffectiveSelectionWithoutHistoricalBackfill(t *testing.T) {
	role := workflow.TargetAgentRole{
		Identity: "deleted-saved-role", ConfiguredThinking: "high",
		Thinking: workflow.ThinkingCapability{ReasoningCapable: true, Finite: true, Levels: []string{"low", "high"}},
	}
	request := workflow.TransitionParameterContractRequest{
		Edge: workflow.Edge{
			ContextMode:       workflow.ContextModeNewSession,
			AssigneeSelection: workflow.AssigneeSelectionPreviousNode, ThinkingSelection: workflow.ThinkingSelectionPreviousNode,
			Parameters: []workflow.Parameter{
				{Key: "agent", Purpose: workflow.ParameterPurposeTargetAssignee},
				{Key: "effort", Purpose: workflow.ParameterPurposeTargetThinking},
				{Key: "ordinary", Purpose: workflow.ParameterPurposeOrdinary},
			},
		},
		SourceKind: workflow.NodeKindAgent, TargetKind: workflow.NodeKindAgent, TargetRole: "unused-deleted-fallback",
		Catalog: selectorCatalog{explicit: []workflow.TargetAgentRole{
			{Identity: "one", ExplicitAgentCallable: true}, {Identity: "two", ExplicitAgentCallable: true},
		}},
	}
	for _, test := range []struct {
		name     string
		prompt   string
		thinking string
		current  map[string]string
		want     map[string]string
	}{
		{name: "effective selection", prompt: "{{.Params.agent}} {{.Params.effort}}", thinking: "high",
			want: map[string]string{"agent": role.Identity, "effort": "high"}},
		{name: "model default", prompt: "{{.Params.effort}}",
			want: map[string]string{"effort": "model default"}},
		{name: "unreferenced", thinking: "high", want: nil},
		{name: "prior reference", prompt: "{{.Params.previous.effort}}", thinking: "high", want: nil},
		{name: "ordinary absent", prompt: "{{.Params.ordinary}}", thinking: "high", want: nil},
		{name: "preserved input", prompt: "{{.Params.effort}}", thinking: "high",
			current: map[string]string{"effort": "low"}, want: map[string]string{"effort": "low"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			request.Edge.PromptTemplate = test.prompt
			role.ConfiguredThinking = test.thinking
			before := maps.Clone(test.current)
			values, err := workflow.ResumePromptParameters(request, role, test.current)
			if err != nil {
				t.Fatal(err)
			}
			if !maps.Equal(values, test.want) {
				t.Fatalf("Resume inputs = %v, want %v", values, test.want)
			}
			if !maps.Equal(test.current, before) {
				t.Fatal("Resume mutated original inputs")
			}
		})
	}
}

func TestResumePromptParametersKeepAutomaticSelectionsHidden(t *testing.T) {
	for _, levels := range [][]string{nil, {"high"}} {
		for _, roles := range [][]workflow.TargetAgentRole{nil, {{Identity: "sole", ExplicitAgentCallable: true}}} {
			for _, parameter := range []workflow.Parameter{
				{Key: "agent", Purpose: workflow.ParameterPurposeTargetAssignee},
				{Key: "effort", Purpose: workflow.ParameterPurposeTargetThinking},
			} {
				request := workflow.TransitionParameterContractRequest{
					Edge: workflow.Edge{
						ContextMode:       workflow.ContextModeNewSession,
						AssigneeSelection: workflow.AssigneeSelectionPreviousNode, ThinkingSelection: workflow.ThinkingSelectionPreviousNode,
						Parameters: []workflow.Parameter{parameter},
					},
					SourceKind: workflow.NodeKindAgent, TargetKind: workflow.NodeKindAgent,
					Catalog: selectorCatalog{explicit: roles},
				}
				effective := workflow.TargetAgentRole{
					Identity: "saved", ConfiguredThinking: "high",
					Thinking: workflow.ThinkingCapability{ReasoningCapable: true, Finite: true, Levels: levels},
				}
				values, err := workflow.ResumePromptParameters(request, effective, nil)
				if err != nil || len(values) != 0 {
					t.Fatalf("hidden automatic selection was exposed: %v, %v", values, err)
				}
				request.Edge.PromptTemplate = "{{.Params." + parameter.Key + "}}"
				if _, err := workflow.ResumePromptParameters(request, effective, nil); err == nil {
					t.Fatalf("new placeholder exposed hidden parameter %q", parameter.Key)
				}
			}
		}
	}
}
