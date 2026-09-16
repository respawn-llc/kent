package subagentpolicy

import (
	"errors"
	"testing"

	"core/shared/config"
	"core/shared/serverapi"
)

func TestAuthorizeCallerTargetMatrix(t *testing.T) {
	settings := config.Settings{
		Workflow: config.WorkflowSettings{Subagents: true},
		Subagents: map[string]config.SubagentRole{
			"worker":  {AgentCallableSet: true, AgentCallable: true, WorkflowSubagentSet: true, WorkflowSubagent: true},
			"hidden":  {AgentCallableSet: true, AgentCallable: true, WorkflowSubagentSet: true, WorkflowSubagent: false},
			"blocked": {AgentCallableSet: true, AgentCallable: false},
		},
	}
	workflow := &Caller{Workflow: true}
	for _, target := range []Target{
		{Kind: TargetOmittedBase},
		{Kind: TargetExplicitBase},
		{Kind: TargetNamed, Selector: "worker"},
		{Kind: TargetNamed, Selector: config.BuiltInSubagentRoleFast},
	} {
		if err := Authorize(settings, workflow, target); err != nil {
			t.Fatalf("Authorize workflow target %+v: %v", target, err)
		}
	}
	for _, selector := range []string{"hidden", "blocked"} {
		err := Authorize(settings, workflow, Target{Kind: TargetNamed, Selector: selector})
		var denied *serverapi.SubagentLaunchDeniedError
		if !errors.As(err, &denied) || denied.Kind != serverapi.SubagentLaunchDenialNotCallable {
			t.Fatalf("Authorize %q error = %T %v, want not-callable denial", selector, err, err)
		}
	}
	if err := Authorize(settings, nil, Target{Kind: TargetNamed, Selector: "blocked"}); err != nil {
		t.Fatalf("human blocked role should bypass callability: %v", err)
	}
	if err := Authorize(settings, nil, Target{Kind: TargetNamed, Selector: "missing"}); !isDenialKind(err, serverapi.SubagentLaunchDenialTargetMissing) {
		t.Fatalf("human missing role error = %v, want missing-target denial", err)
	}
	ordinary := &Caller{}
	if err := Authorize(settings, ordinary, Target{Kind: TargetNamed, Selector: "blocked"}); !isDenialKind(err, serverapi.SubagentLaunchDenialNotCallable) {
		t.Fatalf("ordinary blocked role error = %v, want not-callable denial", err)
	}
	for _, selector := range []string{config.BuiltInSubagentRoleFast, "worker"} {
		if err := Authorize(settings, ordinary, Target{Kind: TargetNamed, Selector: selector}); err != nil {
			t.Fatalf("ordinary caller launching %q: %v", selector, err)
		}
	}
	workflowNoSwitch := &Caller{Workflow: true}
	if err := Authorize(config.Settings{Subagents: settings.Subagents}, workflowNoSwitch, Target{Kind: TargetNamed, Selector: config.BuiltInSubagentRoleFast}); !isDenialKind(err, serverapi.SubagentLaunchDenialNotCallable) {
		t.Fatalf("fast target with workflow delegation disabled: %v, want not-callable denial", err)
	}
	blockedFast := config.Settings{Subagents: map[string]config.SubagentRole{
		config.BuiltInSubagentRoleFast: {AgentCallableSet: true, AgentCallable: false},
	}}
	workflowBlockedFast := &Caller{Workflow: true}
	if err := Authorize(blockedFast, workflowBlockedFast, Target{Kind: TargetNamed, Selector: config.BuiltInSubagentRoleFast}); !isDenialKind(err, serverapi.SubagentLaunchDenialNotCallable) {
		t.Fatalf("blocked fast role error = %v, want not-callable denial", err)
	}
}

func TestFastRoleObeysWorkflowDelegationFlags(t *testing.T) {
	settings := config.Settings{
		Workflow: config.WorkflowSettings{Subagents: true},
		Subagents: map[string]config.SubagentRole{
			config.BuiltInSubagentRoleFast: {WorkflowSubagentSet: true, WorkflowSubagent: false},
		},
	}
	target := Target{Kind: TargetNamed, Selector: config.BuiltInSubagentRoleFast}
	if err := Authorize(settings, &Caller{Workflow: true}, target); !isDenialKind(err, serverapi.SubagentLaunchDenialNotCallable) {
		t.Fatalf("workflow-disabled fast role: %v, want not-callable denial", err)
	}
	for _, caller := range []*Caller{nil, {}} {
		if err := Authorize(settings, caller, target); err != nil {
			t.Fatalf("non-workflow caller launching fast: %v", err)
		}
	}
}

func TestDefaultRoleDelegationPolicy(t *testing.T) {
	for _, target := range []Target{
		{Kind: TargetOmittedBase},
		{Kind: TargetExplicitBase},
		{Kind: TargetNamed, Selector: config.DefaultSubagentRole},
	} {
		for _, tc := range []struct {
			name     string
			role     config.SubagentRole
			workflow bool
			caller   *Caller
			denied   bool
		}{
			{name: "ordinary unconfigured", caller: &Caller{}},
			{name: "ordinary disabled", caller: &Caller{}, role: config.SubagentRole{AgentCallableSet: true}, denied: true},
			{name: "human bypass", role: config.SubagentRole{AgentCallableSet: true, WorkflowSubagentSet: true}},
			{name: "workflow enabled", caller: &Caller{Workflow: true}, workflow: true},
			{name: "workflow globally disabled", caller: &Caller{Workflow: true}, denied: true},
			{name: "workflow role disabled", caller: &Caller{Workflow: true}, workflow: true, role: config.SubagentRole{WorkflowSubagentSet: true}, denied: true},
			{name: "workflow agent disabled", caller: &Caller{Workflow: true}, workflow: true, role: config.SubagentRole{AgentCallableSet: true}, denied: true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				settings := config.Settings{
					Workflow:  config.WorkflowSettings{Subagents: tc.workflow},
					Subagents: map[string]config.SubagentRole{config.DefaultSubagentRole: tc.role},
				}
				err := Authorize(settings, tc.caller, target)
				if !tc.denied {
					if err != nil {
						t.Fatalf("target %+v unexpectedly denied: %v", target, err)
					}
					return
				}
				var denied *serverapi.SubagentLaunchDeniedError
				if !errors.As(err, &denied) || denied.Kind != serverapi.SubagentLaunchDenialNotCallable ||
					denied.Target == nil || *denied.Target != config.DefaultSubagentRole {
					t.Fatalf("target %+v error = %v, want default not-callable denial", target, err)
				}
			})
		}
	}
}

func isDenialKind(err error, kind serverapi.SubagentLaunchDenialKind) bool {
	var denied *serverapi.SubagentLaunchDeniedError
	return errors.As(err, &denied) && denied.Kind == kind
}
