package workflow_test

import (
	"fmt"
	"slices"
	"testing"

	"core/server/workflow"
)

func TestTransitionContractProtectedParameterConsumption(t *testing.T) {
	for _, test := range []struct {
		name      string
		source    workflow.NodeKind
		target    workflow.NodeKind
		mode      workflow.ContextMode
		context   workflow.ContextSourceKind
		disabled  bool
		fanout    bool
		retained  bool
		roleCount int
		thinking  workflow.ThinkingCapability
		required  []string
		known     []string
	}{
		{name: "disabled", disabled: true, roleCount: 2},
		{name: "fanout", fanout: true, roleCount: 2},
		{name: "Start source", source: workflow.NodeKindStart, roleCount: 2},
		{name: "Join source", source: workflow.NodeKindJoin, roleCount: 2},
		{name: "Script target", target: workflow.NodeKindScript, roleCount: 2},
		{name: "Terminal target", target: workflow.NodeKindTerminal, roleCount: 2},
		{name: "no callable roles"},
		{name: "sole role no thinking", roleCount: 1, known: []string{"role"}},
		{name: "many roles no thinking", roleCount: 2, required: []string{"role"}, known: []string{"role"}},
		{
			name: "Script source finite sole options", source: workflow.NodeKindScript, roleCount: 1,
			thinking: workflow.ThinkingCapability{ReasoningCapable: true, Finite: true, Levels: []string{"high"}},
			known:    []string{"role", "effort"},
		},
		{
			name: "finite zero thinking", roleCount: 1,
			thinking: workflow.ThinkingCapability{ReasoningCapable: true, Finite: true},
			known:    []string{"role", "effort"},
		},
		{
			name: "finite many options", roleCount: 2,
			thinking: workflow.ThinkingCapability{ReasoningCapable: true, Finite: true, Levels: []string{"low", "high"}},
			required: []string{"role", "effort"}, known: []string{"role", "effort"},
		},
		{
			name: "open thinking", roleCount: 1,
			thinking: workflow.ThinkingCapability{ReasoningCapable: true},
			required: []string{"effort"}, known: []string{"role", "effort"},
		},
		{
			name: "retained target", mode: workflow.ContextModeContinueSession,
			context: workflow.ContextSourcePreviousTargetOrNew, retained: true, roleCount: 2,
			thinking: workflow.ThinkingCapability{ReasoningCapable: true, Finite: true, Levels: []string{"low", "high"}},
			required: []string{"effort"}, known: []string{"role", "effort"},
		},
		{
			name: "retained target without callable roles", mode: workflow.ContextModeContinueSession,
			context: workflow.ContextSourcePreviousTargetOrNew, retained: true,
			known: []string{"role"},
		},
		{
			name: "new previous target", mode: workflow.ContextModeContinueSession,
			context: workflow.ContextSourcePreviousTargetOrNew, roleCount: 2,
			required: []string{"role"}, known: []string{"role"},
		},
		{
			name: "immediate continuation", mode: workflow.ContextModeContinueSession,
			context: workflow.ContextSourceImmediateSource, roleCount: 2,
			thinking: workflow.ThinkingCapability{ReasoningCapable: true, Finite: true, Levels: []string{"low", "high"}},
			required: []string{"effort"}, known: []string{"effort"},
		},
		{
			name: "compacted continuation", mode: workflow.ContextModeCompactAndContinueSession, roleCount: 2,
			required: []string{"role"}, known: []string{"role"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			source, target := workflow.NodeKindAgent, workflow.NodeKindAgent
			if test.source != "" {
				source = test.source
			}
			if test.target != "" {
				target = test.target
			}
			roles := make([]workflow.TargetAgentRole, test.roleCount)
			for i := range roles {
				roles[i] = workflow.TargetAgentRole{
					Identity: fmt.Sprintf("role-%d", i), ExplicitAgentCallable: true, Thinking: test.thinking,
				}
			}
			edge := workflow.Edge{
				ContextMode: test.mode, ContextSource: workflow.ContextSource{Kind: test.context},
				Parameters: []workflow.Parameter{
					{Key: "ordinary", Purpose: workflow.ParameterPurposeOrdinary},
					{Key: "role", Purpose: workflow.ParameterPurposeTargetAssignee},
					{Key: "effort", Purpose: workflow.ParameterPurposeTargetThinking, Description: "Provider effort"},
				},
			}
			if !test.disabled {
				edge.AssigneeSelection = workflow.AssigneeSelectionPreviousNode
				edge.ThinkingSelection = workflow.ThinkingSelectionPreviousNode
			}
			catalog := targetAgentCatalogStub{
				selectable: roles,
				fallback: map[string]workflow.TargetAgentRole{
					"saved": {Identity: "saved", Thinking: test.thinking},
				},
			}
			request := workflow.TransitionParameterContractRequest{
				Edge: edge, SourceKind: source, TargetKind: target, FanOut: test.fanout,
				Catalog: catalog,
			}
			if test.retained {
				selection, err := workflow.NewAgentExecutionSelection("saved", nil, workflow.AssigneeOriginRetainedSession)
				if err != nil {
					t.Fatal(err)
				}
				request.RetainedSession = &selection
			}
			contract, err := workflow.PlanTransitionParameterContract(request)
			if err != nil {
				t.Fatal(err)
			}
			keys := func(parameters []workflow.Parameter) []string {
				result := make([]string, 0, len(parameters))
				for _, parameter := range parameters {
					result = append(result, parameter.Key)
				}
				return result
			}
			if got, want := keys(contract.Parameters), append([]string{"ordinary"}, test.required...); !slices.Equal(got, want) {
				t.Errorf("required Parameters = %v, want %v", got, want)
			}
			if got, want := keys(contract.KnownParameters), append([]string{"ordinary"}, test.known...); !slices.Equal(got, want) {
				t.Errorf("accepted Parameters = %v, want %v", got, want)
			}
		})
	}
}
