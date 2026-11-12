import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQueryClient } from "@tanstack/react-query";
import { useAppServices, type ChatWorktreeBinding } from "@/app-facade";
import { useAtomMount, useAtomSet, useAtomValue } from "@effect/atom-react";
import { createChatDestinationViewModel, type ChatDestinationOpening } from "./ChatDestinationViewModel";
import { useChatSettings, type ChatSettingsNavigation } from "./useChatSettings";
import { useChatComposer } from "./useChatComposer";
import { promptCommands } from "./promptCommands";
import { createNameCommand } from "./nameCommand";
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
  const setTemporaryName = useAtomSet(model.setName);
  useAtomMount(model.popOutRequest);
  const popOutRequest = useAtomValue(model.popOutRequest);
  const popOut = useAtomSet(model.popOut);
  const editorRef = useRef<HTMLTextAreaElement>(null);
  const focusComposer = useCallback(() => editorRef.current?.focus(), []);
  const roots = useOwnedSidebarRoots();
  const openProcesses = useStableCallback(() => {
    if (target === null) return;
    const scope = target.kind === "session" ? target : { ...target, kind: "project" as const };
    void roots.open({ kind: "processes", scope }).lifecycle.then((outcome) => {
      if (outcome === "closed") focusComposer();
    });
  });
  const processesCommand: ComposerCommand = {
    token: "/ps",
    aliases: [],
    description: t("processes.title"),
    preview: null,
    execution:
      target !== null
        ? {
            kind: "direct",
            send: async () => {
              openProcesses();
              return { kind: "local" };
            },
          }
        : { kind: "unavailable", draft: "discard", notify: openProcesses },
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
  const deliverWorktreeSession = useStableCallback(async (sessionID: string) =>
    adopt(sessionID, { kind: "new_chat" }),
  );
  const selectWorktree = useAtomSet(model.selectWorktree);
  const worktreeBinding = useMemo(
    (): ChatWorktreeBinding => ({
      scope: model.worktreeScope,
      selector: model.worktreeSelector,
      workspaceName: model.workspaceName,
      creationTarget: model.creationTarget,
      draft: model.composer.draft.text,
      creationKey: model.worktreeCreationKey,
      select: selectWorktree,
      delivered: deliverWorktreeSession,
    }),
    [model, selectWorktree, deliverWorktreeSession],
  );
  const worktreeCommands = useWorktreeCommands({
    projectID: opening.projectID,
    sessionID: target?.kind === "session" ? target.sessionID : null,
    focusComposer,
    openSession: navigation.openParentSession,
    chat: worktreeBinding,
  });
  const composer = useChatComposer({
    model: model.composer,
    prepared: (id) => {
      if (editorRef.current !== null) composer.messageMotion.capture(id.toJSONValue(), editorRef.current);
    },
    rejected: (id) => {
      composer.messageMotion.reject(id.toJSONValue());
    },
    commands: [
      ...promptCommands(catalog.data ?? [], t),
      ...worktreeCommands.commands,
      processesCommand,
      createNameCommand({
        api: services.api.chat,
        description: t("chatComposer.commands.name"),
        setTemporaryName,
      }),
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
    projectID: opening.projectID,
    target,
    temporaryName: selection.kind === "new_chat" ? selection.name : null,
    worktreeBinding,
    settings,
    settingsModel: model.settings,
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
