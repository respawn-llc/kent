import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";
import type { ProjectLabelCatalog } from "@/api";
import type { AppStorageNamespace, BrowserStorageError, QuerySnapshot } from "@/app-facade";
import type { QueryObserverResult } from "@tanstack/react-query";
import { readPersistedLabelFilterState, writePersistedLabelFilterState } from "./labelFilterPersistence";
import {
  createLabelFilterState,
  reconcileLabelFilterState,
  reduceLabelFilterState,
  type LabelFilterAction,
  type LabelFilterState,
} from "./labelFilterState";

export class LabelFilterStorageNamespaceError extends Error {
  constructor() {
    super("Label filter storage is unavailable because startup did not resolve a storage namespace.");
    this.name = "LabelFilterStorageNamespaceError";
  }
}

export type LabelFilterPersistenceStatus =
  | Readonly<{ status: "loading" }>
  | Readonly<{ status: "ready" }>
  | Readonly<{ status: "error"; error: BrowserStorageError | LabelFilterStorageNamespaceError }>;

export type ProjectLabelFilterController = Readonly<{
  state: LabelFilterState;
  persistence: LabelFilterPersistenceStatus;
  dispatch(action: LabelFilterAction): void;
}>;

type FilterState = Readonly<{ state: LabelFilterState; persistence: LabelFilterPersistenceStatus }>;

export function createProjectLabelFilter({
  catalog,
  projectID,
  namespace,
  report,
}: Readonly<{
  catalog: Atom.Atom<QuerySnapshot<QueryObserverResult<ProjectLabelCatalog>>>;
  projectID: string;
  namespace: AppStorageNamespace | null;
  report: (error: unknown) => void;
}>) {
  const local = Atom.make<FilterState | null>(null);
  const empty: FilterState = { state: createLabelFilterState(), persistence: { status: "loading" } };
  const failed = (
    state: LabelFilterState,
    error: BrowserStorageError | LabelFilterStorageNamespaceError,
  ): FilterState => {
    report(error);
    return { state, persistence: { status: "error", error } };
  };
  const persist = (state: LabelFilterState): FilterState => {
    if (namespace === null) return failed(state, new LabelFilterStorageNamespaceError());
    const result = writePersistedLabelFilterState(namespace, projectID, state);
    return result.ok ? { state, persistence: { status: "ready" } } : failed(state, result.error);
  };
  const state = Atom.make((get): FilterState => {
    const current = get(local);
    const data = get(catalog).data;
    if (data === undefined) return current ?? empty;
    const ids = data.labels.map((label) => label.id);
    if (current === null) {
      const restored = namespace === null ? null : readPersistedLabelFilterState(namespace, projectID, ids);
      const initial: FilterState =
        restored === null
          ? failed(createLabelFilterState(), new LabelFilterStorageNamespaceError())
          : restored.ok
            ? { state: restored.state, persistence: { status: "ready" } }
            : failed(restored.state, restored.error);
      get.set(local, initial);
      return initial;
    }
    const reconciled = reconcileLabelFilterState(current.state, ids);
    if (reconciled === current.state) return current;
    const next = persist(reconciled);
    get.set(local, next);
    return next;
  });
  const dispatch = Atom.fn((action: LabelFilterAction, get) =>
    Effect.sync(() => {
      const current = get(state);
      get.set(local, persist(reduceLabelFilterState(current.state, action)));
    }),
  );
  return { state, dispatch } as const;
}
