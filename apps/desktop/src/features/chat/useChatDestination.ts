import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQueryClient } from "@tanstack/react-query";
import { useAppServices } from "@/app-facade";
import { useAtomMount, useAtomSet, useAtomValue } from "@effect/atom-react";
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
  openCreatedSession,
}: Readonly<{
  opening: ChatDestinationOpening;
  navigation: ChatSettingsNavigation;
  commands?: readonly ComposerCommand[];
  onSessionDelivered?(sessionID: string): void;
  openCreatedSession?(sessionID: string): Promise<void>;
}>) {
  const services = useAppServices();
  const { t } = useTranslation();
  const client = useQueryClient();
  const { push } = useStatusController();
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const [model] = useState(() => createChatDestinationViewModel({ opening, services, client, t, push }));
  const target = useAtomValue(model.target);
  useAtomMount(model.popOutRequest);
  const popOutRequest = useAtomValue(model.popOutRequest);
  const popOut = useAtomSet(model.popOut);
  const editorRef = useRef<HTMLTextAreaElement>(null);
  const focusComposer = useCallback(() => editorRef.current?.focus(), []);
  const worktreeCommands = useWorktreeCommands(
    target?.kind === "session" ? target.sessionID : null,
    focusComposer,
  );
  const roots = useOwnedSidebarRoots();
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
    execution:
      target?.kind === "session"
        ? {
            kind: "direct",
            send: async () => {
              openProcesses();
              return { kind: "local" };
            },
          }
        : { kind: "unavailable", notify: openProcesses },
  };
  const selection = useAtomValue(model.selection);
  // Delivery must await draft persistence and native opening within the input Query completion.
  // Promise-mode is explicitly approved for this completion boundary (KENT-623).
  const adoptAction = useAtomSet(model.adopt, { mode: "promise" });
  const selectWorkspace = useAtomSet(model.selectWorkspace);
  const workspace = useChatWorkspace(selection, selectWorkspace);
  const firstActionPending = useAtomValue(model.firstActionPending);
  const settings = useChatSettings({ model: model.settings, ...navigation });
  const catalog = useAtomValue(model.catalog.read);
  const retryCatalog = useAtomSet(model.catalog.retry);
  const adopt = async (
    sessionID: string,
    origin: Pick<ChatSettingsTarget, "kind">,
    rejection: ChatNotAcceptedReason | null = null,
  ) => {
    if (mounted.current)
      return adoptAction({
        sessionID,
        origin,
        rejection,
        ...(onSessionDelivered === undefined ? {} : { delivered: onSessionDelivered }),
        ...(openCreatedSession === undefined ? {} : { openCreatedSession }),
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
    onDeliveredSession: async (result, origin) => {
      return adopt(
        result.sessionID,
        origin,
        result.outcome.kind === "not_accepted" ? result.outcome.reason : null,
      );
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
                void adopt(delivery.target.sessionID, delivery.origin);
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
    popOutRequest,
    popOut,
  } as const;
}
