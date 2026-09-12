import type { ChatExecutionFacts, ChatActivityFacts } from "./chatTranscriptTypes";
import type { ChatExecutionTarget, ChatRuntimeActivity } from "./chatTypes";

export function chatExecutionTarget(input: ChatExecutionFacts): ChatExecutionTarget {
  return {
    workspaceID: input.WorkspaceID,
    workspaceName: input.WorkspaceName,
    workspaceRoot: input.WorkspaceRoot,
    workspaceAvailability: input.WorkspaceAvailability,
    worktree: input.Worktree,
    cwdRelpath: input.CwdRelpath,
    effectiveWorkdir: input.EffectiveWorkdir,
  };
}

export function chatRuntimeActivity(input: ChatActivityFacts): ChatRuntimeActivity {
  return {
    state: input.State,
    activeStep:
      input.ActiveStep === null
        ? null
        : {
            runID: input.ActiveStep.RunID,
            stepID: input.ActiveStep.StepID,
            activeKind: input.ActiveStep.ActiveKind,
          },
    reviewer: input.Reviewer,
    queueAccepting: input.QueueAccepting,
    diagnosticRecovery: input.DiagnosticRecovery,
  };
}
