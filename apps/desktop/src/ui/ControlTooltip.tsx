import type { ReactNode } from "react";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "./radix/tooltip";

export function ControlTooltip({
  children,
  label,
  className = "inline-flex",
}: Readonly<{
  children: ReactNode;
  label: string;
  className?: string;
}>) {
  return (
    <TooltipProvider delayDuration={0}>
      <Tooltip>
        <TooltipTrigger asChild>
          <span className={className}>{children}</span>
        </TooltipTrigger>
        <TooltipContent>{label}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
