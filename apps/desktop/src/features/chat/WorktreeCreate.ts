import { useAtomMount, useAtomSet } from "@effect/atom-react";
import { MutationObserver, QueryObserver, type QueryClient } from "@tanstack/react-query";
import * as Effect from "effect/Effect";
import * as Atom from "effect/unstable/reactivity/Atom";
import type { TFunction } from "i18next";

import {
  CreateTargetResolutionKind,
  newSetupOperationID,
  WorktreeError,
  type ApiService,
  type WorktreeCreateInput,
  type WorktreeSwitch,
} from "@/api";
import {
  createWorktreeTargetResolutionRequest,
  disposeWorktreeCreateTargetResolution,
  freshFetchWorktreeCreateTargetResolution,
  worktreeCreateTargetResolutionQueryOptions,
  queryAtom,
  type SidebarPageNavigator,
  type StatusController,
} from "@/app-facade";
import { worktreeErrorMessage } from "./worktreeErrorMessage";

export function createWorktreeCreate({
  client,
  api,
  sessionID,
  suggestion,
  t,
  submitSwitch,
  push,
  navigator,
  refreshOpenWorktreeList,
}: Readonly<{
  client: QueryClient;
  api: ApiService;
  sessionID: string;
  suggestion: string | undefined;
  navigator: SidebarPageNavigator;
  refreshOpenWorktreeList(sessionID: string): void;
  submitSwitch(operation: WorktreeSwitch): void;
  push: StatusController["push"];
  t: TFunction;
}>) {
  const target = Atom.make(suggestion ?? "");
  const trimmedTarget = Atom.make((get) => get(target).trim());
  const base = Atom.make("HEAD");
  const intent = Atom.make<string | null>(null);
  const resolver = Atom.make((get) => {
    const value = get(trimmedTarget);
    if (value.length === 0) return null;
    const request = createWorktreeTargetResolutionRequest(sessionID, value);
    void client.resetQueries({
      queryKey: worktreeCreateTargetResolutionQueryOptions(api, request).queryKey,
      exact: true,
    });
    const observer = new QueryObserver(client, {
      ...worktreeCreateTargetResolutionQueryOptions(api, request),
      enabled: false,
    });
    return { observer, snapshot: queryAtom(observer) };
  });
  const resolution = Atom.make((get) => {
    const request = get(resolver);
    return request === null ? null : get(request.snapshot);
  });
  const resolvedAuthority = Atom.make((get) => {
    const result = get(resolution);
    return result?.isSuccess ? result.data.resolution : undefined;
  });
  const creator = new MutationObserver(client, {
    mutationFn: async (input: WorktreeCreateInput) => api.createWorktree(input),
    retry: false,
    networkMode: "always",
    onSuccess: (result) => {
      const operation = result.worktree?.projection?.switch;
      if (operation === undefined) throw new Error("Created worktree Switch authority is required");
      submitSwitch(operation);
    },
    onError: (error) => {
      if (!(error instanceof WorktreeError) || error.detail.kind !== "setup_retained") return;
      push({
        id: crypto.randomUUID(),
        tone: "danger",
        title: t("chat.worktree.create"),
        body: error.detail.details.diagnostic,
      });
      if (navigator.replace({ kind: "worktree", sessionID, page: "list" }) !== "accepted") {
        refreshOpenWorktreeList(sessionID);
      }
    },
  });
  const creation = queryAtom(creator);
  const submit = Atom.fn<undefined>()(
    (_, get) =>
      Effect.gen(function* () {
        if (creator.getCurrentResult().isPending) return;
        const value = get(trimmedTarget);
        get.set(intent, value);
        const result = get(resolver)?.observer.getCurrentResult();
        if (value.length === 0 || !result?.isSuccess) return;
        const authority = result.data.resolution;
        if (authority === undefined) throw new Error("Create resolution is required");
        const baseRef =
          authority.kind === CreateTargetResolutionKind.WORKTREE_CREATE_TARGET_RESOLUTION_KIND_NEW_BRANCH
            ? get(base).trim()
            : null;
        if (baseRef === "") return;
        get.set(intent, null);
        yield* Effect.tryPromise(async () =>
          creator.mutate({
            sessionID,
            resolution: authority,
            baseRef,
            setupOperationID: newSetupOperationID(),
          }),
        ).pipe(Effect.ignore);
      }),
    { concurrent: true },
  );
  const resolve = Atom.make((get) => {
    const value = get(trimmedTarget);
    return Effect.gen(function* () {
      if (value.length === 0) return;
      const request = createWorktreeTargetResolutionRequest(sessionID, value);
      yield* Effect.addFinalizer(() =>
        Effect.promise(async () => disposeWorktreeCreateTargetResolution(client, request)),
      );
      yield* Effect.sleep(150);
      yield* Effect.tryPromise(async () => freshFetchWorktreeCreateTargetResolution(client, api, request));
      if (get.once(intent) === value) get.set(submit, undefined);
    }).pipe(Effect.ignore);
  });
  const editTarget = Atom.fn<string>()(
    (value, get) =>
      Effect.sync(() => {
        if (creator.getCurrentResult().isPending) return;
        get.set(intent, null);
        creator.reset();
        get.set(target, value);
      }),
    { concurrent: true },
  );
  const editBase = Atom.fn<string>()(
    (value, get) =>
      Effect.sync(() => {
        if (creator.getCurrentResult().isPending) return;
        get.set(intent, null);
        creator.reset();
        get.set(base, value);
      }),
    { concurrent: true },
  );
  const state = Atom.make((get) => {
    const value = get(target);
    const baseRef = get(base);
    const result = get(resolution);
    const created = get(creation);
    const authority = get(resolvedAuthority);
    const isNew =
      authority?.kind === CreateTargetResolutionKind.WORKTREE_CREATE_TARGET_RESOLUTION_KIND_NEW_BRANCH;
    const errors = creationErrors(
      created.error,
      get(intent) === "",
      get(intent) !== null && isNew && baseRef.trim().length === 0,
      t,
    );
    return {
      target: value,
      base: baseRef,
      classification: authority?.kind,
      pending: created.isPending,
      resolving: value.trim().length > 0 && authority === undefined && !result?.isError,
      ...errors,
      targetError:
        errors.targetError ?? (result?.isError ? worktreeErrorMessage(result.error, t) : undefined),
    };
  });
  return { state, resolution, creation, resolve, submit, editTarget, editBase };
}

function creationErrors(error: Error | null, targetRequired: boolean, baseRequired: boolean, t: TFunction) {
  const result: Readonly<{
    targetError: string | undefined;
    baseError: string | undefined;
    formError: string | undefined;
  }> = {
    targetError: targetRequired ? t("chat.worktree.targetRequired") : undefined,
    baseError: baseRequired ? t("chat.worktree.baseRequired") : undefined,
    formError: undefined,
  };
  if (error === null) return result;
  if (error instanceof WorktreeError) {
    if (error.detail.kind === "setup_retained") return result;
    if (error.detail.kind === "create" && error.detail.owner === "base_ref") {
      return { ...result, baseError: error.detail.diagnostic };
    }
  }
  return { ...result, formError: worktreeErrorMessage(error, t) };
}

export function useWorktreeCreate(model: ReturnType<typeof createWorktreeCreate>) {
  useAtomMount(model.resolution);
  useAtomMount(model.creation);
  useAtomMount(model.resolve);
  return {
    submit: useAtomSet(model.submit, { mode: "value" }),
    editTarget: useAtomSet(model.editTarget, { mode: "value" }),
    editBase: useAtomSet(model.editBase, { mode: "value" }),
  };
}
