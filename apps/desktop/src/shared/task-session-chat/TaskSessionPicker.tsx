import { useTranslation } from "react-i18next";
import type { TaskDetail } from "@/api";
import type { SessionChatOpeningIntent } from "@/app-facade";
import { Button, PopoverContent, Spinner } from "@/ui";

export function TaskSessionPicker({
  sessions,
  onOpen,
  defaultIntent,
  nativeWindowsAvailable,
  pendingSessionID,
}: Readonly<{
  sessions: TaskDetail["liveSessions"];
  onOpen(sessionID: string, intent: SessionChatOpeningIntent): void;
  defaultIntent: SessionChatOpeningIntent;
  nativeWindowsAvailable: boolean;
  pendingSessionID: string | null;
}>) {
  const { t } = useTranslation();
  return (
    <PopoverContent
      align="start"
      collisionPadding={12}
      level={3}
      className="w-[min(26rem,calc(100vw-24px))] gap-0 p-[var(--space-2)]"
    >
      <div className="grid max-h-[min(20rem,var(--radix-popover-content-available-height))] min-w-0 gap-[var(--space-1)] overflow-y-auto">
        {sessions.map((session) => {
          const action = t("task.openChat", { name: session.displayName });
          const tooltip = nativeWindowsAvailable ? t("app.shiftClickToOpenInNewWindow", { action }) : action;
          return (
            <Button
              key={session.sessionID}
              aria-label={session.displayName}
              disabled={pendingSessionID === session.sessionID}
              title={tooltip}
              variant="secondary"
              className="w-full justify-start rounded-[var(--radius-s)] border-0 bg-transparent text-left font-sans text-sm hover:bg-[var(--color-island-2)]"
              onClick={(event) => {
                onOpen(
                  session.sessionID,
                  event.shiftKey && nativeWindowsAvailable ? "native_window" : defaultIntent,
                );
              }}
            >
              {pendingSessionID === session.sessionID && <Spinner size="sm" />}
              <span className="min-w-0 truncate">{session.displayName}</span>
            </Button>
          );
        })}
      </div>
    </PopoverContent>
  );
}
