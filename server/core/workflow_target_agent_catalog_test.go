package core

import (
	"reflect"
	"testing"

	"core/server/workflow"
	"core/shared/config"
	"core/shared/toolspec"
)

func TestConfigTargetAgentCatalogSeparatesFallbackAndExplicitCallableRoles(t *testing.T) {
	settings := config.Settings{
		EnabledTools: map[toolspec.ID]bool{toolspec.ToolAskQuestion: true},
		Subagents: map[string]config.SubagentRole{
			"zeta": {
				AgentCallable: true,

				WorkflowSubagent: false, Sources: map[string]config.Origin{"agent_callable": {
					Kind:     config.SourceInput,
					Property: config.PropertyAddress{Key: "agent_callable"}}, "workflow_subagent": {
					Kind:     config.SourceInput,
					Property: config.PropertyAddress{Key: "workflow_subagent"}}},
			},
			"alpha": {
				AgentCallable: true, Sources: map[string]config.Origin{"agent_callable": {
					Kind:     config.SourceInput,
					Property: config.PropertyAddress{Key: "agent_callable"}}},
			},
			"blocked": {

				AgentCallable: false, Sources: map[string]config.Origin{"agent_callable": {
					Kind:     config.SourceInput,
					Property: config.PropertyAddress{Key: "agent_callable"}}},
			},
			"implicit": {},
		},
	}
	catalog := configTargetAgentCatalog{settings: settings}

	fallback, ok := catalog.ResolveConfiguredRole("blocked")
	if !ok || fallback.Identity != "blocked" {
		t.Fatalf("fallback role = %#v, %v", fallback, ok)
	}
	selectable := catalog.ExplicitCallableRoles()
	want := []workflow.TargetAgentRole{
		{Identity: "alpha", ExplicitAgentCallable: true, QuestionsEnabled: true},
		{Identity: "zeta", ExplicitAgentCallable: true, QuestionsEnabled: true},
	}
	if !reflect.DeepEqual(selectable, want) {
		t.Fatalf("selectable roles = %#v, want %#v", selectable, want)
	}
}

func TestConfigTargetAgentCatalogUsesEffectiveQuestionsForFallbackButSelectionForceEnables(t *testing.T) {
	settings := config.Settings{
		EnabledTools: map[toolspec.ID]bool{toolspec.ToolAskQuestion: true},
		Subagents: map[string]config.SubagentRole{
			"silent": {
				AgentCallable: true,

				Settings: config.Settings{EnabledTools: map[toolspec.ID]bool{toolspec.ToolAskQuestion: false}},
				Sources: map[string]config.Origin{"tools.ask_question": {
					Kind:     config.SourceInput,
					Property: config.PropertyAddress{Key: "tools.ask_question"}}, "agent_callable": {
					Kind:     config.SourceInput,
					Property: config.PropertyAddress{Key: "agent_callable"}},
				},
			},
		},
	}
	catalog := configTargetAgentCatalog{settings: settings}
	role, ok := catalog.ResolveConfiguredRole("SILENT")
	if !ok || role.QuestionsEnabled {
		t.Fatalf("fallback questions = %#v, %v", role, ok)
	}
	selection, err := workflow.PlanTargetAgentSelection(catalog, workflow.TargetAgentSelectionRequest{
		OverrideEnabled: true,
		SubmittedRole:   "silent",
	})
	if err != nil {
		t.Fatalf("PlanTargetAgentSelection: %v", err)
	}
	if !selection.QuestionsRequired {
		t.Fatal("transition-selected role did not force-enable Questions")
	}
}

func TestConfigTargetAgentCatalogResolvesFiniteAndOpenThinkingContracts(t *testing.T) {
	settings := config.Settings{
		Model:         "gpt-5.6-luna",
		ThinkingLevel: "medium",
		Subagents: map[string]config.SubagentRole{
			"finite": {
				AgentCallable: true,

				Settings: config.Settings{Model: "gpt-5.6-luna", ThinkingLevel: "high"},
				Sources: map[string]config.Origin{"model": {
					Kind:     config.SourceInput,
					Property: config.PropertyAddress{Key: "model"}}, "thinking_level": {
					Kind:     config.SourceInput,
					Property: config.PropertyAddress{Key: "thinking_level"}}, "agent_callable": {
					Kind:     config.SourceInput,
					Property: config.PropertyAddress{Key: "agent_callable"}},
				},
			},
			"open": {
				AgentCallable: true,

				Settings: config.Settings{Model: "custom-alias", ThinkingLevel: "medium"},
				Sources: map[string]config.Origin{"model": {
					Kind:     config.SourceInput,
					Property: config.PropertyAddress{Key: "model"}}, "thinking_level": {
					Kind:     config.SourceInput,
					Property: config.PropertyAddress{Key: "thinking_level"}}, "agent_callable": {
					Kind:     config.SourceInput,
					Property: config.PropertyAddress{Key: "agent_callable"}},
				},
			},
		},
	}
	catalog := configTargetAgentCatalog{settings: settings}
	finite, ok := catalog.ResolveConfiguredRole("finite")
	if !ok || !finite.Thinking.Finite || len(finite.Thinking.Levels) == 0 || finite.ConfiguredThinking != "high" {
		t.Fatalf("finite role = %#v, %v", finite, ok)
	}
	open, ok := catalog.ResolveConfiguredRole("open")
	if !ok || open.Thinking.Finite || !open.Thinking.ReasoningCapable {
		t.Fatalf("open role = %#v, %v", open, ok)
	}
}
