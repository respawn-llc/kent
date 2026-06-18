import { CircleQuestionMark, type LucideIcon } from "lucide-react";
import type { ReactNode } from "react";

import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "../components/ui/tooltip";
import { cx } from "./classes";
import type { IslandLevel } from "./islandSurfaceStyles";

export type HelpHintProps = Readonly<{
  /**
   * Tooltip content shown on hover/focus. Pass already-resolved copy
   * (e.g. `t("some.key")`); the component stays i18n-agnostic so callers
   * own translation and interpolation.
   */
  label: ReactNode;
  /**
   * Icon rendered in both the trigger and the tooltip. Defaults to a circled
   * question mark; pass any Lucide icon to customize.
   */
  icon?: LucideIcon | undefined;
  /**
   * Accessible name for the trigger button. Defaults to `label` when it is a
   * plain string; provide explicitly when `label` is rich content.
   */
  ariaLabel?: string | undefined;
  /** Tooltip placement relative to the trigger. */
  side?: "top" | "right" | "bottom" | "left" | undefined;
  /** Surface depth for the tooltip background. */
  level?: IslandLevel | undefined;
  /** Extra classes for the trigger hit area. */
  className?: string | undefined;
}>;

/** Diameter of the rendered question/help glyph, per design spec. */
const ICON_SIZE = 14;

/**
 * Faint circled help icon with a generous 40x40 hit/hover area. Hovering or
 * focusing the trigger fades the glyph to full strength and reveals the shared
 * tooltip, which repeats the icon alongside the help copy. Newlines in a string
 * `label` are preserved so multi-line hints lay out as written.
 */
export function HelpHint({
  ariaLabel,
  className,
  icon: Icon = CircleQuestionMark,
  label,
  level = 0,
  side = "top",
}: HelpHintProps) {
  const accessibleName = ariaLabel ?? (typeof label === "string" ? label : undefined);

  return (
    <TooltipProvider delayDuration={0}>
      <Tooltip>
        <TooltipTrigger asChild>
          <button
            aria-label={accessibleName}
            className={cx(
              "group grid size-10 shrink-0 place-items-center rounded-full border border-transparent bg-transparent text-[var(--color-muted)] transition-colors hover:text-[var(--color-on-island)] focus-visible:border-[var(--color-primary)] focus-visible:text-[var(--color-on-island)] focus-visible:outline-none",
              className,
            )}
            data-slot="help-hint-trigger"
            type="button"
          >
            <Icon aria-hidden="true" size={ICON_SIZE} strokeWidth={1.7} />
          </button>
        </TooltipTrigger>
        <TooltipContent className="max-w-xs items-start gap-2" level={level} side={side} sideOffset={6}>
          <Icon
            aria-hidden="true"
            className="mt-px shrink-0 text-[var(--color-muted)]"
            size={ICON_SIZE}
            strokeWidth={1.7}
          />
          <span className="min-w-0 whitespace-pre-line leading-snug">{label}</span>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
