import { MutationObserver, QueryObserver, type QueryClient, type QueryObserverResult } from "@tanstack/react-query";
import { useAtomMount, useAtomSet, useAtomValue } from "@effect/atom-react";
import * as Effect from "effect/Effect";
import * as Atom from "effect/unstable/reactivity/Atom";
import {
  workflowLabelMaxIDs,
  type ApiService,
  type ProjectLabelCatalog,
  type TaskLabelAssignment,
} from "@/api";
import { queryAtom, queryKeys, type QuerySnapshot } from "@/app-facade";
import { patchExistingTaskLabelAssignment, patchExistingTaskLabelProjections } from "./taskLabelCache";

export type TaskLabelAssignmentFailure = Readonly<{
  labelID: string;
  desiredSelected: boolean;
  error: unknown;
}>;
export type TaskLabelAssignmentData = Readonly<{
  selectedLabelIDs: readonly string[];
  pendingLabelIDs: readonly string[];
  failures: readonly TaskLabelAssignmentFailure[];
  isPending: boolean;
  error: Error | null;
  retryLoad(): void;
  setSelected(labelID: string, selected: boolean): void;
  retry(labelID: string): void;
}>;
type AssignmentIntent = Readonly<{ labelID: string; desiredSelected: boolean }>;
type LocalAssignmentState = Readonly<{
  pending: ReadonlyMap<string, boolean>;
  failures: ReadonlyMap<string, TaskLabelAssignmentFailure>;
}>;
type AssignmentBasis = Readonly<{ base: readonly string[]; available: ReadonlySet<string> }>;

export function createTaskLabelAssignmentModel({
  api,
  catalog,
  client,
  projectID,
  taskID,
  scheduleCatalogRefresh,
  scheduleTaskAssignmentRefresh,
}: Readonly<{
  api: Pick<ApiService, "getTaskLabels" | "updateTaskLabels">;
  catalog: Atom.Atom<QuerySnapshot<QueryObserverResult<ProjectLabelCatalog>>>;
  client: QueryClient;
  projectID: string;
  taskID: string;
  scheduleCatalogRefresh(): void;
  scheduleTaskAssignmentRefresh(taskID: string): void;
}>) {
  const assignmentKey = queryKeys.taskLabels(taskID);
  const catalogKey = queryKeys.projectLabels(projectID);
  const readAssignment = () => client.getQueryData<TaskLabelAssignment>(assignmentKey);
  const readCatalog = () => {
    const catalog = client.getQueryData<ProjectLabelCatalog>(catalogKey);
    if (catalog !== undefined && catalog.projectID !== projectID) {
      throw new Error(
        `Project label catalog for ${catalog.projectID} cannot serve Task ${taskID} in Project ${projectID}.`,
      );
    }
    return catalog;
  };
  const basis = (): AssignmentBasis | null => {
    const assignment = readAssignment();
    const catalog = readCatalog();
    return assignment === undefined || catalog === undefined
      ? null
      : { base: assignment.labelIDs, available: new Set(catalog.labels.map((label) => label.id)) };
  };
  const observer = new QueryObserver(client, {
    queryKey: assignmentKey,
    retry: false,
    queryFn: async () => {
      const loaded = await api.getTaskLabels(taskID);
      assertTaskAssignment(loaded, taskID);
      return loaded;
    },
  });
  const assignment = queryAtom(observer);
  const mutation = new MutationObserver(client, {
    mutationFn: async (intent: AssignmentIntent) => {
      const result = await api.updateTaskLabels(
        taskID,
        intent.desiredSelected ? [intent.labelID] : [],
        intent.desiredSelected ? [] : [intent.labelID],
      );
      assertTaskAssignment(result, taskID);
      return result;
    },
    async onSuccess(response) {
      if (readAssignment() === undefined) return;
      await client.cancelQueries({ queryKey: assignmentKey, exact: true }, { revert: false, silent: true });
      if (readAssignment() === undefined) return;
      const current = basis();
      if (current !== null) {
        const installed = response.labelIDs.filter((id) => current.available.has(id));
        patchExistingTaskLabelAssignment(client, { taskID, labelIDs: installed });
        patchExistingTaskLabelProjections(client, taskID, installed);
      } else {
        scheduleCatalogRefresh();
      }
      void client.invalidateQueries({
        queryKey: queryKeys.projectTaskListsRoot(projectID),
        refetchType: "active",
      });
      scheduleTaskAssignmentRefresh(taskID);
    },
  });
  const request = queryAtom(mutation);
  const local = Atom.make<LocalAssignmentState>({ pending: new Map(), failures: new Map() });
  const inFlight = (): AssignmentIntent | null => {
    const result = mutation.getCurrentResult();
    return result.isPending ? result.variables : null;
  };
  const save = (get: Atom.FnContext, next: LocalAssignmentState) => {
    const previous = get(local);
    if (!mapsEqual(previous.pending, next.pending) || !mapsEqual(previous.failures, next.failures))
      get.set(local, next);
  };
  const drain = Atom.fn<undefined>()(
    (_, get) =>
      Effect.gen(function* () {
        if (mutation.getCurrentResult().isPending) return;
        while (readAssignment() !== undefined) {
          const current = basis();
          const pending = get(local);
          const prepared = current === null ? pending : prepareNext(pending, current, null);
          save(get, prepared);
          const next = prepared.pending.entries().next();
          if (next.done) return;
          const [labelID, desiredSelected] = next.value;
          const intent = { labelID, desiredSelected };
          const outcome = yield* Effect.tryPromise(async () => mutation.mutate(intent)).pipe(
            Effect.match({
              onSuccess: () => ({ kind: "success" }) as const,
              onFailure: (error) => ({ kind: "failure", error: error.cause }) as const,
            }),
          );
          const currentLocal = get(local);
          const remaining = new Map(currentLocal.pending);
          const failures = new Map(currentLocal.failures);
          if (remaining.get(labelID) === desiredSelected) {
            remaining.delete(labelID);
            if (outcome.kind === "failure") failures.set(labelID, { ...intent, error: outcome.error });
          }
          const updated = { pending: remaining, failures };
          const nextBasis = basis();
          save(get, nextBasis === null ? updated : prepareNext(updated, nextBasis, null));
          if (nextBasis === null) return;
        }
        save(get, { pending: new Map(), failures: new Map() });
      }),
    { concurrent: true },
  );
  const start = (get: Atom.FnContext) => {
    if (!get(drain).waiting) get.set(drain, undefined);
  };
  const observation = Atom.make((get) => {
    const reconcile = () => {
      if (readAssignment() === undefined) {
        save(get, { pending: new Map(), failures: new Map() });
        return;
      }
      const current = basis();
      if (current === null) return;
      patchExistingTaskLabelProjections(
        client,
        taskID,
        current.base.filter((id) => current.available.has(id)),
      );
      save(get, prepareNext(get(local), current, inFlight()));
      if (get(local).pending.size > 0) start(get);
    };
    get.subscribe(assignment, reconcile);
    get.subscribe(catalog, reconcile);
    reconcile();
    return null;
  });
  const setSelected = Atom.fn<AssignmentIntent>()(
    (intent, get) =>
      Effect.sync(() => {
        const current = readAssignment();
        if (current === undefined) return;
        const currentBasis = basis();
        if (currentBasis === null && !intent.desiredSelected) return;
        if (currentBasis !== null && !currentBasis.available.has(intent.labelID)) return;
        const state = get(local);
        const available =
          currentBasis?.available ??
          new Set([...state.pending.keys(), ...state.failures.keys(), intent.labelID]);
        const pending = new Map(state.pending);
        if (!pending.has(intent.labelID) && pending.size >= workflowLabelMaxIDs) {
          throw new Error(
            `Task label assignment pending intents exceeded the ${String(workflowLabelMaxIDs)}-Label Project bound.`,
          );
        }
        pending.set(intent.labelID, intent.desiredSelected);
        const failures = new Map(state.failures);
        failures.delete(intent.labelID);
        save(get, prepareNext({ pending, failures }, { base: current.labelIDs, available }, inFlight()));
        start(get);
      }),
    { concurrent: true },
  );
  const retry = Atom.fn<string>()((labelID, get) =>
    Effect.sync(() => {
      const failed = get(local).failures.get(labelID);
      if (failed !== undefined && basis()?.available.has(labelID)) get.set(setSelected, failed);
    }),
  );
  const state = Atom.make((get) => {
    const data = get(assignment);
    const available = new Set(get(catalog).data?.labels.map((label) => label.id) ?? []);
    const inputs = get(local);
    return {
      selectedLabelIDs: visibleLabelIDs(data.data?.labelIDs ?? [], inputs.pending, available),
      pendingLabelIDs: [...inputs.pending.keys()].filter((id) => available.has(id)),
      failures: [...inputs.failures.values()].sort((a, b) => a.labelID.localeCompare(b.labelID)),
      isPending: data.isPending,
      error: data.isError ? data.error : null,
    };
  });
  const retryLoad = Atom.fn(() => Effect.promise(async () => observer.refetch()));
  return { state, request, observation, drain, setSelected, retry, retryLoad } as const;
}

