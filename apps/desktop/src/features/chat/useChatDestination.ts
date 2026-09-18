import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { useAppServices, workspaceCatalogInfiniteQueryOptions } from "@/app-facade";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { createChatDestinationViewModel, type ChatDestinationOpening } from "./ChatDestinationViewModel";
import { useChatSettings, type ChatSettingsNavigation } from "./useChatSettings";
import { useChatComposer } from "./useChatComposer";
import { chatDestinationCommands } from "./chatDestinationCommands";
import type { ComposerCommand } from "./composerCommands";
import { useGoalSidebarLauncher } from "./goal/useGoalSidebarLauncher";
import { useNewChatGoalActions } from "./goal/goalBinding";

export function useChatDestination({
  opening,
  navigation,
  commands = [],
  onSessionDelivered,
}: Readonly<{
  opening: ChatDestinationOpening;
  navigation: ChatSettingsNavigation;
  commands?: readonly ComposerCommand[];
  onSessionDelivered?(sessionID: string): void;
}>) {
  const services = useAppServices();
  const { t } = useTranslation();
  const client = useQueryClient();
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const [model] = useState(() => createChatDestinationViewModel({ opening, services, client, t }));
  const target = useAtomValue(model.target);
  const selection = useAtomValue(model.selection);
  const [workspaceOpen, setWorkspaceOpen] = useState(false);
  const workspaceCatalog = useInfiniteQuery({
    ...workspaceCatalogInfiniteQueryOptions(services.api, target.projectID),
    enabled: selection.kind === "new_chat" && workspaceOpen,
    retry: false,
  });
  const adoptAction = useAtomSet(model.adopt);
  const selectWorkspace = useAtomSet(model.selectWorkspace);
  const firstActionPending = useAtomValue(model.firstActionPending);
  const settings = useChatSettings({ model: model.settings, ...navigation });
  const adopt = (sessionID: string) => {
    if (mounted.current)
      adoptAction({
        sessionID,
        ...(onSessionDelivered === undefined ? {} : { delivered: onSessionDelivered }),
      });
  };
  const composer = useChatComposer({
    model: model.composer,
    commands: [...chatDestinationCommands(services.api.chat, target.kind === "new_chat", t), ...commands],
    onDeliveredSession: (result) => {
      adopt(result.sessionID);
    },
  });
  const goalActions = useNewChatGoalActions(model.goal);
  const openGoal = useGoalSidebarLauncher({
    kind: "new_chat",
    api: services.api.chat,
    binding: model.goal,
    setGoal: async (objective) =>
      goalActions.setGoal({
        objective,
        delivered: (delivery) => {
          adopt(delivery.target.sessionID);
        },
      }),
  });
  return {
    target,
    settings,
    composer,
    adopt,
    selectWorkspace,
    openGoal,
    firstActionPending,
    workspace: selection.kind === "new_chat" ? selection.workspace : null,
    workspaceOpen,
    setWorkspaceOpen,
    workspaceCatalog,
  } as const;
}
