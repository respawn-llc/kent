import type { ReactNode } from "react";

import { cx } from "./classes";

export type BadgeTone = "neutral" | "info" | "success" | "warning" | "danger";

export type BadgeProps = Readonly<{
  children: ReactNode;
  tone?: BadgeTone;
  title?: string;
}>;

export function Badge({ children, tone = "neutral", title }: BadgeProps) {
  return (
    <span className={cx("ui-badge", `ui-badge--${tone}`)} title={title}>
      {children}
    </span>
  );
}
