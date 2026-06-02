import { useLocation } from "@tanstack/react-router";
import { X } from "lucide-react";
import {
  useCallback,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent,
  type PointerEvent,
} from "react";
import { useTranslation } from "react-i18next";

import { Button, showStatusToast } from "../ui";
import { cx } from "../ui/classes";
import { ProjectDeleteButton } from "../features/project-edit/ProjectDeleteButton";
import { WorkflowDeleteButton } from "../features/workflow-editor/WorkflowDeleteButton";
import { useAppServices } from "./useAppServices";
import { SidebarDestinationView, sidebarTitle } from "./sidebarDestinations";
import { useSidebar, type SidebarDestination } from "./sidebarContext";
import {
  clampSidebarWidth,
  sidebarMaxWidthRatio,
  sidebarMinWidthPx,
  sidebarResizeBoundsForShellWidth,
  sidebarResizeStepPx,
  type SidebarResizeBounds,
} from "./sidebarSizing";

export function SidebarRouteChangeCloser() {
  const location = useLocation();
  const { activeDestination, closeSidebar } = useSidebar();
  const routeKey = `${location.pathname}?${location.searchStr}`;
  const previousRouteKeyRef = useRef(routeKey);

  useEffect(() => {
    if (previousRouteKeyRef.current !== routeKey) {
      previousRouteKeyRef.current = routeKey;
      if (activeDestination !== null) {
        closeSidebar("route_change");
      }
    }
  }, [activeDestination, closeSidebar, routeKey]);

  return null;
}

