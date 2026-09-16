import { useMemo, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { useBlocker } from "@tanstack/react-router";
import { errorMessage } from "@/api";
import type { ChatDestinationOpening } from "./ChatDestinationViewModel";
import { ChatRuntimeProvider, useAppServices, useChatRuntimePresentation } from "@/app-facade";
import { ChatShell, type ChatShellState } from "./ChatShell";
import { ChatComposer } from "./ChatComposer";
import { ChatComposerSurface } from "./ChatComposerSurface";
import { useChatDestination } from "./useChatDestination";
import type { ChatSettingsNavigation } from "./useChatSettings";
import { ChatWorkspaceChip } from "./ChatWorkspaceChip";
import {
  Button,
  ErrorState,
  LoadingState,
  Spinner,
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/ui";
import { GoalAffordance } from "./goal";

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
  const { t } = useTranslation();
  const [opened, setOpened] = useState(false);
  if (!opened && "settingsChip" in settings) setOpened(true);
  const host = useMemo(
    () => ({ logger, ...composer.pending.observation }),
    [logger, composer.pending.observation],
  );
  if (!opened) {
    return "error" in settings ? (
      <ErrorState
        title={t("states.error")}
        body={errorMessage(settings.error)}
        onRetry={settings.retry}
        retryLabel={t("app.retry")}
      />
    ) : (
      <LoadingState title={t("states.loading")} />
    );
  }
  const chips = (
    <div className="flex min-w-0 items-center gap-[var(--space-1)]">
      {"settingsChip" in settings ? (
        settings.settingsChip
      ) : "error" in settings ? (
        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button onClick={settings.retry}>{t("app.retry")}</Button>
            </TooltipTrigger>
            <TooltipContent>{errorMessage(settings.error)}</TooltipContent>
          </Tooltip>
        </TooltipProvider>
      ) : (
        <Spinner size="sm" />
      )}
      {destination.workspace !== null && (
        <ChatWorkspaceChip
          projectID={target.projectID}
          selected={destination.workspace}
          pending={destination.firstActionPending}
          loading={settings.kind === "loading-new-chat"}
          select={destination.selectWorkspace}
        />
      )}
    </div>
  );
  return (
    <ChatRuntimeProvider api={api} target={target.kind === "session" ? target : null} host={host}>
      <ChatDestinationShell destination={destination} chips={chips} />
    </ChatRuntimeProvider>
  );
}

function ChatDestinationShell({
  destination,
  chips,
}: Readonly<{
  destination: ReturnType<typeof useChatDestination>;
  chips: ReactNode;
}>) {
  const { sessionName, goal, mainView } = useChatRuntimePresentation();
  const draft = destination.composer.draft;
  const state: ChatShellState =
    draft.kind === "failed"
      ? { kind: "error", diagnostic: errorMessage(draft.error), onRetry: destination.composer.retryDraft }
      : mainView.kind === "session" && mainView.status === "error"
        ? {
            kind: "error",
            diagnostic: errorMessage(mainView.error),
            onRetry: () => {
              void mainView.retry();
            },
          }
        : { kind: "ready" };
  return (
    <ChatComposerSurface composer={destination.composer}>
      <ChatShell
        selectedSession={destination.target}
        sessionName={sessionName}
        state={state}
        content={() => null}
        composer={(_, layout) => (
          <ChatComposer
            {...layout}
            settingsChip={
              <div className="flex min-w-0 items-center gap-[var(--space-1)]">
                {chips}
                <GoalAffordance
                  goal={goal ?? { goal: null, availability: null }}
                  onActivate={destination.openGoal}
                />
              </div>
            }
          />
        )}
      />
    </ChatComposerSurface>
  );
}
