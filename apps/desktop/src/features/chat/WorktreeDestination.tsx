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
import { WorktreeDeleteButton } from "./WorktreeDeleteButton";
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

function WorktreeDestinationContent({ destination, navigator }: Parameters<typeof WorktreeDestination>[0]) {
  const { api } = useAppServices();
  const client = useQueryClient();
  const { currentSurface } = useSidebarShell();
  const { push } = useStatusController();
  const { t } = useTranslation();
  const model = useMemo(
    () =>
      createWorktreeActions({
        client,
        api,
        sessionID: destination.sessionID,
        navigator,
        push,
        t,
        refreshOpenWorktreeList: createRefreshOpenWorktreeList(client, api, currentSurface),
      }),
    [api, client, currentSurface, destination.sessionID, navigator, push, t],
  );
  const actions = useWorktreeActions(model);
  const switching = useAtomValue(model.switching);
  if (destination.page === "create")
    return <WorktreeCreateForm sessionID={destination.sessionID} navigator={navigator} actions={model} />;
  return (
    <WorktreeBrowser
      sessionID={destination.sessionID}
      navigator={navigator}
      switchPending={switching.isPending}
      renderDelete={(operation) => (
        <WorktreeDeleteButton
          sessionID={destination.sessionID}
          selector={operation.selector}
          refreshOpenWorktreeList={model.refreshOpenWorktreeList}
        />
      )}
      onSwitch={actions.switchWorktree}
      onCreate={() => {
        navigator.replace({ ...destination, page: "create" });
      }}
    />
  );
}