export function SidebarHost() {
  const { t } = useTranslation();
  const { activeDestination, closeSidebar, phase, resizeSidebar, resolveSidebar, sidebarWidthPx } =
    useSidebar();
  const titleId = useId();
  const sidebarRef = useRef<HTMLElement | null>(null);
  const resizeDragRef = useRef<SidebarResizeDrag | null>(null);
  const [resizing, setResizing] = useState(false);
  const [resizeBounds, setResizeBounds] = useState(() =>
    sidebarResizeBoundsForShellWidth(fallbackSidebarShellWidth()),
  );

  const sidebarStyle = useMemo<SidebarStyle>(
    () => ({
      "--app-sidebar-inset": "var(--space-2)",
      "--app-sidebar-width": `${sidebarWidthPx.toString()}px`,
    }),
    [sidebarWidthPx],
  );

  const resizeTo = useCallback(
    (widthPx: number) => {
      const nextBounds = sidebarResizeBounds(sidebarRef.current);
      setResizeBounds(nextBounds);
      resizeSidebar(clampSidebarWidth(widthPx, nextBounds.maxWidthPx));
    },
    [resizeSidebar],
  );

  const startResize = useCallback(
    (event: PointerEvent<HTMLDivElement>) => {
      if (event.button !== 0) {
        return;
      }
      event.preventDefault();
      const nextBounds = sidebarResizeBounds(sidebarRef.current);
      setResizeBounds(nextBounds);
      resizeDragRef.current = {
        bounds: nextBounds,
        pointerID: event.pointerId,
        startWidth: sidebarWidthPx,
        startX: event.clientX,
      };
      setPointerCaptureIfAvailable(event.currentTarget, event.pointerId);
      setResizing(true);
    },
    [sidebarWidthPx],
  );

  const resizeFromPointer = useCallback(
    (event: PointerEvent<HTMLDivElement>) => {
      const drag = resizeDragRef.current;
      if (drag?.pointerID !== event.pointerId) {
        return;
      }
      event.preventDefault();
      resizeSidebar(clampSidebarWidth(drag.startWidth + drag.startX - event.clientX, drag.bounds.maxWidthPx));
    },
    [resizeSidebar],
  );

  const stopResize = useCallback((event: PointerEvent<HTMLDivElement>) => {
    const drag = resizeDragRef.current;
    if (drag?.pointerID !== event.pointerId) {
      return;
    }
    resizeDragRef.current = null;
    releasePointerCaptureIfAvailable(event.currentTarget, event.pointerId);
    setResizing(false);
  }, []);

  const resizeWithKeyboard = useCallback(
    (event: KeyboardEvent<HTMLDivElement>) => {
      if (event.key === "ArrowLeft") {
        event.preventDefault();
        resizeTo(sidebarWidthPx + sidebarResizeStepPx);
        return;
      }
      if (event.key === "ArrowRight") {
        event.preventDefault();
        resizeTo(sidebarWidthPx - sidebarResizeStepPx);
        return;
      }
      if (event.key === "Home") {
        event.preventDefault();
        resizeTo(sidebarMinWidthPx);
        return;
      }
      if (event.key === "End") {
        event.preventDefault();
        resizeTo(resizeBounds.maxWidthPx);
      }
    },
    [resizeBounds.maxWidthPx, resizeTo, sidebarWidthPx],
  );

  useEffect(() => {
    if (!resizing) {
      return;
    }
    const previousCursor = document.body.style.cursor;
    const previousUserSelect = document.body.style.userSelect;
    document.body.style.cursor = "ew-resize";
    document.body.style.userSelect = "none";
    return () => {
      document.body.style.cursor = previousCursor;
      document.body.style.userSelect = previousUserSelect;
    };
  }, [resizing]);

  useEffect(() => {
    if (activeDestination === null) {
      return;
    }
    const clampToCurrentBounds = () => {
      const nextBounds = sidebarResizeBounds(sidebarRef.current);
      setResizeBounds(nextBounds);
      resizeSidebar(clampSidebarWidth(sidebarWidthPx, nextBounds.maxWidthPx));
    };
    clampToCurrentBounds();
    const shellElement = sidebarRef.current?.closest('[data-testid="app-shell-content"]') ?? null;
    const resizeObserver =
      typeof ResizeObserver === "undefined" || shellElement === null
        ? null
        : new ResizeObserver(clampToCurrentBounds);
    if (resizeObserver !== null && shellElement !== null) {
      resizeObserver.observe(shellElement);
    }
    window.addEventListener("resize", clampToCurrentBounds);
    return () => {
      resizeObserver?.disconnect();
      window.removeEventListener("resize", clampToCurrentBounds);
    };
  }, [activeDestination, resizeSidebar, sidebarWidthPx]);

  if (activeDestination === null) {
    return null;
  }

  const title = sidebarTitle(activeDestination, t);
  const mode = activeDestination.mode ?? "shift";

  return (
    <aside
      aria-labelledby={titleId}
      className={cx(
        "app-region-no-drag app-sidebar-panel island-glass z-10 grid grid-rows-[auto_1fr] overflow-hidden",
        "w-[var(--app-sidebar-width)] min-w-[var(--app-sidebar-width)] rounded-l-[var(--radius-xl)] rounded-r-[var(--radius-l)]",
        mode === "shift" &&
          "app-sidebar-panel-shift relative mr-[var(--app-sidebar-inset)] mt-[var(--app-sidebar-inset)] h-[calc(100%-(var(--app-sidebar-inset)*2))] shrink-0 self-start",
        mode === "overlay" &&
          "app-sidebar-panel-overlay fixed top-[calc(var(--native-titlebar-height)+var(--app-sidebar-inset))] right-[var(--app-sidebar-inset)] bottom-[var(--app-sidebar-inset)]",
        phase === "closing" && "app-sidebar-panel-closing",
      )}
      data-testid="app-sidebar-host"
      data-mode={mode}
      data-state={phase}
      ref={sidebarRef}
      role="complementary"
      style={sidebarStyle}
    >
      <div
        aria-label={t("app.resizeSidebar")}
        aria-orientation="vertical"
        aria-valuemax={resizeBounds.maxWidthPx}
        aria-valuemin={sidebarMinWidthPx}
        aria-valuenow={sidebarWidthPx}
        className={cx(
          "absolute top-0 bottom-0 left-0 z-20 w-3 cursor-ew-resize touch-none",
          "after:absolute after:top-[var(--space-4)] after:bottom-[var(--space-4)] after:left-1/2 after:w-px after:-translate-x-1/2 after:rounded-full after:bg-transparent after:transition-colors",
          "hover:after:bg-[var(--color-primary)] focus-visible:outline-none focus-visible:after:bg-[var(--color-primary)]",
          resizing && "after:bg-[var(--color-primary)]",
        )}
        data-testid="app-sidebar-resize-handle"
        onKeyDown={resizeWithKeyboard}
        onPointerCancel={stopResize}
        onPointerDown={startResize}
        onPointerMove={resizeFromPointer}
        onPointerUp={stopResize}
        role="separator"
        tabIndex={0}
      />
      <header className="grid grid-cols-[auto_auto_minmax(0,1fr)] items-center gap-[var(--space-3)] border-b border-[var(--color-outline)] px-[var(--space-4)] py-[var(--space-3)]">
        <Button
          aria-label={t("app.close")}
          onClick={() => {
            closeSidebar("closed");
          }}
          size="icon"
          variant="ghost"
        >
          <X aria-hidden="true" size={18} strokeWidth={1.5} />
        </Button>
        <h2 className="m-0 whitespace-nowrap text-[1.05rem] font-bold" id={titleId}>
          {title}
        </h2>
        <SidebarHeaderAccessory destination={activeDestination} />
      </header>
      <div className="min-h-0 overflow-y-auto px-[var(--space-4)] py-[var(--space-4)]">
        <SidebarDestinationView destination={activeDestination} resolveSidebar={resolveSidebar} />
      </div>
    </aside>
  );
}

