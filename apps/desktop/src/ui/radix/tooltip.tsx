import * as React from "react";
import {
  arrow,
  autoUpdate,
  flip,
  FloatingArrow,
  FloatingNode,
  FloatingPortal,
  offset,
  safePolygon,
  shift,
  useDismiss,
  useFloating,
  useFloatingNodeId,
  useFloatingRootContext,
  useHover,
  useInteractions,
  useMergeRefs,
  useTransitionStatus,
} from "@floating-ui/react";

import { cn } from "../classes";
import { FloatingTreeScope } from "../FloatingTreeScope";
import { motionDurationFromCSSVar } from "../motion";
import { radixIslandSurfaceContentClassName } from "./radix-island-surface";
import type { IslandLevel } from "../islandSurfaceStyles";

const TooltipDelayContext = React.createContext(0);

function TooltipProvider({
  delayDuration = 0,
  children,
}: Readonly<{ delayDuration?: number; children: React.ReactNode }>) {
  return <TooltipDelayContext value={delayDuration}>{children}</TooltipDelayContext>;
}

type TooltipProps = Readonly<{
  children: React.ReactNode;
  open?: boolean;
  defaultOpen?: boolean;
  onOpenChange?(open: boolean): void;
  delayDuration?: number;
}>;

function Tooltip(props: TooltipProps) {
  return (
    <FloatingTreeScope>
      <TooltipNode {...props} />
    </FloatingTreeScope>
  );
}

function useTooltipController({
  open: controlledOpen,
  defaultOpen = false,
  onOpenChange,
  delayDuration,
}: TooltipProps) {
  const [uncontrolledOpen, setOpen] = React.useState(defaultOpen);
  const open = controlledOpen ?? uncontrolledOpen;
  const [reference, setReference] = React.useState<Element | null>(null);
  const [floating, setFloating] = React.useState<HTMLElement | null>(null);
  const nodeId = useFloatingNodeId();
  const providerDelay = React.useContext(TooltipDelayContext);
  const context = useFloatingRootContext({
    open,
    onOpenChange(nextOpen) {
      if (controlledOpen === undefined) setOpen(nextOpen);
      onOpenChange?.(nextOpen);
    },
    elements: { reference, floating },
  });
  const hover = useHover(context, {
    mouseOnly: true,
    delay: { open: delayDuration ?? providerDelay },
    handleClose: safePolygon({ requireIntent: false }),
  });
  const dismiss = useDismiss(context, { referencePress: true });
  return {
    context,
    nodeId,
    setReference,
    setFloating,
    ...useInteractions([hover, dismiss]),
  };
}

const TooltipContext = React.createContext<ReturnType<typeof useTooltipController> | null>(null);

function useTooltipContext() {
  const context = React.useContext(TooltipContext);
  if (!context) throw new Error("Tooltip components require a Tooltip parent");
  return context;
}

function TooltipNode(props: TooltipProps) {
  const controller = useTooltipController(props);
  return (
    <FloatingNode id={controller.nodeId}>
      <TooltipContext value={controller}>{props.children}</TooltipContext>
    </FloatingNode>
  );
}

function TooltipTrigger({
  asChild = false,
  children,
  ref,
  ...props
}: React.ComponentPropsWithRef<"button"> & Readonly<{ asChild?: boolean }>) {
  const { setReference, getReferenceProps } = useTooltipContext();
  const child = asChild ? React.Children.only(children) : null;
  if (child !== null && !React.isValidElement<React.ComponentPropsWithRef<"button">>(child)) {
    throw new Error("TooltipTrigger asChild requires a React element");
  }
  const mergedRef = useMergeRefs([setReference, ref, child?.props.ref]);
  const triggerProps = getReferenceProps({
    ...child?.props,
    ...props,
    ref: mergedRef,
  });
  const slottedProps = { ...triggerProps, ref: mergedRef, "data-slot": "tooltip-trigger" };
  return child ? (
    React.cloneElement(child, slottedProps)
  ) : (
    <button type="button" {...slottedProps}>
      {children}
    </button>
  );
}

function TooltipContent({
  className,
  level = 0,
  showArrow = false,
  side = "top",
  align = "center",
  sideOffset = 0,
  children,
  ref,
  style,
  ...props
}: React.ComponentPropsWithRef<"div"> &
  Readonly<{
    level?: IslandLevel | undefined;
    showArrow?: boolean | undefined;
    side?: "top" | "bottom" | "left" | "right" | undefined;
    align?: "start" | "center" | "end" | undefined;
    sideOffset?: number | undefined;
  }>) {
  const { context: rootContext, nodeId, setFloating, getFloatingProps } = useTooltipContext();
  const [arrowElement, setArrowElement] = React.useState<SVGSVGElement | null>(null);
  const { refs, context, floatingStyles, placement } = useFloating({
    rootContext,
    nodeId,
    placement: align === "center" ? side : `${side}-${align}`,
    whileElementsMounted: autoUpdate,
    middleware: [
      offset(sideOffset),
      flip(),
      shift({ padding: 8 }),
      ...(showArrow ? [arrow({ element: arrowElement })] : []),
    ],
  });
  const setPositionedFloating = React.useCallback(
    (element: HTMLDivElement | null) => {
      refs.setFloating(element);
    },
    [refs],
  );
  const mergedRef = useMergeRefs([setPositionedFloating, setFloating, ref]);
  const { isMounted } = useTransitionStatus(context, {
    duration: motionDurationFromCSSVar("--motion-fast", 140),
  });
  if (!isMounted) return null;
  return (
    <FloatingPortal>
      <div
        {...getFloatingProps()}
        ref={mergedRef}
        role="tooltip"
        inert={!context.open}
        style={floatingStyles}
        className="z-50"
      >
        <div
          {...props}
          data-slot="tooltip-content"
          data-state={context.open ? "open" : "closed"}
          data-side={placement.split("-")[0]}
          style={style}
          className={cn(
            radixIslandSurfaceContentClassName({
              level,
              noDrag: false,
              originClassName:
                "data-[side=top]:origin-bottom data-[side=bottom]:origin-top data-[side=left]:origin-right data-[side=right]:origin-left",
            }),
            "inline-flex w-fit max-w-xs items-center gap-1.5 rounded-[var(--radius-m)] px-3 py-1.5 text-xs text-[var(--color-on-island)] has-data-[slot=kbd]:pr-1.5 **:data-[slot=kbd]:relative **:data-[slot=kbd]:isolate **:data-[slot=kbd]:z-50 **:data-[slot=kbd]:rounded-sm",
            className,
          )}
        >
          {children}
          {showArrow && (
            <FloatingArrow
              ref={setArrowElement}
              context={context}
              className="z-50 fill-[var(--color-island-0)]"
              width={10}
              height={5}
              tipRadius={2}
              data-slot="tooltip-arrow"
              data-testid="tooltip-arrow"
            />
          )}
        </div>
      </div>
    </FloatingPortal>
  );
}

export { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger };
