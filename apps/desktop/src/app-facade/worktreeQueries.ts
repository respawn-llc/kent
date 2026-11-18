import { queryOptions, skipToken, type QueryClient } from "@tanstack/react-query";

import type { ApiService, WorktreeScope } from "@/api";
import { queryKeys, type WorktreeListReadOwner } from "./queryKeys";

export type WorktreeCreateTargetResolutionRequest = Readonly<{
  scope: WorktreeScope;
  target: string;
}>;
export type WorktreeSelectorRequest = Readonly<{ sessionID: string; selector: string }>;
export type WorktreeDeleteRequest = Readonly<{ scope: WorktreeScope; selector: string }>;

export function createWorktreeTargetResolutionRequest(
  scope: WorktreeScope,
  rawTarget: string,
): WorktreeCreateTargetResolutionRequest {
  const request = Object.freeze({ scope, target: rawTarget.trim() });
  createKey(request);
  return request;
}

export function createWorktreeSelectorRequest(sessionID: string, selector: string): WorktreeSelectorRequest {
  const request = Object.freeze({ sessionID, selector });
  selectorKey(request);
  return request;
}

export async function invalidateWorktreeSessionReads(
  queryClient: QueryClient,
  sessionID: string,
): Promise<void> {
  await Promise.all(
    [queryKeys.worktreeStatus(sessionID), queryKeys.worktreeList({ kind: "session", sessionID })].map(
      async (queryKey) => queryClient.invalidateQueries({ queryKey, exact: true, refetchType: "active" }),
    ),
  );
}

export const worktreeStatusQueryOptions = (api: ApiService, sessionID: string) =>
  queryOptions({
    queryKey: queryKeys.worktreeStatus(sessionID),
    queryFn: async () => api.getWorktreeStatus(sessionID),
  });

export const worktreeListQueryOptions = (
  api: ApiService,
  scope: WorktreeScope | null,
  owner: WorktreeListReadOwner = "sidebar",
) =>
  queryOptions({
    queryKey:
      scope === null ? (["worktree", null, "list", owner] as const) : queryKeys.worktreeList(scope, owner),
    queryFn: scope === null ? skipToken : async () => api.listWorktrees(scope),
    staleTime: 0,
    structuralSharing: false,
    retry: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });

export async function replaceWorktreeListRead(
  queryClient: QueryClient,
  api: ApiService,
  scope: WorktreeScope,
  owner: WorktreeListReadOwner = "sidebar",
): Promise<void> {
  const options = worktreeListQueryOptions(api, scope, owner);
  await queryClient.cancelQueries({ queryKey: options.queryKey, exact: true });
  // The query retains the error and previous data for the browser's Error + Retry state.
  await queryClient.fetchQuery(options).catch(() => undefined);
}

const createKey = (request: WorktreeCreateTargetResolutionRequest) =>
  ["worktree", request.scope, "create-target-resolution", request.target] as const;
const selectorKey = (request: WorktreeSelectorRequest) =>
  queryKeys.worktreeSelectorResolution(request.sessionID, request.selector);
const deleteKey = (request: WorktreeDeleteRequest) =>
  ["worktree", request.scope, "delete-preview", request.selector] as const;

const createOperation = operation(createKey, async (api, request: WorktreeCreateTargetResolutionRequest) =>
  api.resolveWorktreeCreateTarget(request.scope, request.target),
);
const selectorOperation = operation(selectorKey, async (api, request: WorktreeSelectorRequest) =>
  api.resolveWorktreeSelector(request.sessionID, request.selector),
);
const deleteOperation = operation(deleteKey, async (api, request: WorktreeDeleteRequest) =>
  api.previewWorktreeDelete(request.scope, request.selector),
);
export const worktreeCreateTargetResolutionQueryOptions = createOperation.options;
export const freshFetchWorktreeCreateTargetResolution = createOperation.fresh;
export const disposeWorktreeCreateTargetResolution = createOperation.dispose;
export const worktreeSelectorResolutionQueryOptions = selectorOperation.options;
export const freshFetchWorktreeSelectorResolution = selectorOperation.fresh;
export const disposeWorktreeSelectorResolution = selectorOperation.dispose;
export const worktreeDeletePreviewQueryOptions = deleteOperation.options;
export const freshFetchWorktreeDeletePreview = deleteOperation.fresh;
export const disposeWorktreeDeletePreview = deleteOperation.dispose;

function operation<Request, Result extends NonNullable<unknown>>(
  key: (request: Request) => readonly unknown[],
  query: (api: ApiService, request: Request) => Promise<Result>,
) {
  const options = (api: ApiService, request: Request) =>
    transient(key(request), async () => query(api, request));
  return {
    options,
    fresh: async (queryClient: QueryClient, api: ApiService, request: Request) =>
      fresh(queryClient, options(api, request)),
    dispose: async (queryClient: QueryClient, request: Request) => dispose(queryClient, key(request)),
  };
}
const transient = <T>(queryKey: readonly unknown[], queryFn: () => Promise<T>) =>
  queryOptions({ queryKey, queryFn, refetchOnReconnect: false, retry: false });
async function fresh<T>(queryClient: QueryClient, options: ReturnType<typeof queryOptions<T>>) {
  await queryClient.resetQueries({ queryKey: options.queryKey, exact: true });
  return queryClient.fetchQuery(options);
}
async function dispose(queryClient: QueryClient, queryKey: readonly unknown[]) {
  await queryClient.cancelQueries({ queryKey, exact: true }, { revert: true, silent: true });
  queryClient.removeQueries({ queryKey, exact: true });
}
