import type { MouseEvent, ReactNode } from "react";

import { cx } from "./classes";

export const homeListCardShellClassName =
  "relative rounded-[var(--radius-l)] border border-[var(--color-outline)] bg-[var(--color-island-1)]";

export const homeListCardButtonClassName =
  "grid w-full gap-[var(--space-1)] p-[var(--space-3)] pr-14 text-left text-[var(--color-on-island)]";

export function HomeListCard({
  action,
  ariaLabel,
  children,
  className,
  onClick,
  title,
}: Readonly<{
  action?: ReactNode | undefined;
  ariaLabel: string;
  children: ReactNode;
  className?: string | undefined;
  onClick: (event: MouseEvent<HTMLButtonElement>) => void;
  title?: string | undefined;
}>) {
  return (
    <article
      className={cx(homeListCardShellClassName, className)}
      data-slot="home-list-card"
      data-testid="home-list-card"
    >
      <button
        aria-label={ariaLabel}
        className={homeListCardButtonClassName}
        data-slot="home-list-card-button"
        data-testid="home-list-card-button"
        onClick={onClick}
        title={title}
        type="button"
      >
        {children}
      </button>
      {action}
    </article>
  );
}
