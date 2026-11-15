import { useAtomValue } from "@effect/atom-react";
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
  const scope = useAtomValue(destination.chat.scope);
  const sessionID = scope?.kind === "session" ? scope.sessionID : null;
  return { scope, sessionID };
}

function WorktreeDestinationContent({ destination, navigator }: Parameters<typeof WorktreeDestination>[0]) {
  const { api } = useAppServices();
  const client = useQueryClient();
  const { currentSurface } = useSidebarShell();
  const { push } = useStatusController();
  const { t } = useTranslation();
  const { scope, sessionID } = useWorktreeDestinationTarget(destination);
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
        chat={destination.chat}
        openSession={destination.openSession}
        navigator={navigator}
        actions={model}
      />
    );
  return (
    <WorktreeBrowser
      projectID={destination.projectID}
      chat={destination.chat}
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
