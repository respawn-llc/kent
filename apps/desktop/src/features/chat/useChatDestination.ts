import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQueryClient } from "@tanstack/react-query";
import { useAppServices } from "@/app-facade";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { createChatDestinationViewModel, type ChatDestinationOpening } from "./ChatDestinationViewModel";
import { useChatSettings, type ChatSettingsNavigation } from "./useChatSettings";
import { useChatComposer } from "./useChatComposer";
import { promptCommands } from "./promptCommands";
import { useWorktreeCommands } from "./WorktreeCommands";
import { useOwnedSidebarRoots, useStatusController } from "@/app-facade";
import { useStableCallback } from "@/ui";
import type { ComposerCommand } from "./composerCommands";
import { useGoalSidebarLauncher } from "./goal/useGoalSidebarLauncher";
import { useNewChatGoalActions } from "./goal/goalBinding";
import type { ChatNotAcceptedReason, ChatSettingsTarget } from "@/api";
import { useChatWorkspace } from "./useChatWorkspace";

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
  const editorRef = useRef<HTMLTextAreaElement>(null);
  const focusComposer = useCallback(() => editorRef.current?.focus(), []);
  const worktreeCommands = useWorktreeCommands(
    target?.kind === "session" ? target.sessionID : null,
    focusComposer,
  );
  const roots = useOwnedSidebarRoots();
  const { push } = useStatusController();
  const openProcesses = useStableCallback(() => {
    if (target?.kind !== "session") {
      push({
        id: "processes-session-required",
        tone: "danger",
        title: t("processes.title"),
        body: t("processes.sessionRequired"),
      });
      return;
    }
    void roots
      .open({ kind: "processes", projectID: target.projectID, sessionID: target.sessionID })
      .lifecycle.then((outcome) => {
        if (outcome === "closed") focusComposer();
      });
  });
  const processesCommand: ComposerCommand = {
    token: "/ps",
    aliases: [],
    description: t("processes.title"),
    preview: null,
    execution: {
      kind: "direct",
      send: async () => {
        openProcesses();
        return { kind: "local" };
      },
    },
  };
  const selection = useAtomValue(model.selection);
  const adoptAction = useAtomSet(model.adopt);
  const selectWorkspace = useAtomSet(model.selectWorkspace);
  const workspace = useChatWorkspace(selection, selectWorkspace);
  const firstActionPending = useAtomValue(model.firstActionPending);
  const settings = useChatSettings({ model: model.settings, ...navigation });
  const catalog = useAtomValue(model.catalog.read);
  const retryCatalog = useAtomSet(model.catalog.retry);
  const adopt = (
    sessionID: string,
    origin: Pick<ChatSettingsTarget, "kind">,
    rejection: ChatNotAcceptedReason | null = null,
  ) => {
    if (mounted.current)
      adoptAction({
        sessionID,
        origin,
        rejection,
        ...(onSessionDelivered === undefined ? {} : { delivered: onSessionDelivered }),
      });
  };
  const composer = useChatComposer({
    model: model.composer,
    commands: [
      ...promptCommands(catalog.data ?? [], t),
      ...worktreeCommands.commands,
      processesCommand,
      ...commands,
    ],
    catalog,
    retryCatalog: () => {
      retryCatalog(undefined);
    },
    onDeliveredSession: (result, origin) => {
      adopt(result.sessionID, origin, result.outcome.kind === "not_accepted" ? result.outcome.reason : null);
    },
  });
  const goalActions = useNewChatGoalActions(model.goal);
  const goalState = useAtomValue(model.goal.state);
  const openGoal = useGoalSidebarLauncher(
    target?.kind === "session"
      ? {
          kind: "session",
          api: services.api.chat,
          target,
        }
      : {
          kind: "new_chat",
          api: services.api.chat,
          binding: model.goal,
          setGoal: async (objective) =>
            goalActions.setGoal({
              objective,
              delivered: (delivery) => {
                adopt(delivery.target.sessionID, delivery.origin);
              },
            }),
        },
  );
  return {
    target,
    settings,
    composer,
    adopt,
    selectWorkspace,
    openGoal,
    goalState,
    firstActionPending,
    ...workspace,
    editorRef,
    commandPresentation: worktreeCommands.presentation,
    worktreeObservation: worktreeCommands.observation,
  } as const;
}
