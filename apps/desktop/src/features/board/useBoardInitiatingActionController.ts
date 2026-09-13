import { useCallback } from "react";

import type { ApiService, WorkflowExecutionTargetSelection } from "@/api";
import {
  executeTaskInitiatingAction,
  type TaskInitiatingAction,
  useTaskInitiatingActionController,
} from "@/shared/execution-target";

type BoardInitiatingActionControllerOptions = Readonly<{
  api: ApiService;
  connected: boolean;
  onActionError(id: string, title: string, error: unknown): void;
  onApplied(): void | Promise<void>;
  startErrorTitle: string;
  moveErrorTitle: string;
  refreshErrorTitle: string;
}>;

export function useBoardInitiatingActionController({
  api,
  connected,
  onActionError,
  onApplied,
  startErrorTitle,
  moveErrorTitle,
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
  });
  const { pending, run, running } = initiatingAction;
  const runCardAction = useCallback(
    (action: TaskInitiatingAction, selection?: WorkflowExecutionTargetSelection): void => {
      void run(action, selection).catch((error: unknown) => {
        onActionError(
          action.kind === "start" ? "board-start-error" : "board-move-error",
          action.kind === "start" ? startErrorTitle : moveErrorTitle,
          error,
        );
      });
    },
    [moveErrorTitle, onActionError, run, startErrorTitle],
  );
  const actionPending = running || pending !== null;
  return {
    actionPending,
    actionsDisabled: !connected || actionPending,
    initiatingAction,
    runCardAction,
  };
}
