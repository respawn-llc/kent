import { useAtomMount, useAtomSet } from "@effect/atom-react";
import { MutationObserver, QueryObserver, type QueryClient } from "@tanstack/react-query";
import * as Effect from "effect/Effect";
import * as Atom from "effect/reactivity/Atom";
import type { TFunction } from "i18next";

import {
  CreateTargetResolutionKind,
  newSetupOperationID,
  WorktreeError,
  type ApiService,
  type WorktreeCreateInput,
  type WorktreeSwitch,
  type WorktreeScope,
  type ChatMutationTarget,
} from "@/api";
import {
  createWorktreeTargetResolutionRequest,
  disposeWorktreeCreateTargetResolution,
  freshFetchWorktreeCreateTargetResolution,
  worktreeCreateTargetResolutionQueryOptions,
  queryAtom,
  type SidebarPageNavigator,
  type StatusController,
  type ChatWorktreeBinding,
} from "@/app-facade";
import { worktreeErrorMessage } from "./worktreeErrorMessage";

export function createWorktreeCreate({
  client,
  api,
  projectID,
  openSession,
  suggestion,
  t,
  submitSwitch,
  push,
  navigator,
  refreshOpenWorktreeList,
  chat,
}: Readonly<{
  client: QueryClient;
  api: ApiService;
  projectID: string;
  chat: ChatWorktreeBinding;
  openSession(sessionID: string): void | Promise<void>;
  suggestion: string | undefined;
  navigator: SidebarPageNavigator;
  refreshOpenWorktreeList(scope: WorktreeScope): void | Promise<void>;
  submitSwitch(operation: WorktreeSwitch): Promise<void>;
  push: StatusController["push"];
  t: TFunction;
}>) {
  const readyTarget = chat.creationTarget;
  const inputDraft = chat.draft;
  const managementScope = chat.scope;
  const target = Atom.make(suggestion ?? "");
  const trimmedTarget = Atom.make((get) => get(target).trim());
  const base = Atom.make("HEAD");
  const intent = Atom.make<string | null>(null);
  const resolver = Atom.make((get) => {
    const value = get(trimmedTarget);
    const selectedScope = get(managementScope);
    if (value.length === 0 || selectedScope === null) return null;
    const request = createWorktreeTargetResolutionRequest(selectedScope, value);
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
    mutationKey: chat.creationKey,
    mutationFn: async (
      request: Readonly<{
        input: Omit<WorktreeCreateInput, "sessionID">;
        target: ChatMutationTarget;
        draft: string;
        scope: WorktreeScope;
      }>,
    ) => {
      const target = request.target;
      const identity =
        target.kind === "session" ? target.sessionID : await api.chat.planNewChat(target, request.draft);
      if (target.kind === "new_chat") await chat.delivered(identity);
      const result = await api.createWorktree({ ...request.input, sessionID: identity });
      const operation = result.worktree?.projection?.switch;
      if (operation === undefined) throw new Error("Created worktree Switch authority is required");
      if (target.kind === "new_chat") {
        await api.switchWorktree(identity, operation);
      }
      return { operation, newChat: target.kind === "new_chat" };
    },
    retry: false,
    networkMode: "always",
    onSuccess: ({ operation, newChat }) => {
      if (newChat) {
        navigator.close();
        return;
      }
      void submitSwitch(operation);
    },
    onError: (error, request) => {
      if (
        creator.hasListeners() &&
        error instanceof WorktreeError &&
        error.detail.kind === "create" &&
        error.detail.owner === "base_ref"
      )
        return;
      push({
        id: crypto.randomUUID(),
        tone: "danger",
        title: t("chat.worktree.create"),
        body: worktreeErrorMessage(error, t),
      });
      if (
        error instanceof WorktreeError &&
        error.detail.kind === "setup_retained" &&
        navigator.replace({
          kind: "worktree",
          projectID,
          page: "list",
          openSession,
          chat,
        }) !== "accepted"
      ) {
        void refreshOpenWorktreeList(request.scope);
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
        if (authority.kind === CreateTargetResolutionKind.WORKTREE_CREATE_TARGET_RESOLUTION_KIND_INVALID)
          return;
        const baseRef =
          authority.kind === CreateTargetResolutionKind.WORKTREE_CREATE_TARGET_RESOLUTION_KIND_NEW_BRANCH
            ? get(base).trim()
            : null;
        if (baseRef === "") return;
        get.set(intent, null);
        const captured = get(readyTarget);
        const scope = get(managementScope);
        if (captured === null || scope === null) return;
        const draft = get(inputDraft);
        yield* Effect.tryPromise(async () =>
          creator.mutate({
            target: captured,
            draft,
            scope,
            input: {
              resolution: authority,
              baseRef,
              setupOperationID: newSetupOperationID(),
            },
          }),
        ).pipe(Effect.ignore);
      }),
    { concurrent: true },
  );
  const resolve = Atom.make((get) => {
    const value = get(trimmedTarget);
    const selectedScope = get(managementScope);
    return Effect.gen(function* () {
      if (value.length === 0 || selectedScope === null) return;
      const request = createWorktreeTargetResolutionRequest(selectedScope, value);
      yield* Effect.addFinalizer(() =>
        Effect.promise(async () => disposeWorktreeCreateTargetResolution(client, request)),
      );
      yield* Effect.sleep(150);
      yield* Effect.tryPromise(async () => freshFetchWorktreeCreateTargetResolution(client, api, request));
      if (get.once(intent) === value) get.set(submit, undefined);
    }).pipe(Effect.ignore);
  });
  const retryResolution = Atom.fn<undefined>()(
    (_, get) =>
      Effect.gen(function* () {
        get.set(intent, null);
        const request = get(resolver);
        if (request !== null) yield* Effect.promise(async () => request.observer.refetch());
      }),
    { concurrent: true },
  );
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
        authority?.kind === CreateTargetResolutionKind.WORKTREE_CREATE_TARGET_RESOLUTION_KIND_INVALID
          ? t("chat.worktree.targetInvalid")
          : errors.targetError,
    };
  });
  return { state, resolution, creation, resolve, retryResolution, submit, editTarget, editBase };
}

function creationErrors(error: Error | null, targetRequired: boolean, baseRequired: boolean, t: TFunction) {
  const result: Readonly<{
    targetError: string | undefined;
    baseError: string | undefined;
  }> = {
    targetError: targetRequired ? t("chat.worktree.targetRequired") : undefined,
    baseError: baseRequired ? t("chat.worktree.baseRequired") : undefined,
  };
  if (error === null) return result;
  if (error instanceof WorktreeError) {
    if (error.detail.kind === "setup_retained") return result;
    if (error.detail.kind === "create" && error.detail.owner === "base_ref") {
      return { ...result, baseError: error.detail.diagnostic };
    }
  }
  return result;
}

export function useWorktreeCreate(model: ReturnType<typeof createWorktreeCreate>) {
  useAtomMount(model.resolution);
  useAtomMount(model.creation);
  useAtomMount(model.resolve);
  return {
    submit: useAtomSet(model.submit, { mode: "value" }),
    editTarget: useAtomSet(model.editTarget, { mode: "value" }),
    editBase: useAtomSet(model.editBase, { mode: "value" }),
    retryResolution: useAtomSet(model.retryResolution, { mode: "value" }),
  };
}
