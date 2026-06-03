import { useId, type ReactNode } from "react";
import { Maximize2, Minus } from "lucide-react";

import { cx } from "./classes";
import { islandSurfaceClassName, type IslandLevel } from "./islandSurfaceStyles";

export type FloatingNoticeTone = "danger" | "neutral";

export type FloatingNoticeIslandProps = Readonly<{
  children: ReactNode;
  className?: string | undefined;
  collapsed: boolean;
  collapseLabel: string;
  expandedClassName?: string | undefined;
  expandLabel: string;
  icon?: ReactNode;
  onCollapsedChange: (collapsed: boolean) => void;
  positionClassName?: string | undefined;
  positionStrategy?: "absolute" | "fixed" | undefined;
  level?: IslandLevel | undefined;
  title: string;
  tone?: FloatingNoticeTone;
}>;

export function FloatingNoticeIsland({
  children,
  className,
  collapsed,
  collapseLabel,
  expandedClassName,
  expandLabel,
  icon,
  level = 1,
  onCollapsedChange,
  positionClassName = "right-[var(--space-4)] bottom-[var(--space-4)]",
  positionStrategy = "fixed",
  title,
  tone = "danger",
}: FloatingNoticeIslandProps) {
  const titleID = useId();
  const styles = noticeToneStyles[tone];
  const expandedClasses =
    expandedClassName ??
    "floating-notice-expanded grid h-[176px] w-[min(420px,calc(100vw-32px))] gap-[6px] overflow-hidden rounded-[var(--radius-xl)] p-[var(--space-3)]";

  return (
    <aside
      aria-label={collapsed ? title : undefined}
      aria-labelledby={collapsed ? undefined : titleID}
      className={cx(
        "floating-notice-morph app-region-no-drag z-50",
        positionStrategy,
        islandSurfaceClassName(level),
        collapsed
          ? cx(
            "floating-notice-collapsed grid h-12 w-12 place-items-center overflow-hidden rounded-[var(--radius-m)] p-0",
            styles.collapsedTextClassName,
          )
          : expandedClasses,
        positionClassName,
        styles.borderClassName,
        collapsed ? styles.collapsedClassName : undefined,
        className,
      )}
    >
      {collapsed ? (
        <button
          aria-label={expandLabel}
          className={cx(
            "grid h-full w-full place-items-center rounded-[var(--radius-m)] border border-transparent bg-transparent",
            styles.collapsedTextClassName,
          )}
          onClick={() => {
            onCollapsedChange(false);
          }}
          type="button"
        >
          {icon ?? <Maximize2 aria-hidden="true" size={24} strokeWidth={1.7} />}
        </button>
      ) : (
        <>
          <header
            className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-[var(--space-2)] leading-none"
            data-testid="floating-notice-header"
          >
            <h2 className={cx("m-0 text-lg font-bold leading-none", styles.titleClassName)} id={titleID}>
              {title}
            </h2>
            <button
              aria-label={collapseLabel}
              className="grid h-[18px] w-[18px] place-items-center rounded-full border border-transparent bg-transparent text-[var(--color-on-island)]"
              onClick={() => {
                onCollapsedChange(true);
              }}
              type="button"
            >
              <Minus aria-hidden="true" size={18} strokeWidth={1.5} />
            </button>
          </header>
          {children}
        </>
      )}
    </aside>
  );
}

const noticeToneStyles: Record<
  FloatingNoticeTone,
  Readonly<{
    borderClassName: string;
    collapsedClassName: string;
    collapsedTextClassName: string;
    titleClassName: string;
  }>
> = {
  danger: {
    borderClassName: "border-[var(--color-error)]",
    collapsedClassName: "floating-notice-collapsed-danger",
    collapsedTextClassName: "text-[var(--color-on-error)]",
    titleClassName: "text-[var(--color-error)]",
  },
  neutral: {
    borderClassName: "border-[var(--color-outline)]",
    collapsedClassName: "floating-notice-collapsed-neutral",
    collapsedTextClassName: "text-[var(--color-on-island)]",
    titleClassName: "text-[var(--color-on-island)]",
  },
};
