import type { ReactNode } from "react";

import { cx } from "./classes";

export function CopyableValueButton({
  accessibleLabel,
  children,
  className,
  onActivate,
  size = "sm",
  unpadded = false,
}: Readonly<{
  accessibleLabel: string;
  children: ReactNode;
  className?: string;
  onActivate: () => void;
  size?: "sm" | "xs";
  unpadded?: boolean;
}>) {
  return (
    <button
      aria-label={accessibleLabel}
      className={cx(
        "min-w-0 whitespace-pre-wrap rounded-[var(--radius-m)] text-left text-[var(--color-muted)] transition-colors hover:bg-[var(--color-island-2)] hover:text-[var(--color-on-island)] focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--color-primary)]",
        !unpadded && "-mx-[var(--space-1)] px-[var(--space-1)] py-[var(--space-1)]",
        size === "xs" ? "text-xs" : "text-sm",
        className,
      )}
      onClick={onActivate}
      type="button"
    >
      {children}
    </button>
  );
}
