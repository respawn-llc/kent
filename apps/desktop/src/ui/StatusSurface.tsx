import type { ReactNode } from "react";

import { Badge } from "./Badge";
import { Button } from "./Button";

export type StatusNotice = Readonly<{
  id: string;
  tone: "info" | "success" | "warning" | "danger";
  title: string;
  body: string;
  actionLabel?: string;
  onAction?: () => void;
}>;

export type StatusSurfaceProps = Readonly<{
  notices: readonly StatusNotice[];
  dismissLabel: string;
  children?: ReactNode;
  onDismiss: (id: string) => void;
}>;

export function StatusSurface({ notices, dismissLabel, children, onDismiss }: StatusSurfaceProps) {
  return (
    <>
      {children}
      <div aria-live="polite" className="status-surface">
        {notices.map((notice) => (
          <article className="status-notice" key={notice.id}>
            <Badge tone={notice.tone}>{notice.title}</Badge>
            <p>{notice.body}</p>
            <div className="status-notice__actions">
              {notice.actionLabel !== undefined && notice.onAction !== undefined ? (
                <Button onClick={notice.onAction} variant="ghost">
                  {notice.actionLabel}
                </Button>
              ) : null}
              <Button onClick={() => { onDismiss(notice.id); }} variant="ghost">
                {dismissLabel}
              </Button>
            </div>
          </article>
        ))}
      </div>
    </>
  );
}
