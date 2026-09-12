import { useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { useWindowChromeTitle, type SessionChatTarget } from "@/app-facade";
import { ErrorState } from "@/ui";

export type SelectedSession = Pick<SessionChatTarget, "projectID" | "sessionID">;

export type ChatShellState =
  | Readonly<{ kind: "ready" }>
  | Readonly<{
      kind: "error";
      diagnostic?: ReactNode;
      onRetry: () => void;
    }>;

export type ChatShellProps = Readonly<{
  composer: (session: SelectedSession, layout: ChatComposerLayout) => ReactNode;
  content: (session: SelectedSession) => ReactNode;
  selectedSession: SelectedSession;
  sessionName: string | null;
  state: ChatShellState;
  onComposerHeightChange?: (height: number) => void;
}>;
export type ChatComposerLayout = Readonly<{
  availableHeight: number | null;
  onHeightChange(height: number): void;
}>;
const ignoreHeight = () => {
  /* Production viewport integration supplies the height callback. */
};

export function ChatShell({
  composer,
  content,
  selectedSession,
  sessionName,
  state,
  onComposerHeightChange = ignoreHeight,
}: ChatShellProps) {
  const { t } = useTranslation();
  useWindowChromeTitle(sessionName);
  const container = useRef<HTMLDivElement>(null);
  const [availableHeight, setAvailableHeight] = useState<number | null>(null);
  useLayoutEffect(() => {
    const element = container.current;
    if (element === null) return;
    const measure = () => {
      setAvailableHeight(element.getBoundingClientRect().height);
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => {
      observer.disconnect();
    };
  }, [state.kind]);

  if (state.kind === "error") {
    return (
      <div className="flex h-full min-h-0 flex-col" data-testid="chat-shell">
        <div className="min-h-0 flex-1">
          <ErrorState
            body={state.diagnostic}
            onRetry={state.onRetry}
            retryLabel={t("app.retry")}
            title={t("states.error")}
          />
        </div>
      </div>
    );
  }

  return (
    <div className="flex h-full min-h-0 flex-col" data-testid="chat-shell" ref={container}>
      <div className="min-h-0 flex-1">{content(selectedSession)}</div>
      <div className="shrink-0">
        {composer(selectedSession, { availableHeight, onHeightChange: onComposerHeightChange })}
      </div>
    </div>
  );
}
