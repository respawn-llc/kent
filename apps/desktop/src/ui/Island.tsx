import type { HTMLAttributes, ReactNode } from "react";

import { cx } from "./classes";

export type IslandProps = Readonly<{
  children: ReactNode;
  tone?: "primary" | "secondary" | "floating";
}> &
  HTMLAttributes<HTMLElement>;

export function Island({ children, className, tone = "primary", ...props }: IslandProps) {
  return (
    <section className={cx("ui-island", `ui-island--${tone}`, className)} {...props}>
      {children}
    </section>
  );
}
