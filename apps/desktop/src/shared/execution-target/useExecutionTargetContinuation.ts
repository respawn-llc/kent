import { useMemo, useState } from "react";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { useAtomMount, useAtomSet, useAtomValue } from "@effect/atom-react";
import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";
import type {
  WorkflowExecutionTargetSelection,
  WorkflowExecutionTargetSelectionMode,
  WorkflowExecutionTargetSelectionRequirement,
  TaskMovePreviewResponse,
} from "@/api";
import { decodeWorktreeSetupRetainedError, type WorktreeSetupRetainedError } from "@/api";
import { reportNonCancelledError } from "@/app-facade";
import {
  initialExecutionTargetSelectionDraft,
  moveTaskInitiatingAction,
  taskInitiatingActionTaskID,
  type ExecutionTargetSelectionDraft,
  type TaskInitiatingAction,
  type TaskInitiatingActionResult,
} from "./executionTargetContinuation";
import { createTaskRequests } from "./taskRequests";

export type PendingTaskInitiatingAction =
  | Readonly<{
      kind: "move_preview";
      action: Extract<TaskInitiatingAction, { kind: "move" }>;
      preview: TaskMovePreviewResponse;
    }>
  | Readonly<{
      kind: "dependency_confirmation";
      action: TaskInitiatingAction;
      unsatisfiedDependencyCount: number;
    }>
  | Readonly<{
      kind: "execution_target";
      action: TaskInitiatingAction;
      requirement: WorkflowExecutionTargetSelectionRequirement;
      selection: ExecutionTargetSelectionDraft;
    }>
  | Readonly<{
      kind: "setup_recovery";
      action: Extract<TaskInitiatingAction, { kind: "move" }>;
      failure: WorktreeSetupRetainedError;
      retrySelection?: WorkflowExecutionTargetSelection;
    }>;

type Options = Readonly<{
  execute(
    action: TaskInitiatingAction,
    selection?: WorkflowExecutionTargetSelection,
  ): Promise<TaskInitiatingActionResult>;
  onApplied(result: TaskInitiatingActionResult): void | Promise<void>;
  onAppliedError(error: unknown, result: TaskInitiatingActionResult): void;
  onError(action: TaskInitiatingAction, error: unknown): void;
}>;

type Preview = Readonly<{
  taskID: string;
  targetNodeID: string;
  execute(): Promise<TaskMovePreviewResponse>;
  onBlocked(reason: string): void;
  onError(error: unknown): void;
}>;

function createTaskInitiatingActions(client: QueryClient) {
  const requests = createTaskRequests(client);
  const run = Atom.fn<
    Options &
      Readonly<{
        action: TaskInitiatingAction;
        selection?: WorkflowExecutionTargetSelection;
        onConfirmation(pending: PendingTaskInitiatingAction): void;
        onCompleted(): void;
      }>
  >()(
    (input) =>
      Effect.gen(function* () {
        const { action, selection } = input;
        const taskID = taskInitiatingActionTaskID(action);
        const operation = requests.start(taskID, action.kind === "resume" ? "resume" : "start-move", {
          mutationFn: async () => input.execute(action, selection),
          async onSuccess(result) {
            const { response } = result;
            if (response.outcome === "dependency_confirmation_required") {
              input.onConfirmation({
                kind: "dependency_confirmation",
                action,
                unsatisfiedDependencyCount: response.unsatisfiedDependencyCount,
              });
              return;
            }
            if (response.outcome === "selection_required") {
              input.onConfirmation({
                kind: "execution_target",
                action,
                requirement: response.selectionRequired,
                selection: initialExecutionTargetSelectionDraft(response.selectionRequired),
              });
              return;
            }
            input.onCompleted();
            try {
              await input.onApplied(result);
            } catch (error) {
              reportNonCancelledError(error, (failure) => {
                input.onAppliedError(failure, result);
              });
            }
          },
          onError(error) {
            const failure = decodeWorktreeSetupRetainedError(error);
            if (action.kind === "move" && failure !== null) {
              input.onConfirmation({
                kind: "setup_recovery",
                action,
                failure,
                ...(selection === undefined ? {} : { retrySelection: selection }),
              });
            } else input.onError(action, error);
          },
        });
        if (operation === null) return;
        yield* Effect.tryPromise(async () => operation).pipe(Effect.ignore);
      }),
    { concurrent: true },
  );
  const preview = Atom.fn<
    Preview & Readonly<{ onConfirmation(pending: PendingTaskInitiatingAction): void }>
  >()(
    (input) =>
      Effect.gen(function* () {
        const operation = requests.start(input.taskID, "start-move", {
          mutationFn: input.execute,
          onSuccess(response) {
            if (response.outcome === "no_op") return;
            if (response.outcome === "blocked") {
              input.onBlocked(response.blocked.reason);
              return;
            }
            input.onConfirmation({
              kind: "move_preview",
              action: moveTaskInitiatingAction({ taskID: input.taskID, targetNodeID: input.targetNodeID }),
              preview: response,
            });
          },
          onError: input.onError,
        });
        if (operation !== null) yield* Effect.tryPromise(async () => operation).pipe(Effect.ignore);
      }),
    { concurrent: true },
  );
  return { requests: requests.pending, run, preview } as const;
}

export function useTaskInitiatingActionController(options: Options) {
  const client = useQueryClient();
  const model = useMemo(() => createTaskInitiatingActions(client), [client]);
  useAtomMount(model.requests);
  const [pending, setPending] = useState<PendingTaskInitiatingAction | null>(null);
  const requests = useAtomValue(model.requests);
  const submit = useAtomSet(model.run, { mode: "value" });
  const preview = useAtomSet(model.preview, { mode: "value" });
  const confirmationTaskID = pending === null ? null : taskInitiatingActionTaskID(pending.action);
  return {
    pending,
    confirmationTaskID,
    pendingStartMoveTaskIDs: requests.startMove,
    pendingResumeTaskIDs: requests.resume,
    running:
      pending !== null &&
      (pending.action.kind === "resume" ? requests.resume : requests.startMove).has(
        taskInitiatingActionTaskID(pending.action),
      ),
    run: (action: TaskInitiatingAction, selection?: WorkflowExecutionTargetSelection) => {
      submit({
        ...options,
        action,
        ...(selection === undefined ? {} : { selection }),
        onConfirmation: setPending,
        onCompleted: () => {
          setPending((current) => (current?.action === action ? null : current));
        },
      });
    },
    preview: (input: Preview) => {
      preview({ ...input, onConfirmation: setPending });
    },
    close: () => {
      setPending(null);
    },
    selectMode: (mode: WorkflowExecutionTargetSelectionMode) => {
      setPending((current) =>
        current?.kind === "execution_target"
          ? { ...current, selection: { ...current.selection, mode } }
          : current,
      );
    },
    setCustomRef: (customRef: string | null) => {
      setPending((current) =>
        current?.kind === "execution_target"
          ? { ...current, selection: { ...current.selection, customRef } }
          : current,
      );
    },
  } as const;
}

export type TaskInitiatingActionController = ReturnType<typeof useTaskInitiatingActionController>;
