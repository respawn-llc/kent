import type { ReactNode } from "react";

import { Island } from "./Island";

export function PeekingSurface({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <Island
      className="relative mx-[var(--space-3)] -mb-[var(--space-4)] overflow-hidden pb-[var(--space-4)] animate-in fade-in slide-in-from-bottom-1 duration-[var(--motion-fast)]"
      level={1}
      unpadded
    >
      {children}
    </Island>
  );
}
