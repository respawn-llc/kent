"use client";

import * as React from "react";
import * as PopoverPrimitive from "@radix-ui/react-popover";

import { cn } from "../classes";
import { AnimatedSize } from "../AnimatedSize";
import { motionDurationFromCSSVar } from "../motion";
import { radixIslandSurfaceContentClassName } from "./radix-island-surface";
import type { IslandLevel } from "../islandSurfaceStyles";

function Popover({ ...props }: React.ComponentProps<typeof PopoverPrimitive.Root>) {
  return <PopoverPrimitive.Root data-slot="popover" {...props} />;
}

function PopoverTrigger({ ...props }: React.ComponentProps<typeof PopoverPrimitive.Trigger>) {
  return <PopoverPrimitive.Trigger data-slot="popover-trigger" {...props} />;
}

function PopoverContent({
  align = "center",
  className,
  level,
  ref,
  children,
  sideOffset = 8,
  ...props
}: React.ComponentProps<typeof PopoverPrimitive.Content> & Readonly<{ level?: IslandLevel | undefined }>) {
  return (
    <PopoverPrimitive.Portal>
      <PopoverPrimitive.Content
        align={align}
        className={cn(
          radixIslandSurfaceContentClassName({
            level,
            originClassName: "origin-(--radix-popover-content-transform-origin)",
          }),
          "grid w-64 gap-[var(--space-3)] rounded-[var(--radius-l)] p-[var(--space-3)] text-[var(--color-on-island)]",
          className,
        )}
        data-slot="popover-content"
        ref={ref}
        sideOffset={sideOffset}
        {...props}
      >
        <AnimatedSize
          className="justify-start gap-[inherit]"
          contentClassName="grid min-h-0 gap-[inherit]"
          transition={{
            duration: motionDurationFromCSSVar("--motion-fast", 140) / 1000,
            ease: "easeInOut",
          }}
        >
          {children}
        </AnimatedSize>
      </PopoverPrimitive.Content>
    </PopoverPrimitive.Portal>
  );
}

function PopoverClose({ ...props }: React.ComponentProps<typeof PopoverPrimitive.Close>) {
  return <PopoverPrimitive.Close data-slot="popover-close" {...props} />;
}

export { Popover, PopoverClose, PopoverContent, PopoverTrigger };
