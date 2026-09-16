package serverapi

import (
	"encoding/json"
	"errors"

	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/protocol"
)

type workflowSetupRetainedEnvelope struct {
	Type                     string                             `json:"type"`
	RecoveryDisposition      string                             `json:"recovery_disposition"`
	Worktree                 workflowRegisteredWorktreeEnvelope `json:"worktree"`
	ScriptPath               string                             `json:"script_path"`
	Diagnostic               string                             `json:"diagnostic"`
	RetainedPreviousWorktree json.RawMessage                    `json:"retained_previous_worktree"`
}

var setupRecoverySpellings = map[worktreepb.SetupRecoveryDisposition]string{
	worktreepb.SetupRecoveryDisposition_SETUP_RECOVERY_DISPOSITION_RETRY_EXISTING:    "retry_existing",
	worktreepb.SetupRecoveryDisposition_SETUP_RECOVERY_DISPOSITION_FRESH_REPLACEMENT: "fresh_replacement",
}

type workflowRetainedPreviousWorktreeEnvelope struct {
	Worktree workflowRegisteredWorktreeEnvelope `json:"worktree"`
}

func (e *WorkflowSetupRetainedError) MarshalJSON() ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	details := e.Details
	var previous *workflowRetainedPreviousWorktreeEnvelope
	if details.RetainedPreviousWorktree != nil {
		previous = &workflowRetainedPreviousWorktreeEnvelope{Worktree: registeredWorktreeEnvelope(details.RetainedPreviousWorktree.Worktree)}
	}
	previousJSON, err := json.Marshal(previous)
	if err != nil {
		return nil, err
	}
	return json.Marshal(workflowSetupRetainedEnvelope{
		Type: "worktree_setup_retained", RecoveryDisposition: setupRecoverySpellings[details.RecoveryDisposition],
		Worktree:   registeredWorktreeEnvelope(details.Worktree),
		ScriptPath: details.ScriptPath, Diagnostic: details.Diagnostic, RetainedPreviousWorktree: previousJSON,
	})
}

func (e *WorkflowSetupRetainedError) UnmarshalJSON(data []byte) error {
	var envelope workflowSetupRetainedEnvelope
	if err := protocol.DecodeStrictJSON(data, &envelope); err != nil {
		return err
	}
	if envelope.Type != "worktree_setup_retained" || len(envelope.RetainedPreviousWorktree) == 0 {
		return errors.New("invalid retained setup envelope")
	}
	facts, err := envelope.Worktree.protoFacts()
	if err != nil {
		return err
	}
	details := &worktreepb.SetupRetainedDetails{Worktree: facts, ScriptPath: envelope.ScriptPath, Diagnostic: envelope.Diagnostic}
	for value, spelling := range setupRecoverySpellings {
		if spelling == envelope.RecoveryDisposition {
			details.RecoveryDisposition = value
			break
		}
	}
	var previous *workflowRetainedPreviousWorktreeEnvelope
	if err := protocol.DecodeStrictJSON(envelope.RetainedPreviousWorktree, &previous); err != nil {
		return err
	}
	if previous != nil {
		facts, err := previous.Worktree.protoFacts()
		if err != nil {
			return err
		}
		details.RetainedPreviousWorktree = &worktreepb.RetainedPreviousWorktree{Worktree: facts}
	}
	result := WorkflowSetupRetainedError{Details: details}
	if err := result.Validate(); err != nil {
		return err
	}
	*e = result
	return nil
}

func (topology workflowRegisteredWorktreeEnvelope) protoFacts() (*worktreepb.RegisteredFacts, error) {
	if topology.Variant != "registered" || topology.Registered == nil {
		return nil, errors.New("workflow retained worktree must be registered")
	}
	git, kent := topology.Registered.Git, topology.Registered.Kent
	return &worktreepb.RegisteredFacts{
		Git: &worktreepb.GitFacts{
			CanonicalRoot: git.CanonicalRoot, HeadObject: git.HeadObject,
			BranchRef: git.BranchRef, BranchName: git.BranchName, Detached: git.Detached,
			Bare: git.Bare, LockedReason: git.LockedReason, PrunableReason: git.PrunableReason,
			IsMainWorktree: git.IsMainWorktree, PathAvailable: git.PathAvailable,
		},
		Kent: &worktreepb.KentFacts{
			WorktreeId: kent.WorktreeID, CanonicalRoot: kent.CanonicalRoot,
			DisplayName: kent.DisplayName, Managed: kent.Managed,
			CreatedBranch: kent.CreatedBranch, OriginSessionId: kent.OriginSessionID,
		},
	}, nil
}

func registeredWorktreeEnvelope(registered *worktreepb.RegisteredFacts) workflowRegisteredWorktreeEnvelope {
	git, kent := registered.GetGit(), registered.GetKent()
	facts := &workflowRegisteredWorktreeFacts{}
	if git != nil {
		facts.Git = workflowWorktreeGitFacts{
			CanonicalRoot: git.CanonicalRoot, HeadObject: git.HeadObject,
			BranchRef: git.BranchRef, BranchName: git.BranchName, Detached: git.Detached,
			Bare: git.Bare, LockedReason: git.LockedReason, PrunableReason: git.PrunableReason,
			IsMainWorktree: git.IsMainWorktree, PathAvailable: git.PathAvailable,
		}
	}
	if kent != nil {
		facts.Kent = workflowWorktreeKentFacts{
			WorktreeID: kent.WorktreeId, CanonicalRoot: kent.CanonicalRoot,
			DisplayName: kent.DisplayName, Managed: kent.Managed,
			CreatedBranch: kent.CreatedBranch, OriginSessionID: kent.OriginSessionId,
		}
	}
	return workflowRegisteredWorktreeEnvelope{Variant: "registered", Registered: facts}
}

func (t WorkflowRegisteredWorktreeTopology) MarshalJSON() ([]byte, error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(registeredWorktreeEnvelope(t.Registered))
}

func (t *WorkflowRegisteredWorktreeTopology) UnmarshalJSON(data []byte) error {
	var envelope workflowRegisteredWorktreeEnvelope
	if err := protocol.DecodeStrictJSON(data, &envelope); err != nil {
		return err
	}
	facts, err := envelope.protoFacts()
	if err != nil {
		return err
	}
	result := WorkflowRegisteredWorktreeTopology{Registered: facts}
	if err := result.Validate(); err != nil {
		return err
	}
	*t = result
	return nil
}
