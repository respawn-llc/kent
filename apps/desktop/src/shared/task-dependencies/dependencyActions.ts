import { useMemo } from "react";
import { useQueryClient, MutationObserver, type QueryClient } from "@tanstack/react-query";
import { useAtomMount, useAtomSet, useAtomValue } from "@effect/atom-react";
import { useTranslation } from "react-i18next";
import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";
import type { TaskDependencyDirection, TaskDependencyItem, TaskDetail } from "@/api";
import { errorMessage } from "@/api";
import {
  invalidateProjectBoardQueries,
  mutationPendingAtom,
  queryAtom,
  queryKeys,
  useAppServices,
  useStatusController,
  type TaskSearchResult,
  type AppServices,
} from "@/app-facade";
import { optimisticTaskDependencyRemoval, type TaskDependencyPair } from "./dependencyCache";

export type DependencyInteraction =
  | Readonly<{ kind: "persisted"; taskID: string; onChanged?: (() => void) | undefined }>
  | Readonly<{
      kind: "prepared";
      onRemove(direction: TaskDependencyDirection, item: TaskDependencyItem): void;
      onSelect(direction: TaskDependencyDirection, result: TaskSearchResult): void;
    }>;
type Submission = Readonly<{
  kind: "add" | "remove";
  onSuccess?: (() => void) | undefined;
  onChanged?: (() => void) | undefined;
}>;

function createDependencyActions({
  api,
  client,
  projectID,
  taskID,
  pair,
  report,
}: Readonly<{
  api: AppServices["api"];
  client: QueryClient;
  projectID: string;
  taskID: string;
  pair: TaskDependencyPair;
  report(kind: Submission["kind"], error: unknown): void;
}>) {
  const mutationKey = ["task-dependency", pair.blockerTaskID, pair.blockedTaskID] as const;
  const observer = new MutationObserver(client, {
    mutationKey,
    mutationFn: async (input: Submission) =>
      input.kind === "add"
        ? api.addTaskDependency(pair.blockerTaskID, pair.blockedTaskID)
        : api.removeTaskDependency(pair.blockerTaskID, pair.blockedTaskID),
    async onMutate(input) {
      if (input.kind === "add") return { previous: null };
      await client.cancelQueries({ queryKey: queryKeys.task(taskID) });
      const previous = client.getQueryData<TaskDetail>(queryKeys.task(taskID)) ?? null;
      client.setQueryData<TaskDetail>(queryKeys.task(taskID), (current) =>
        current === undefined ? current : optimisticTaskDependencyRemoval(current, pair),
      );
      return { previous };
    },
    onError(error, input, context) {
      if (context?.previous != null) client.setQueryData(queryKeys.task(taskID), context.previous);
      report(input.kind, error);
    },
    async onSuccess(_response, input) {
      await Promise.all([
        client.invalidateQueries({ queryKey: queryKeys.task(pair.blockerTaskID) }),
        client.invalidateQueries({ queryKey: queryKeys.task(pair.blockedTaskID) }),
        invalidateProjectBoardQueries(client, projectID),
        client.invalidateQueries({ queryKey: queryKeys.projectTaskListsRoot(projectID) }),
      ]);
      input.onChanged?.();
      input.onSuccess?.();
    },
  });
  const filters = { mutationKey, exact: true };
  const pending = mutationPendingAtom(client, filters);
  const request = queryAtom(observer);
  const submit = Atom.fn<Submission>()(
    (input) =>
      Effect.gen(function* () {
        if (client.isMutating(filters) > 0) return;
        yield* Effect.tryPromise(async () => observer.mutate(input)).pipe(Effect.ignore);
      }),
    { concurrent: true },
  );
  return { request, pending, submit } as const;
}

export function useTaskDependencyActions(projectID: string, taskID: string, pair: TaskDependencyPair) {
  const { api } = useAppServices();
  const client = useQueryClient();
  const { t } = useTranslation();
  const { push } = useStatusController();
  const { blockerTaskID, blockedTaskID } = pair;
  const model = useMemo(
    () =>
      createDependencyActions({
        api,
        client,
        projectID,
        taskID,
        pair: { blockerTaskID, blockedTaskID },
        report(kind, error) {
          push({
            id: `task-dependency-${kind}-error`,
            body: errorMessage(error),
            tone: "danger",
            title: t(kind === "add" ? "task.dependenciesAddFailed" : "task.dependenciesRemoveFailed"),
            durationMs: kind === "remove" ? 5000 : Infinity,
          });
        },
      }),
    [api, client, projectID, taskID, blockerTaskID, blockedTaskID, t, push],
  );
  useAtomMount(model.request);
  const submit = useAtomSet(model.submit, { mode: "value" });
  return {
    pending: useAtomValue(model.pending),
    submit: (input: Submission) => {
      submit(input);
    },
  };
}
