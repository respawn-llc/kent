import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useAppServices } from "@/app-facade";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { ContractError, type WorkspaceCatalogRow } from "@/api";
import { createChatDestinationViewModel, type ChatDestinationOpening } from "./ChatDestinationViewModel";
import { useChatSettings, type ChatSettingsNavigation } from "./useChatSettings";
import { useChatComposer } from "./useChatComposer";
import { chatDestinationCommands } from "./chatDestinationCommands";
import type { ComposerCommand } from "./composerCommands";
import { useChatGoal } from "./goal/useChatGoal";

const ignoreProjection = () => {
  /* Settings owns these projections until Context is assembled. */
};
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
  const { api } = useAppServices();
  const { t } = useTranslation();
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const [model] = useState(() => createChatDestinationViewModel(opening));
  const target = useAtomValue(model.target);
  const selection = useAtomValue(model.selection);
  const select = useAtomSet(model.selection);
  const settings = useChatSettings(
    target.kind === "new_chat"
      ? { target, onInitialSettingsChange: ignoreProjection }
      : { target, ...navigation, authoritativeRefreshGeneration: null, onContextChange: ignoreProjection },
  );
  const submission =
    settings.kind === "ready-new-chat"
      ? { kind: "ready" as const, initialSettings: settings.initialSettings }
      : settings.kind === "ready-session"
        ? { kind: "ready" as const }
        : settings.kind === "failed-new-chat" || settings.kind === "failed-session"
          ? { kind: "failed" as const, error: settings.error }
          : { kind: "loading" as const };
  const composer = useChatComposer({
    target: model.target,
    submission,
    commands: [...chatDestinationCommands(api.chat, target.kind === "new_chat", t), ...commands],
    onDeliveredSession: (result) => {
      adopt(result.sessionID);
    },
  });
  const selectedAgent =
    settings.kind === "ready-new-chat"
      ? settings.catalog.choices.find((choice) => choice.agent.role === settings.initialSettings.agentRole)
      : undefined;
  const goal = useChatGoal({
    target,
    ready: settings.kind === "ready-new-chat",
    availability:
      selectedAgent === undefined
        ? null
        : selectedAgent.questions.capable
          ? "available"
          : "agent_capability_missing",
    captureTarget: () => {
      if (
        target.kind !== "new_chat" ||
        settings.kind !== "ready-new-chat" ||
        !("workspaceID" in target.workspace)
      )
        throw new ContractError("New Chat Settings are not ready.");
      composer.beginFirstAction(undefined);
      return {
        kind: "new_chat",
        projectID: target.projectID,
        workspaceID: target.workspace.workspaceID,
        initialSettings: settings.initialSettings,
        initialInputDraft: composer.text,
      };
    },
    delivered: (delivery) => {
      adopt(delivery.target.sessionID);
    },
    failed: () => {
      if (mounted.current) composer.resumeNewChat(undefined);
    },
  });
  function adopt(sessionID: string) {
    if (!mounted.current) return;
    const session = { kind: "session" as const, projectID: opening.projectID, sessionID };
    select(session);
    composer.adoptDraft(session);
    onSessionDelivered?.(sessionID);
  }
  function selectWorkspace(workspace: WorkspaceCatalogRow) {
    if (
      selection.kind !== "new_chat" ||
      composer.inputPending ||
      goal.pending ||
      selection.workspace.id === workspace.id
    )
      return;
    select({ ...selection, workspace });
  }
  return {
    target,
    settings,
    composer,
    adopt,
    selectWorkspace,
    openGoal: goal.open,
    firstActionPending: composer.inputPending || goal.pending,
    workspace: selection.kind === "new_chat" ? selection.workspace : null,
  } as const;
}