export type TaskLabelAssignmentModel = ReturnType<typeof createTaskLabelAssignmentModel>;

export function useTaskLabelAssignmentModel(model: TaskLabelAssignmentModel): TaskLabelAssignmentData {
  useAtomMount(model.request);
  useAtomMount(model.observation);
  useAtomMount(model.drain);
  const setSelected = useAtomSet(model.setSelected, { mode: "value" });
  return {
    ...useAtomValue(model.state),
    setSelected: (labelID, desiredSelected) => { setSelected({ labelID, desiredSelected }); },
    retry: useAtomSet(model.retry, { mode: "value" }),
    retryLoad: useAtomSet(model.retryLoad, { mode: "value" }),
  };
}

function prepareNext(
  state: LocalAssignmentState,
  basis: AssignmentBasis,
  inFlight: AssignmentIntent | null,
): LocalAssignmentState {
  const base = new Set(basis.base);
  const pending = new Map(
    [...state.pending].filter(
      ([id, selected]) => basis.available.has(id) && (id === inFlight?.labelID || base.has(id) !== selected),
    ),
  );
  const failures = new Map([...state.failures].filter(([id]) => basis.available.has(id)));
  return mapsEqual(pending, state.pending) && mapsEqual(failures, state.failures)
    ? state
    : { pending, failures };
}

function visibleLabelIDs(
  authoritative: readonly string[],
  pending: ReadonlyMap<string, boolean>,
  available: ReadonlySet<string>,
): readonly string[] {
  const visible = new Set(authoritative.filter((id) => available.has(id)));
  for (const [id, selected] of pending) {
    if (!available.has(id)) continue;
    if (selected) visible.add(id);
    else visible.delete(id);
  }
  return [...visible];
}

function mapsEqual<K, V>(left: ReadonlyMap<K, V>, right: ReadonlyMap<K, V>): boolean {
  if (left.size !== right.size) return false;
  for (const [key, value] of left) if (right.get(key) !== value) return false;
  return true;
}

function assertTaskAssignment(assignment: TaskLabelAssignment, taskID: string): void {
  if (assignment.taskID !== taskID)
    throw new Error(
      `Task label assignment response for Task ${assignment.taskID} cannot update Task ${taskID}.`,
    );
}