function SidebarHeaderAccessory({ destination }: Readonly<{ destination: SidebarDestination }>) {
  if (destination.kind === "projectEdit") {
    return <ProjectDeleteButton projectID={destination.projectID} />;
  }
  if (destination.kind === "workflowInspect") {
    if (destination.selection.kind === "workflow") {
      return <WorkflowDeleteButton workflowID={destination.workflowID} />;
    }
    if (destination.selection.kind === "node") {
      return <WorkflowEntityIDHeader entityID={destination.selection.nodeID} entityKind="node" />;
    }
    if (destination.selection.kind === "edge") {
      return <WorkflowEntityIDHeader entityID={destination.selection.edgeID} entityKind="edge" />;
    }
  }
  return null;
}

function WorkflowEntityIDHeader({
  entityID,
  entityKind,
}: Readonly<{ entityID: string; entityKind: "edge" | "node" }>) {
  const { t } = useTranslation();
  const { nativeBridge } = useAppServices();
  const copyLabel =
    entityKind === "node"
      ? t("workflowEditor.copyNodeId", { id: entityID })
      : t("workflowEditor.copyEdgeId", { id: entityID });
  const successMessage =
    entityKind === "node" ? t("workflowEditor.nodeIdCopied") : t("workflowEditor.edgeIdCopied");
  const failureMessage =
    entityKind === "node" ? t("workflowEditor.nodeIdCopyFailed") : t("workflowEditor.edgeIdCopyFailed");
  const toastPrefix = entityKind === "node" ? "workflow-node-id" : "workflow-edge-id";
  return (
    <button
      aria-label={copyLabel}
      className="grid min-w-0 grid-cols-[minmax(0,1fr)] justify-items-end rounded-[var(--radius-s)] border border-transparent bg-transparent px-[var(--space-1)] py-[2px] font-mono text-xs text-[var(--color-muted)] outline-none hover:border-[var(--color-outline)] hover:bg-[var(--color-island-1)] focus-visible:border-[var(--color-primary)]"
      onClick={() => {
        void copyWorkflowEntityID(entityID, nativeBridge)
          .then(() => {
            showStatusToast({
              id: `${toastPrefix}-copied-${entityID}`,
              title: successMessage,
              tone: "success",
            });
          })
          .catch(() => {
            showStatusToast({
              id: `${toastPrefix}-copy-failed-${entityID}`,
              title: failureMessage,
              tone: "danger",
            });
          });
      }}
      title={entityID}
      type="button"
    >
      <span className="block max-w-full overflow-hidden text-ellipsis whitespace-nowrap text-right">
        {entityID}
      </span>
    </button>
  );
}

async function copyWorkflowEntityID(
  value: string,
  nativeBridge: ReturnType<typeof useAppServices>["nativeBridge"],
): Promise<void> {
  if (nativeBridge.capabilities.clipboard.writeText) {
    await nativeBridge.clipboard.writeText(value);
    return;
  }
  await navigator.clipboard.writeText(value);
}

type SidebarStyle = CSSProperties & Readonly<Record<"--app-sidebar-inset" | "--app-sidebar-width", string>>;

type PointerCaptureTarget = Partial<
  Readonly<{
    releasePointerCapture(pointerID: number): void;
    setPointerCapture(pointerID: number): void;
  }>
>;

type SidebarResizeDrag = Readonly<{
  bounds: SidebarResizeBounds;
  pointerID: number;
  startWidth: number;
  startX: number;
}>;

function sidebarResizeBounds(sidebarElement: HTMLElement | null): SidebarResizeBounds {
  const shellWidth = sidebarElement
    ?.closest('[data-testid="app-shell-content"]')
    ?.getBoundingClientRect().width;
  if (shellWidth === undefined || shellWidth === 0) {
    return sidebarResizeBoundsForShellWidth(fallbackSidebarShellWidth());
  }
  return sidebarResizeBoundsForShellWidth(shellWidth);
}

function fallbackSidebarShellWidth(): number {
  if (typeof window === "undefined") {
    return Math.ceil(sidebarMinWidthPx / sidebarMaxWidthRatio);
  }
  return window.innerWidth;
}

function setPointerCaptureIfAvailable(element: PointerCaptureTarget, pointerID: number): void {
  element.setPointerCapture?.(pointerID);
}

function releasePointerCaptureIfAvailable(element: PointerCaptureTarget, pointerID: number): void {
  element.releasePointerCapture?.(pointerID);
}
