import type { ReactNode } from "react";

import { Island } from "./Island";

export function PeekingSurface({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <Island
      className="relative mx-[var(--radius-xl)] -mb-[var(--space-4)] overflow-hidden pb-[var(--space-4)]"
      level={1}
      radius="l"
      unpadded
    >
      {children}
    </Island>
  );
}
