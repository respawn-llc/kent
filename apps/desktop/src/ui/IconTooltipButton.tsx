import type { MouseEventHandler, ReactNode } from "react";

import { Button, type ButtonSize, type ButtonVariant } from "./Button";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "./radix/tooltip";
import { Spinner } from "./Spinner";

export type IconTooltipButtonProps = Readonly<{
  label: string;
  tooltip?: string;
  onClick: MouseEventHandler<HTMLButtonElement>;
  children: ReactNode;
  disabled?: boolean | undefined;
  loading?: boolean | undefined;
  size?: ButtonSize | undefined;
  variant?: ButtonVariant | undefined;
}>;

/**
 * Icon button with a styled hover tooltip. The button is wrapped in a `span`
 * trigger because {@link Button} renders a plain element without ref forwarding,
 * which Radix's `asChild` requires; the span also keeps the tooltip anchored
 * while a disabled button suppresses its own pointer events.
 */
export function IconTooltipButton({
  children,
  disabled,
  loading = false,
  label,
  tooltip = label,
  onClick,
  size = "icon",
  variant = "ghost",
}: IconTooltipButtonProps) {
  return (
    <TooltipProvider delayDuration={0}>
      <Tooltip>
        <TooltipTrigger asChild>
          <span className="inline-flex">
            <Button
              aria-label={label}
              aria-busy={loading}
              disabled={disabled}
              onClick={onClick}
              size={size}
              variant={variant}
            >
              {loading ? <Spinner size="sm" /> : children}
            </Button>
          </span>
        </TooltipTrigger>
        <TooltipContent>{tooltip}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
