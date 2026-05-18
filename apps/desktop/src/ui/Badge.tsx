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
    <span
      className={cx(
        "inline-flex items-center rounded-full border border-[var(--color-outline)] px-[10px] py-1 text-[0.78rem] font-extrabold text-[var(--color-muted)]",
        tone === "info" && "text-[var(--color-primary)]",
        tone === "success" && "text-[var(--color-success)]",
        tone === "warning" && "text-[var(--color-warning)]",
        tone === "danger" && "text-[var(--color-error)]",
      )}
      title={title}
    >
      {children}
    </span>
  );
}
