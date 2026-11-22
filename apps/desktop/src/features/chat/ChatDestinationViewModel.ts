import * as Atom from "effect/reactivity/Atom";
import * as Effect from "effect/Effect";
import { MutationObserver, type QueryClient } from "@tanstack/react-query";
import type { TFunction } from "i18next";
import {
  openNativeChat,
  queryAtom,
  mutationPendingAtom,
  type AppServices,
  type StatusController,
} from "@/app-facade";
import {
  errorMessage,
  type ChatNotAcceptedReason,
  type ChatSettingsTarget,
  type ChatMutationTarget,
  type WorkspaceCatalogRow,
} from "@/api";
import { createChatSettingsViewModel } from "./ChatSettingsViewModel";
import { createChatCommandCatalogViewModel } from "./ChatCommandCatalogViewModel";
import { createChatComposerViewModel } from "./ChatComposerViewModel";
import type { ComposerSubmission } from "./ComposerInputViewModel";
import { createNewChatGoalBinding } from "./goal/goalBinding";

export type ChatDestinationOpening =
  | Extract<ChatSettingsTarget, { kind: "session" }>
  | Readonly<{
      kind: "new_chat";
      projectID: string;
      workspace: WorkspaceCatalogRow | null;
      initialAgentRole?: string | undefined;
    }>;
type ChatSelection =
  | Extract<ChatDestinationOpening, { kind: "session" }>
  | (Extract<ChatDestinationOpening, { kind: "new_chat" }> &
      Readonly<{ name: string | null; worktreeSelector: string | null }>);

