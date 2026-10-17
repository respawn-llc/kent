import { ChevronRight } from "lucide-react";
import { useAtomValue } from "@effect/atom-react";
import { useTranslation } from "react-i18next";

import { errorMessage, sessionDisplayID, type ChatSettingsSessionFacts as SessionFacts } from "@/api";
import { useAppServices, useNativeChatOpening } from "@/app-facade";
import { writeClipboardText } from "@/shared/native-clipboard";
import { showStatusToast, Spinner, Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/ui";

import { SettingsRow } from "./ChatSettingsRows";
import type { ChatSettingsNavigation } from "./useChatSettings";

export function ChatSettingsSessionFacts({
  facts,
  navigation,
  closeAndNavigate,
}: Readonly<{
  facts: SessionFacts;
  navigation: ChatSettingsNavigation;
  closeAndNavigate(action: () => void): void;
}>) {
  const { t } = useTranslation();
  const { nativeBridge } = useAppServices();
  const nativeChat = useNativeChatOpening();
  const { sessionID, previousSessionID, task, connection } = facts;
  const parentPending = useAtomValue(nativeChat.pending(previousSessionID));
  const displayID = sessionDisplayID(sessionID, facts.shortID);
  const parentLabel = t("chatSettings.toParentChat");
  const parentTooltip = nativeBridge.capabilities.dialogWindows
    ? t("app.shiftClickToOpenInNewWindow", { action: parentLabel })
    : parentLabel;
  async function copySessionID() {
    try {
      await writeClipboardText(displayID, nativeBridge);
      showStatusToast({
        id: "chat-settings-copy",
        tone: "success",
        title: t("chatSettings.copied"),
        durationMs: 2000,
      });
    } catch (error) {
      showStatusToast({
        id: "chat-settings-copy",
        tone: "danger",
        title: t("chatSettings.copyFailed"),
        body: errorMessage(error),
      });
    }
  }
  return (
    <div className="mt-[var(--space-1)] border-t border-[var(--color-outline)] pt-[var(--space-1)]">
      {previousSessionID === null ? null : (
        <SettingsRow
          as="button"
          title={parentPending ? t("app.openingChatWindow") : parentTooltip}
          onActivate={(event) => {
            if (parentPending) return;
            if (event.shiftKey && nativeBridge.capabilities.dialogWindows) {
              void navigation.openParentSession(previousSessionID, "native_window");
              return;
            }
            closeAndNavigate(() => {
              void navigation.openParentSession(previousSessionID);
            });
          }}
          reason={undefined}
        >
          <span className="min-w-0 flex-1 truncate">{parentLabel}</span>
          {parentPending ? (
            <Spinner size="sm" />
          ) : (
            <ChevronRight className="shrink-0 text-[var(--color-muted)]" size={14} />
          )}
        </SettingsRow>
      )}
      {task === null ? null : (
        <SettingsRow
          as="button"
          onActivate={() => {
            closeAndNavigate(() => {
              navigation.openTask(task.taskID);
            });
          }}
          reason={undefined}
        >
          <span className="min-w-0 flex-1 truncate font-mono">{task.shortID}</span>
          <ChevronRight className="shrink-0 text-[var(--color-muted)]" size={14} />
        </SettingsRow>
      )}
      {connection === undefined ? null : (
        <SettingsRow reason={undefined}>
          <span className="min-w-0 flex-1 truncate text-xs text-[var(--color-muted)]">
            {t("chatSettings.sessionConnection", {
              id: connection.id,
              mode: t(`chatSettings.connectionModes.${connection.mode}`),
            })}
          </span>
        </SettingsRow>
      )}
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger asChild>
            <div>
              <SettingsRow
                as="button"
                onActivate={() => {
                  void copySessionID();
                }}
                reason={undefined}
              >
                <span className="min-w-0 flex-1 truncate font-mono text-xs text-[var(--color-muted)]">
                  {displayID}
                </span>
              </SettingsRow>
            </div>
          </TooltipTrigger>
          <TooltipContent>{t("chatSettings.clickToCopy")}</TooltipContent>
        </Tooltip>
      </TooltipProvider>
    </div>
  );
}
