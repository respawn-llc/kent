import { useCallback } from "react";

import type { ApiService, WorkflowExecutionTargetSelection } from "@/api";
import {
  executeTaskInitiatingAction,
  type TaskInitiatingAction,
  useTaskInitiatingActionController,
} from "@/shared/execution-target";

type BoardInitiatingActionControllerOptions = Readonly<{
  api: ApiService;
  onActionError(id: string, title: string, error: unknown): void;
  onApplied(): void | Promise<void>;
  startErrorTitle: string;
  moveErrorTitle: string;
  resumeErrorTitle: string;
  refreshErrorTitle: string;
}>;

export function useBoardInitiatingActionController({
  api,
  onActionError,
  onApplied,
  startErrorTitle,
  moveErrorTitle,
  resumeErrorTitle,
  refreshErrorTitle,
}: BoardInitiatingActionControllerOptions) {
  const execute = useCallback(
    async (action: TaskInitiatingAction, selection?: WorkflowExecutionTargetSelection) =>
      executeTaskInitiatingAction(api, action, selection),
    [api],
  );
  const onAppliedError = useCallback(
    (error: unknown) => {
      onActionError("board-action-refresh-error", refreshErrorTitle, error);
    },
    [onActionError, refreshErrorTitle],
  );
  const initiatingAction = useTaskInitiatingActionController({
    execute,
    onApplied,
    onAppliedError,
    onError: (action, error) => {
      onActionError(
        action.kind === "start"
          ? "board-start-error"
          : action.kind === "resume"
            ? "board-resume-error"
            : "board-move-error",
        action.kind === "start"
          ? startErrorTitle
          : action.kind === "resume"
            ? resumeErrorTitle
            : moveErrorTitle,
        error,
      );
    },
  });
  return {
    initiatingAction,
    runCardAction: initiatingAction.run,
  };
}
