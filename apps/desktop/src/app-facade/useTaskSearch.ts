import {
  InfiniteQueryObserver,
  useQueryClient,
  type InfiniteData,
  type QueryClient,
} from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { useAtomMount, useAtomSet, useAtomValue } from "@effect/atom-react";
import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";

import { TaskSearchError, type ApiService, type TaskSearchGroup, type TaskSearchResponse } from "@/api";
import { queryKeys } from "./queryKeys";
import { useAppServices } from "./useAppServices";
import { retainQueryData, type RetainedQueryData } from "./useRetainedQueryData";
import { queryAtom } from "./queryAtom";

export const taskSearchDebounceMs = 300;
const taskSearchPageSize = 40;
const retainedTaskSearchPages = 3;
const taskSearchContext = 20;

type SearchPage = Readonly<{
  offset: number | null;
  projectID: string | null;
  query: string;
  response: TaskSearchResponse;
}>;

export type TaskSearchResult = Readonly<{
  key: string;
  group: TaskSearchGroup;
}>;

type SearchData = InfiniteData<SearchPage, number | null>;
type SearchScope = Readonly<{ projectID: string | null }>;

export function useTaskSearch(projectID: string | null, open: boolean, query: string) {
  const { api } = useAppServices();
  const client = useQueryClient();
  const debouncedQuery = useSearchDebounce(query);
  const [retained] = useState(() => Atom.make<RetainedQueryData<SearchData, SearchScope> | null>(null));
  useAtomMount(retained);
  const model = useMemo(
    () => createTaskSearchModel({ api, client, projectID, open, debouncedQuery, retained }),
    [api, client, projectID, open, debouncedQuery, retained],
  );
  const state = useAtomValue(model.state);
  const refetch = useAtomSet(model.retry, { mode: "value" });
  const fetchNextPage = useAtomSet(model.nextPage, { mode: "value" });
  return { ...state, request: { ...state.request, refetch, fetchNextPage } };
}

function useSearchDebounce(value: string): string {
  const [text] = useState(() => Atom.make(value));
  const timing = useMemo(
    () =>
      Atom.make((get) =>
        Effect.gen(function* () {
          yield* Effect.sleep(taskSearchDebounceMs);
          get.set(text, value);
        }),
      ),
    [text, value],
  );
  useAtomMount(timing);
  return useAtomValue(text);
}

function createTaskSearchModel({
  api,
  client,
  projectID,
  open,
  debouncedQuery,
  retained,
}: Readonly<{
  api: ApiService;
  client: QueryClient;
  projectID: string | null;
  open: boolean;
  debouncedQuery: string;
  retained: Atom.Writable<RetainedQueryData<SearchData, SearchScope> | null>;
}>) {
  const trimmedQuery = debouncedQuery.trim();
  const searchable = Array.from(trimmedQuery).length >= 3;
  const observer = new InfiniteQueryObserver<
    SearchPage,
    Error,
    InfiniteData<SearchPage, number | null>,
    readonly (string | null)[],
    number | null
  >(client, {
    queryKey: queryKeys.taskSearch(projectID, trimmedQuery),
    queryFn: async ({ pageParam, signal }) => ({
      offset: pageParam,
      projectID,
      query: trimmedQuery,
      response: await api.searchTasks(
        {
          mode: "literal",
          query: trimmedQuery,
          context: taskSearchContext,
          caseSensitive: false,
          includeComments: true,
          projectIDs: projectID === null ? undefined : [projectID],
          pageSize: taskSearchPageSize,
          offset: pageParam ?? undefined,
        },
        signal,
      ),
    }),
    initialPageParam: null,
    enabled: open && searchable,
    getNextPageParam: (lastPage) => lastPage.response.nextOffset ?? undefined,
    maxPages: retainedTaskSearchPages,
  });
  const request = open ? queryAtom(observer) : Atom.make(observer.getCurrentResult());
  const state = Atom.make((get) => {
    const current = get(request);
    const previous = get.once(retained);
    const next = retainQueryData(
      previous,
      { scope: { projectID }, data: current.data, retain: true },
      sameTaskSearchProject,
    );
    if (next.retained !== previous) get.set(retained, next.retained);
    const normalizedTooShort = current.error instanceof TaskSearchError;
    const visible = searchable && !normalizedTooShort ? next.data : undefined;
    return {
      displayedQuery: visible?.pages[0]?.query ?? null,
      normalizedTooShort,
      paginationUsesVisibleData: visible !== undefined && visible === current.data,
      request: current,
      results: flattenSearchResults(visible),
      searchable,
    };
  });
  const nextPage = Atom.fn(
    (_, get) =>
      Effect.promise(async () => {
        const current = observer.getCurrentResult();
        if (open && get(state).paginationUsesVisibleData && current.hasNextPage && !current.isFetching) {
          await observer.fetchNextPage();
        }
      }),
    { concurrent: true },
  );
  const retry = Atom.fn(
    () =>
      Effect.promise(async () => {
        if (open && searchable && !observer.getCurrentResult().isFetching) await observer.refetch();
      }),
    { concurrent: true },
  );
  return { state, nextPage, retry } as const;
}

function flattenSearchResults(
  data: InfiniteData<SearchPage, number | null> | undefined,
): readonly TaskSearchResult[] {
  if (data === undefined) {
    return [];
  }
  return data.pages.flatMap((page) =>
    page.response.groups.map((group, groupIndex) => ({
      key: taskSearchResultKey(page, group, groupIndex),
      group,
    })),
  );
}

function taskSearchResultKey(page: SearchPage, group: TaskSearchGroup, groupIndex: number): string {
  const firstOrdinal = group.hits[0]?.ordinal;
  if (firstOrdinal === undefined) {
    throw new Error(`Task Search group ${group.taskID} at offset ${String(page.offset)} has no hits.`);
  }
  return JSON.stringify([page.projectID, page.query, page.offset, groupIndex, group.taskID, firstOrdinal]);
}

function sameTaskSearchProject(
  left: Readonly<{ projectID: string | null }>,
  right: Readonly<{ projectID: string | null }>,
): boolean {
  return left.projectID === right.projectID;
}
