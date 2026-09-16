import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";
import type { QueryClient } from "@tanstack/react-query";
import type { TFunction } from "i18next";
import type { AppServices } from "@/app-facade";
import type { ChatSettingsTarget, WorkspaceCatalogRow } from "@/api";
import { createChatSettingsViewModel } from "./ChatSettingsViewModel";
import { createChatComposerViewModel } from "./ChatComposerViewModel";
import type { ComposerSubmission } from "./ComposerInputViewModel";
import { createNewChatGoalBinding } from "./goal/goalBinding";

export type ChatDestinationOpening =
  | Extract<ChatSettingsTarget, { kind: "session" }>
  | Readonly<{ kind: "new_chat"; projectID: string; workspace: WorkspaceCatalogRow }>;

export function createChatDestinationViewModel({
  opening,
  services,
  client,
  t,
}: Readonly<{
  opening: ChatDestinationOpening;
  services: AppServices;
  client: QueryClient;
  t: TFunction;
}>) {
  const selection = Atom.make(opening);
  const target = Atom.make<ChatSettingsTarget>((get) => {
    const selected = get(selection);
    return selected.kind === "session"
      ? selected
      : {
          kind: "new_chat",
          projectID: selected.projectID,
          workspace: { workspaceID: selected.workspace.id },
        };
  });
  const settings = createChatSettingsViewModel({ services, client, t, target });
  const submission = Atom.make((get): ComposerSubmission => {
    const value = get(settings.state);
    if (value.kind === "ready-new-chat") return { kind: "ready", initialSettings: value.initialSettings };
    if (value.kind === "ready-session") return { kind: "ready" };
    return "error" in value ? { kind: "failed", error: value.error } : { kind: "loading" };
  });
  const initial: ChatSettingsTarget =
    opening.kind === "session"
      ? opening
      : {
          kind: "new_chat",
          projectID: opening.projectID,
          workspace: { workspaceID: opening.workspace.id },
        };
  const composer = createChatComposerViewModel({ services, client, target, submission, opening: initial, t });
  const goal = createNewChatGoalBinding({
    api: services.api.chat,
    client,
    target,
    settings,
    draft: composer.draft,
  });
  const firstActionPending = Atom.make((get) => get(composer.input.pending) || get(goal.pending));
  const adopt = Atom.fn<Readonly<{ sessionID: string; delivered?(sessionID: string): void }>>()(
    (input, get) =>
      Effect.sync(() => {
        const session = {
          kind: "session" as const,
          projectID: opening.projectID,
          sessionID: input.sessionID,
        };
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
        if (current.kind !== "new_chat" || get(firstActionPending) || current.workspace.id === workspace.id)
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
    goal,
    firstActionPending,
    adopt,
    selectWorkspace,
  } as const;
}
