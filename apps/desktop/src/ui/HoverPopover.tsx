import { useCallback, type ComponentPropsWithRef, type ReactNode } from "react";
import {
  autoUpdate,
  flip,
  FloatingPortal,
  offset,
  safePolygon,
  shift,
  size,
  useDismiss,
  useFloating,
  useHover,
  useInteractions,
  useTransitionStatus,
} from "@floating-ui/react";
import { cn } from "./classes";
import { motionDurationFromCSSVar } from "./motion";
import { PopoverBody, popoverLayoutClassName } from "./popoverPresentation";
import { radixIslandSurfaceContentClassName } from "./radix/radix-island-surface";

export function HoverPopover({
  open,
  onOpenChange,
  trigger,
  children,
  side = "bottom",
  align = "start",
  className,
}: Readonly<{
  open: boolean;
  onOpenChange(open: boolean): void;
  trigger(props: ComponentPropsWithRef<"button">): ReactNode;
  children: ReactNode;
  side?: "top" | "bottom" | "left" | "right";
  align?: "start" | "end";
  className?: string;
}>) {
  const { refs, floatingStyles, context, placement } = useFloating({
    open,
    onOpenChange,
    placement: `${side}-${align}`,
    whileElementsMounted: autoUpdate,
    middleware: [
      offset(8),
      flip(),
      shift({ padding: 12 }),
      size({
        padding: 12,
        apply({ availableWidth, availableHeight, elements }) {
          elements.floating.style.setProperty(
            "--hover-popover-available-width",
            `${Math.max(0, availableWidth).toString()}px`,
          );
          elements.floating.style.setProperty(
            "--hover-popover-available-height",
            `${Math.max(0, availableHeight).toString()}px`,
          );
        },
      }),
    ],
  });
  const hover = useHover(context, { mouseOnly: true, handleClose: safePolygon() });
  const dismiss = useDismiss(context);
  const { getReferenceProps, getFloatingProps } = useInteractions([hover, dismiss]);
  const { isMounted } = useTransitionStatus(context, {
    duration: motionDurationFromCSSVar("--motion-fast", 140),
  });
  const setReference = useCallback(
    (element: HTMLButtonElement | null) => {
      refs.setReference(element);
    },
    [refs],
  );
  const setFloating = useCallback(
    (element: HTMLDivElement | null) => {
      refs.setFloating(element);
    },
    [refs],
  );
  return (
    <>
      {trigger({ ...getReferenceProps(), ref: setReference })}
      {isMounted && (
        <FloatingPortal>
          <div
            ref={setFloating}
            inert={!open}
            style={floatingStyles}
            className="z-50"
            role="dialog"
            {...getFloatingProps()}
          >
            <div
              className={cn(
                radixIslandSurfaceContentClassName({ originClassName: "origin-center" }),
                popoverLayoutClassName,
                className,
              )}
              data-state={open ? "open" : "closed"}
              data-side={placement.split("-")[0]}
            >
              <PopoverBody>{children}</PopoverBody>
            </div>
          </div>
        </FloatingPortal>
      )}
    </>
  );
}