export function createChatDestinationViewModel({
  opening,
  services,
  client,
  t,
  push,
}: Readonly<{
  opening: ChatDestinationOpening;
  services: AppServices;
  client: QueryClient;
  t: TFunction;
  push: StatusController["push"];
}>) {
  const selection = Atom.make<ChatSelection>(
    opening.kind === "new_chat" ? { ...opening, name: null, worktreeSelector: null } : opening,
  );
  const worktreeCreationKey = ["chat-worktree-create", crypto.randomUUID()] as const;
  const settingsSelection = Atom.make((get) => {
    const selected = get(selection);
    return selected.kind === "session" ? selected : selected.workspace;
  });
  const target = Atom.make<ChatSettingsTarget | null>((get) => {
    const selected = get(settingsSelection);
    return selected !== null && "kind" in selected
      ? selected
      : selected === null
        ? null
        : {
            kind: "new_chat",
            projectID: opening.projectID,
            workspace: { workspaceID: selected.id },
          };
  });
  const settings = createChatSettingsViewModel({
    services,
    client,
    t,
    target,
    initialAgentRole: opening.kind === "new_chat" ? opening.initialAgentRole : undefined,
  });
  const catalog = createChatCommandCatalogViewModel({ services, client, target });
  const creationTarget = Atom.make((get): ChatMutationTarget | null => {
    const selected = get(selection);
    if (selected.kind === "session") return selected;
    const value = get(settings.state);
    const readTarget = get(target);
    return value.kind === "ready-new-chat" && readTarget?.kind === "new_chat"
      ? {
          ...readTarget,
          initialSettings: value.initialSettings,
          name: selected.name,
          worktreeSelector: selected.worktreeSelector,
        }
      : null;
  });
  const submission = Atom.make((get): ComposerSubmission => {
    const value = get(settings.state);
    const ready = get(creationTarget);
    if (ready?.kind === "new_chat") return { kind: "ready", target: ready };
    if (value.kind === "ready-session") return { kind: "ready" };
    return "error" in value ? { kind: "failed", error: value.error } : { kind: "loading" };
  });
  type PopOutInput = Readonly<{
    target: Extract<ChatSettingsTarget, { kind: "session" }>;
    flushDraft(): Promise<boolean>;
    onCreated(projectID: string): Promise<void>;
  }>;
  const popOutObserver = new MutationObserver(client, {
    retry: false,
    networkMode: "always",
    mutationFn: async (input: PopOutInput) => {
      if (!(await input.flushDraft())) return;
      const result = await openNativeChat(services.nativeBridge, input.target);
      if (result === "created") await input.onCreated(input.target.projectID);
    },
    onError: (error) => {
      push({
        id: "chat-pop-out-error",
        tone: "danger",
        title: t("app.popOutError"),
        body: errorMessage(error),
      });
    },
  });
  const popOutRequest = queryAtom(popOutObserver);
  const transferPending = Atom.make((get) => get(popOutRequest).isPending);
  const composer = createChatComposerViewModel({
    services,
    client,
    target,
    submission,
    opening,
    t,
    transferPending,
  });
  const popOut = Atom.fn<
    Omit<PopOutInput, "target"> &
      Readonly<{
        editRequest: Atom.Atom<Readonly<{ isPending: boolean }>>;
      }>
  >()(
    (input, get) =>
      Effect.gen(function* () {
        const selected = get(target);
        if (
          selected?.kind !== "session" ||
          get(input.editRequest).isPending ||
          !services.nativeBridge.capabilities.dialogWindows ||
          popOutObserver.getCurrentResult().isPending
        )
          return;
        yield* Effect.tryPromise(async () =>
          popOutObserver.mutate({
            flushDraft: input.flushDraft,
            onCreated: input.onCreated,
            target: selected,
          }),
        ).pipe(Effect.ignore);
      }),
    { concurrent: true },
  );
  const goal = createNewChatGoalBinding({
    api: services.api.chat,
    client,
    target,
    creationTarget,
    settings,
    draft: composer.draft,
  });
  const worktreeCreationPending = mutationPendingAtom(client, { mutationKey: worktreeCreationKey });
  const firstActionPending = Atom.make(
    (get) => get(composer.input.pending) || get(goal.pending) || get(worktreeCreationPending),
  );
  const adopt = Atom.fn<
    Readonly<{
      sessionID: string;
      origin: Pick<ChatSettingsTarget, "kind">;
      rejection: ChatNotAcceptedReason | null;
      delivered?(sessionID: string): void;
      openCreatedSession?(sessionID: string): Promise<void>;
    }>
  >()(
    (input, get) =>
      Effect.gen(function* () {
        if (input.origin.kind === "new_chat") get.set(composer.draft.consumeNewChat, undefined);
        const current = get(selection);
        if (current.kind === "session" && current.sessionID === input.sessionID) {
          if (input.rejection?.kind === "prompt_command_not_found") get.set(catalog.retry, undefined);
          return;
        }
        const session = {
          kind: "session" as const,
          projectID: opening.projectID,
          sessionID: input.sessionID,
        };
        const openCreatedSession = input.openCreatedSession;
        if (current.kind === "session" && openCreatedSession !== undefined) {
          const saved = yield* get.setResult(composer.draft.adopt, session);
          if (!saved) return;
          yield* Effect.promise(async () => openCreatedSession(input.sessionID));
          return;
        }
        get.set(selection, session);
        get.set(composer.draft.adopt, session);
        input.delivered?.(input.sessionID);
      }),
    { concurrent: true },
  );
  const selectWorkspace = Atom.fn<WorkspaceCatalogRow>()(
    (workspace, get) =>
      Effect.sync(() => {
        const current = get(selection);
        if (current.kind !== "new_chat" || get(firstActionPending) || current.workspace?.id === workspace.id)
          return;
        get.set(selection, { ...current, workspace, worktreeSelector: null });
      }),
    { concurrent: true },
  );
  return {
    selection: Atom.make((get) => get(selection)),
    target,
    creationTarget,
    worktreeCreationKey,
    worktreeScope: Atom.make((get) => {
      const selected = get(target);
      if (selected === null || selected.kind === "session") return selected;
      if (!("workspaceID" in selected.workspace)) throw new Error("Chat Workspace ID is required");
      return {
        kind: "workspace" as const,
        projectID: selected.projectID,
        workspaceID: selected.workspace.workspaceID,
      };
    }),
    worktreeSelector: Atom.make((get) => {
      const selected = get(selection);
      return selected.kind === "new_chat" ? selected.worktreeSelector : null;
    }),
    workspaceName: Atom.make((get) => {
      const selected = get(selection);
      return selected.kind === "new_chat" ? (selected.workspace?.name ?? null) : null;
    }),
    selectWorktree: Atom.fn<string | null>()((worktreeSelector, get) =>
      Effect.sync(() => {
        const current = get(selection);
        if (current.kind === "new_chat") get.set(selection, { ...current, worktreeSelector });
      }),
    ),
    setName: Atom.fn<string | null>()((name, get) =>
      Effect.sync(() => {
        const current = get(selection);
        if (current.kind === "new_chat") get.set(selection, { ...current, name });
      }),
    ),
    settings,
    composer,
    catalog,
    goal,
    firstActionPending,
    adopt,
    selectWorkspace,
    popOut,
    popOutRequest,
  } as const;
}
