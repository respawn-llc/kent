import { useAtomValue } from "@effect/atom-react";
import * as Atom from "effect/reactivity/Atom";
import type { WorktreeScope } from "@/api";
import { useQueryClient } from "@tanstack/react-query";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";

import {
  createRefreshOpenWorktreeList,
  useAppServices,
  useSidebarShell,
  useStatusController,
  type SidebarDestination,
  type SidebarPageNavigator,
} from "@/app-facade";
import { createWorktreeActions, useWorktreeActions } from "./WorktreeActions";
import { WorktreeBrowser } from "./WorktreeBrowser";
import { WorktreeCreateForm } from "./WorktreeCreateForm";

export function WorktreeDestination({
  destination,
  navigator,
}: Readonly<{
  destination: Extract<SidebarDestination, { kind: "worktree" }>;
  navigator: SidebarPageNavigator;
}>) {
  const { phase } = useSidebarShell();
  return phase === "closing" ? null : (
    <WorktreeDestinationContent destination={destination} navigator={navigator} />
  );
}

function useWorktreeDestinationTarget(destination: Parameters<typeof WorktreeDestination>[0]["destination"]) {
  const fallbackScope = useMemo(
    () =>
      Atom.make<WorktreeScope | null>(
        destination.sessionID === null ? null : { kind: "session", sessionID: destination.sessionID },
      ),
    [destination.sessionID],
  );
  const empty = useMemo(() => Atom.make<string | null>(null), []);
  const scope = useAtomValue(destination.chat?.scope ?? fallbackScope);
  const selector = useAtomValue(destination.chat?.selector ?? empty);
  const workspaceName = useAtomValue(destination.chat?.workspaceName ?? empty);
  const sessionID = scope?.kind === "session" ? scope.sessionID : null;
  return { scope, selector, workspaceName, sessionID };
}

function WorktreeDestinationContent({ destination, navigator }: Parameters<typeof WorktreeDestination>[0]) {
  const { api } = useAppServices();
  const client = useQueryClient();
  const { currentSurface } = useSidebarShell();
  const { push } = useStatusController();
  const { t } = useTranslation();
  const { scope, selector, workspaceName, sessionID } = useWorktreeDestinationTarget(destination);
  const model = useMemo(
    () =>
      createWorktreeActions({
        client,
        api,
        sessionID,
        onAccepted: () => {
          navigator.close();
        },
        push,
        t,
        refreshOpenWorktreeList: createRefreshOpenWorktreeList(client, api, currentSurface),
      }),
    [api, client, currentSurface, sessionID, navigator, push, t],
  );
  const actions = useWorktreeActions(model);
  const switching = useAtomValue(model.requestPending);
  if (scope === null) return null;
  if (destination.page === "create")
    return (
      <WorktreeCreateForm
        projectID={destination.projectID}
        sessionID={sessionID}
        scope={scope}
        {...(destination.chat === undefined ? {} : { chat: destination.chat })}
        openSession={destination.openSession}
        navigator={navigator}
        actions={model}
      />
    );
  return (
    <WorktreeBrowser
      projectID={destination.projectID}
      sessionID={sessionID}
      scope={scope}
      {...(scope.kind !== "workspace" || destination.chat === undefined || workspaceName === null
        ? {}
        : {
            workspace: { name: workspaceName, selector, select: destination.chat.select },
          })}
      onOpenSession={destination.openSession}
      navigator={navigator}
      switchPending={switching}
      refreshOpenWorktreeList={model.refreshOpenWorktreeList}
      onSwitch={actions.switchWorktree}
      onCreate={() => {
        navigator.replace({ ...destination, page: "create" });
      }}
    />
  );
}
