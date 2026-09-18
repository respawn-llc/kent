import { useMemo, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { useBlocker } from "@tanstack/react-router";
import { errorMessage } from "@/api";
import type { ChatDestinationOpening } from "./ChatDestinationViewModel";
import {
  ChatRuntimeProvider,
  useAppServices,
  useChatRuntimePresentation,
  useStatusController,
} from "@/app-facade";
import { useQueryClient } from "@tanstack/react-query";
import { useAtomValue } from "@effect/atom-react";
import type { TFunction } from "i18next";
import { ChatShell, type ChatShellState } from "./ChatShell";
import { ChatComposer } from "./ChatComposer";
import { ChatComposerSurface } from "./ChatComposerSurface";
import { useChatDestination } from "./useChatDestination";
import type { ChatSettingsNavigation } from "./useChatSettings";
import { ChatWorkspaceChip } from "./ChatWorkspaceChip";
import { Spinner, useDelayedAppearance } from "@/ui";
import { GoalAffordance } from "./goal";
import {
  createChatMessageEditViewModel,
  useChatMessageEditActions,
} from "./messageRows/ChatMessageEditViewModel";
import { ChatTranscriptContent } from "./ChatTranscriptContent";
import { WorktreeControl } from "./WorktreeControl";
import { useWorktreeList } from "./useWorktreeList";
import { ChatProcessesChip } from "./ChatProcessesChip";
import { chatOperationFailureMessage } from "./chatSettingsPresentation";

export function ChatDestination(
  props: Readonly<{
    opening: ChatDestinationOpening;
    navigation: ChatSettingsNavigation;
    onSessionDelivered?(sessionID: string): void;
  }>,
) {
  const destination = useChatDestination(props);
  const { target, settings, composer } = destination;
  useBlocker({ enableBeforeUnload: false, shouldBlockFn: async () => !(await composer.flushDraft()) });
  const { api, logger } = useAppServices();
  const openingVisible = useDelayedAppearance();
  const openingPending =
    destination.workspaceUnresolved || !("settingsChip" in settings) || composer.draft.kind === "loading";
  const host = useMemo(
    () => ({ logger, ...composer.observation, ...destination.worktreeObservation }),
    [logger, composer.observation, destination.worktreeObservation],
  );
  const chips = (
    <div className="flex min-w-0 items-center gap-[var(--space-1)]">
      {openingPending && openingVisible && <Spinner size="sm" testID="chat-opening-controls" />}
      {"settingsChip" in settings ? settings.settingsChip : null}
      {destination.workspace !== null && (
        <ChatWorkspaceChip
          catalog={destination.workspaceCatalog}
          open={destination.workspaceOpen}
          setOpen={destination.setWorkspaceOpen}
          selected={destination.workspace}
          pending={destination.firstActionPending}
          loading={false}
          select={destination.selectWorkspace}
        />
      )}
    </div>
  );
  return (
    <ChatRuntimeProvider api={api} target={target?.kind === "session" ? target : null} host={host}>
      <ChatDestinationShell
        destination={destination}
        chips={chips}
        openingVisible={openingVisible}
        navigation={props.navigation}
      />
    </ChatRuntimeProvider>
  );
}

function ChatDestinationShell({
  destination,
  chips,
  openingVisible,
  navigation,
}: Readonly<{
  destination: ReturnType<typeof useChatDestination>;
  chips: ReactNode;
  openingVisible: boolean;
  navigation: ChatSettingsNavigation;
}>) {
  const { sessionName, goal, mainView, activeProcessCount } = useChatRuntimePresentation();
  const { t } = useTranslation();
  const { api } = useAppServices();
  const client = useQueryClient();
  const { push } = useStatusController();
  const target = destination.target?.kind === "session" ? destination.target : null;
  const executionTarget = mainView.kind === "session" ? (mainView.data?.executionTarget ?? null) : null;
  const worktrees = useWorktreeList(target?.sessionID ?? null, executionTarget, "chat-label");
  const newGoal = destination.goalState;
  const shownGoal =
    target !== null
      ? goal
      : newGoal.kind === "unresolved" && newGoal.ready
        ? { goal: null, availability: newGoal.availability }
        : null;
  const editModel = useMemo(
    () => createChatMessageEditViewModel({ api: api.chat, client, target, t, push }),
    [api, client, target, t, push],
  );
  const edit = useChatMessageEditActions(editModel);
  const editRequest = useAtomValue(editModel.request);
  const state: ChatShellState = editRequest.isPending
    ? { kind: "loading" }
    : chatReadState(destination, mainView, worktrees, t);
  return (
    <ChatComposerSurface composer={destination.composer} enabled={!editRequest.isPending}>
      <ChatShell
        selectedSession={destination.target}
        sessionName={sessionName}
        state={state}
        content={() =>
          target === null ? null : (
            <ChatTranscriptContent
              openingVisible={openingVisible}
              edit={{
                onEdit: (item) => {
                  edit.activate({
                    item,
                    draft: destination.composer.text,
                    onSuccess: async ({ sessionID }) => navigation.openParentSession(sessionID),
                  });
                },
              }}
            />
          )
        }
        composer={(_, layout) => (
          <ChatComposer
            {...layout}
            editorRef={destination.editorRef}
            settings={destination.settings}
            settingsChip={chips}
            underControls={
              <div className="chat-under-controls flex min-w-0 flex-wrap items-center gap-[var(--space-2)] pt-[var(--space-2)]">
                {shownGoal !== null && <GoalAffordance goal={shownGoal} onActivate={destination.openGoal} />}
                {target !== null && (
                  <ChatProcessesChip key={target.sessionID} target={target} count={activeProcessCount} />
                )}
                {target !== null && (
                  <WorktreeControl sessionID={target.sessionID} target={executionTarget} query={worktrees} />
                )}
              </div>
            }
          />
        )}
      />
      {destination.commandPresentation}
    </ChatComposerSurface>
  );
}

function chatReadState(
  destination: ReturnType<typeof useChatDestination>,
  mainView: ReturnType<typeof useChatRuntimePresentation>["mainView"],
  worktrees: ReturnType<typeof useWorktreeList>,
  t: TFunction,
): ChatShellState {
  const readFailure = (error: unknown, onRetry: () => void): ChatShellState => ({
    kind: "error",
    diagnostic: chatOperationFailureMessage(t, error, "opening"),
    details: errorMessage(error),
    onRetry,
  });
  const { settings, composer } = destination;
  if ("error" in settings) return readFailure(settings.error, settings.retry);
  if (composer.draft.kind === "failed") return readFailure(composer.draft.error, composer.retryDraft);
  if (mainView.kind === "session" && mainView.status === "error")
    return readFailure(mainView.error, () => {
      void mainView.retry();
    });
  if (composer.pending.query.isError)
    return readFailure(composer.pending.query.error, composer.pending.refresh);
  const workspaceFailure = workspaceReadFailure(destination, t);
  if (workspaceFailure !== null) return workspaceFailure;
  if (worktrees.isError) return readFailure(worktrees.error, worktrees.refresh);
  return { kind: "ready" };
}

function workspaceReadFailure(
  destination: ReturnType<typeof useChatDestination>,
  t: TFunction,
): ChatShellState | null {
  const catalog = destination.workspaceCatalog;
  if (
    !(destination.workspaceUnresolved || destination.workspaceOpen) ||
    !(catalog.isError || destination.defaultWorkspaceMissing) ||
    catalog.isFetchNextPageError ||
    catalog.isFetchPreviousPageError
  )
    return null;
  return {
    kind: "error",
    diagnostic: catalog.isError
      ? chatOperationFailureMessage(t, catalog.error, "opening")
      : t("chat.defaultWorkspaceMissing"),
    onRetry: () => {
      void catalog.refetch();
    },
  };
}
