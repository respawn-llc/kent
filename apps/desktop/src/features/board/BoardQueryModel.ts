import { QueryObserver, type QueryClient } from "@tanstack/react-query";
import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";
import {
  canonicalBoardFilter,
  defaultBoardNodeCardsSort,
  type ApiService,
  type BoardNodeCardsSort,
  type TaskLabelFilter,
  type WorkflowBoard,
} from "@/api";
import { queryAtom, queryKeys, retainQueryData, type RetainedQueryData } from "@/app-facade";

export type BoardQueryInputs = Readonly<{ labelFilter: TaskLabelFilter; queriesEnabled: boolean }>;

export function createBoardQueryModel(initial: BoardQueryInputs) {
  const inputs = Atom.make(initial);
  const dependency = Atom.make<boolean | null>(null);
  const sort = Atom.make<BoardNodeCardsSort>(defaultBoardNodeCardsSort);
  const state = Atom.make((get) => ({
    filter: canonicalBoardFilter({ dependencyFilter: get(dependency), labelFilter: get(inputs).labelFilter }),
    queriesEnabled: get(inputs).queriesEnabled,
    sort: get(sort),
  }));
  const setDependencyFilter = Atom.fn<boolean | null>()((value, get) =>
    Effect.sync(() => {
      get.set(dependency, value);
    }),
  );
  const setSort = Atom.fn<BoardNodeCardsSort>()((value, get) =>
    Effect.sync(() => {
      get.set(sort, value);
    }),
  );
  return { inputs, state, setDependencyFilter, setSort } as const;
}

export function createBoardRead(
  api: ApiService,
  client: QueryClient,
  scope: ReturnType<typeof createBoardQueryModel>,
  { projectID, workflowID }: Readonly<{ projectID: string; workflowID: string | undefined }>,
) {
  const options = (input: Atom.Type<typeof scope.state>) => ({
    queryKey: queryKeys.board(projectID, workflowID, input.filter),
    queryFn: async () => api.getBoard(projectID, workflowID, input.filter),
    enabled: input.queriesEnabled && projectID.trim().length > 0,
    gcTime: 0,
    placeholderData: (previous: WorkflowBoard | undefined) => previous,
  });
  const observer = new QueryObserver<WorkflowBoard, Error>(client, {
    ...options({
      filter: canonicalBoardFilter({ kind: "none" }),
      sort: defaultBoardNodeCardsSort,
      queriesEnabled: false,
    }),
    enabled: false,
  });
  const configuration = Atom.make((get) => {
    observer.setOptions(options(get(scope.state)));
  });
  const request = queryAtom(observer);
  const retained = Atom.make<RetainedQueryData<
    WorkflowBoard,
    Readonly<{ projectID: string; workflowID: string | undefined }>
  > | null>(null);
  const state = Atom.make((get) => {
    get.mount(retained);
    get(configuration);
    const current = get(request);
    const previous = get.once(retained);
    const next = retainQueryData(
      previous,
      { scope: { projectID, workflowID }, data: current.data, retain: true },
      (a, b) => a.projectID === b.projectID && a.workflowID === b.workflowID,
    );
    if (previous !== next.retained) get.set(retained, next.retained);
    return { ...current, data: next.data, isPending: current.isPending && next.data === undefined };
  });
  const retry = Atom.fn(
    () =>
      Effect.promise(async () => {
        const current = observer.getCurrentResult();
        if (current.isEnabled && !current.isFetching) await observer.refetch();
      }),
    { concurrent: true },
  );
  return { state, retry } as const;
}
