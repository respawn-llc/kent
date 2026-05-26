import type { HTMLAttributes, ReactNode } from "react";

import { cx } from "./classes";

export type IslandProps = Readonly<{
  children: ReactNode;
  floatingWidth?: "default" | "full";
  tone?: "primary" | "secondary" | "floating";
  unpadded?: boolean;
}> &
  HTMLAttributes<HTMLElement>;

export function Island({
  children,
  className,
  floatingWidth = "default",
  tone = "primary",
  unpadded = false,
  ...props
}: IslandProps) {
  return (
    <section
      className={cx(
        "app-region-no-drag island-glass rounded-[var(--radius-xl)]",
        !unpadded && "p-[var(--space-4)]",
        tone === "secondary" && "bg-[var(--color-island-1)] shadow-none",
        tone === "floating" && floatingWidth === "default" && "m-auto max-w-[760px]",
        className,
      )}
      {...props}
    >
      {children}
    </section>
  );
}
