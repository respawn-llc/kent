import { useEffect, useMemo } from "react";
import { useAtomMount, useAtomSet, useAtomSuspense, useAtomValue } from "@effect/atom-react";
import { useQueryClient } from "@tanstack/react-query";
import { useLocation, useMatch } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import {
  SidebarRootOwner,
  useOwnedSidebarRoots,
  useAppServices,
  useStatusController,
  useWindowFocus,
  useAppNavigation,
  useChatPromptPresence,
  useSidebarShell,
  sessionChatRoutePath,
  newChatRoutePath,
  sessionChatHistoryStateSchema,
} from "@/app-facade";
import { useStableCallback } from "@/ui";
import { createAttentionViewModel } from "./AttentionViewModel";

export function AttentionController() {
  return (
    <SidebarRootOwner>
      <OwnedAttentionController />
    </SidebarRootOwner>
  );
}

function OwnedAttentionController() {
  const services = useAppServices();
  const client = useQueryClient();
  const { t } = useTranslation();
  const status = useStatusController();
  const roots = useOwnedSidebarRoots();
  const focused = useWindowFocus();
  const picker = useFocusedChatPicker();
  const navigation = useAppNavigation();
  const inputs = useStableCallback(() => ({ focused, picker }));
  const openSessionChat = useStableCallback(navigation.openSessionChat);
  const description = useMemo(
    () =>
      createAttentionViewModel({
        services,
        client,
        t,
        status,
        roots,
        inputs,
        openSessionChat,
      }),
    [services, client, t, status, roots, inputs, openSessionChat],
  );
  const model = useAtomValue(description);
  useAtomMount(model.surfaces);
  useAtomSuspense(model.deliver);
  useAtomSuspense(model.activate);
  useAtomSuspense(model.remove);
  useAtomSuspense(model.refresh);
  useAtomSuspense(model.reportFailure);
  useAtomSuspense(model.inbox);
  useAtomSuspense(model.activation);
  useAtomSuspense(model.permission);
  const focusChanged = useAtomSet(model.focusChanged);
  useEffect(() => {
    focusChanged(undefined);
  }, [focused, picker, focusChanged]);
  return null;
}

function useFocusedChatPicker() {
  const presence = useChatPromptPresence();
  const { activeDestination } = useSidebarShell();
  const chatMatch = useMatch({ from: sessionChatRoutePath, shouldThrow: false });
  const newChatMatch = useMatch({ from: newChatRoutePath, shouldThrow: false });
  const historyState = useLocation({ select: (location) => location.state });
  const bookmark = sessionChatHistoryStateSchema.parse(historyState).sessionChat;
  const projectID = (chatMatch ?? newChatMatch)?.params.projectId;
  const routeSessionID = chatMatch?.params.sessionId;
  const sessionID =
    bookmark?.projectID === projectID ? (bookmark?.deliveredSessionID ?? routeSessionID) : routeSessionID;
  return activeDestination === null &&
    presence.target !== null &&
    projectID === presence.target.projectID &&
    sessionID === presence.target.sessionID
    ? presence.target
    : null;
}
