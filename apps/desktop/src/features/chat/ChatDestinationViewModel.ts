import * as Atom from "effect/reactivity/Atom";
import * as Effect from "effect/Effect";
import { MutationObserver, type QueryClient } from "@tanstack/react-query";
import type { TFunction } from "i18next";
import { openNativeChat, queryAtom, type AppServices, type StatusController } from "@/app-facade";
import {
  errorMessage,
  type ChatNotAcceptedReason,
  type ChatSettingsTarget,
  type WorkspaceCatalogRow,
} from "@/api";
import { createChatSettingsViewModel } from "./ChatSettingsViewModel";
import { createChatCommandCatalogViewModel } from "./ChatCommandCatalogViewModel";
import { createChatComposerViewModel } from "./ChatComposerViewModel";
import type { ComposerSubmission } from "./ComposerInputViewModel";
import { createNewChatGoalBinding } from "./goal/goalBinding";

export type ChatDestinationOpening =
  | Extract<ChatSettingsTarget, { kind: "session" }>
  | Readonly<{ kind: "new_chat"; projectID: string; workspace: WorkspaceCatalogRow | null }>;

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
  const selection = Atom.make(opening);
  const target = Atom.make<ChatSettingsTarget | null>((get) => {
    const selected = get(selection);
    return selected.kind === "session"
      ? selected
      : selected.workspace === null
        ? null
        : {
            kind: "new_chat",
            projectID: selected.projectID,
            workspace: { workspaceID: selected.workspace.id },
          };
  });
  const settings = createChatSettingsViewModel({ services, client, t, target });
  const catalog = createChatCommandCatalogViewModel({ services, client, target });
  const submission = Atom.make((get): ComposerSubmission => {
    const value = get(settings.state);
    if (value.kind === "ready-new-chat") return { kind: "ready", initialSettings: value.initialSettings };
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
    settings,
    draft: composer.draft,
  });
  const firstActionPending = Atom.make((get) => get(composer.input.pending) || get(goal.pending));
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
        get.set(selection, { ...current, workspace });
      }),
    { concurrent: true },
  );
  return {
    selection: Atom.make((get) => get(selection)),
    target,
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
