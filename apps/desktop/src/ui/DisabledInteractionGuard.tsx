import type { ReactNode, SyntheticEvent } from "react";

import { cx } from "./classes";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "../components/ui/tooltip";

export type DisabledInteractionGuardProps = Readonly<{
  children: ReactNode;
  disabled: boolean;
  className?: string | undefined;
  reason?: string | undefined;
}>;

export function DisabledInteractionGuard({
  children,
  className,
  disabled,
  reason,
}: DisabledInteractionGuardProps) {
  if (!disabled) {
    return <div className={className}>{children}</div>;
  }
  const trigger = (
    <div
      aria-disabled="true"
      className={cx("cursor-not-allowed [&_*]:pointer-events-none", className)}
      data-disabled="true"
      data-slot="disabled-interaction-guard"
      onClickCapture={stopDisabledInteraction}
      onKeyDownCapture={stopDisabledInteraction}
      onMouseDownCapture={stopDisabledInteraction}
      onPointerDownCapture={stopDisabledInteraction}
    >
      {children}
    </div>
  );
  if (reason === undefined || reason.length === 0) {
    return trigger;
  }
  return (
    <Tooltip>
      <TooltipTrigger asChild>{trigger}</TooltipTrigger>
      <TooltipContent level={3}>{reason}</TooltipContent>
    </Tooltip>
  );
}

function stopDisabledInteraction(event: SyntheticEvent) {
  event.preventDefault();
  event.stopPropagation();
}
