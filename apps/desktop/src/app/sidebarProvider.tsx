import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import {
  SidebarContext,
  type SidebarCanceledResult,
  type SidebarCancelReason,
  type SidebarDestination,
  type SidebarPhase,
  type SidebarResult,
} from "./sidebarContext";
import { clampSidebarWidth, initialSidebarWidthForViewport } from "./sidebarSizing";

const sidebarExitAnimationMs = 140;
type SidebarWidthProfile = "standard" | "workflowEditor";
type SidebarWidths = Partial<Readonly<Record<SidebarWidthProfile, number>>>;
const defaultSidebarWidthProfile: SidebarWidthProfile = "standard";

type PendingSidebar = Readonly<{
  resolve: (result: SidebarResult) => void;
}>;

export function SidebarProvider({ children }: Readonly<{ children: ReactNode }>) {
  const [activeDestination, setActiveDestination] = useState<SidebarDestination | null>(null);
  const [activeWidthProfile, setActiveWidthProfile] = useState<SidebarWidthProfile>(
    defaultSidebarWidthProfile,
  );
  const [phase, setPhase] = useState<SidebarPhase>("open");
  const [sidebarWidths, setSidebarWidths] = useState<SidebarWidths>(() => ({
    [defaultSidebarWidthProfile]: defaultSidebarWidth(),
  }));
  const sidebarWidthPx = sidebarWidths[activeWidthProfile] ?? defaultSidebarWidth();
  const activeDestinationRef = useRef<SidebarDestination | null>(activeDestination);
  const pendingRef = useRef<PendingSidebar | null>(null);
  const closeTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const clearCloseTimeout = useCallback(() => {
    if (closeTimeoutRef.current !== null) {
      clearTimeout(closeTimeoutRef.current);
      closeTimeoutRef.current = null;
    }
  }, []);

  const animateClosed = useCallback(() => {
    clearCloseTimeout();
    setPhase("closing");
    closeTimeoutRef.current = setTimeout(() => {
      closeTimeoutRef.current = null;
      setActiveDestination(null);
      setPhase("open");
    }, sidebarExitAnimationMs);
  }, [clearCloseTimeout]);

  const closeSidebar = useCallback(
    (reason: SidebarCancelReason = "closed") => {
      const pending = pendingRef.current;
      pendingRef.current = null;
      pending?.resolve({ status: "canceled", reason });
      if (activeDestinationRef.current !== null) {
        animateClosed();
      }
    },
    [animateClosed],
  );

  const openSidebar = useCallback(
    async (destination: SidebarDestination): Promise<SidebarResult> => {
      clearCloseTimeout();
      const nextProfile = sidebarWidthProfile(destination);
      setActiveWidthProfile(nextProfile);
      setSidebarWidths((current) => {
        if (current[nextProfile] !== undefined) {
          return current;
        }
        return {
          ...current,
          [nextProfile]: defaultSidebarWidth(),
        };
      });
      const pending = pendingRef.current;
      pendingRef.current = null;
      pending?.resolve({ status: "canceled", reason: "replaced" });
      setPhase("open");
      activeDestinationRef.current = destination;
      setActiveDestination(destination);
      return new Promise<SidebarResult>((resolve) => {
        pendingRef.current = { resolve };
      });
    },
    [clearCloseTimeout],
  );

  const resolveSidebar = useCallback(
    (result: Exclude<SidebarResult, SidebarCanceledResult>) => {
      const pending = pendingRef.current;
      pendingRef.current = null;
      pending?.resolve(result);
      if (activeDestinationRef.current !== null) {
        animateClosed();
      }
    },
    [animateClosed],
  );

  const resizeSidebar = useCallback(
    (widthPx: number) => {
      setSidebarWidths((current) => ({
        ...current,
        [activeWidthProfile]: clampSidebarWidth(widthPx),
      }));
    },
    [activeWidthProfile],
  );

  useEffect(() => {
    return clearCloseTimeout;
  }, [clearCloseTimeout]);

  useEffect(() => {
    activeDestinationRef.current = activeDestination;
  }, [activeDestination]);

  const value = useMemo(
    () => ({
      activeDestination,
      closeSidebar,
      openSidebar,
      phase,
      resizeSidebar,
      resolveSidebar,
      sidebarWidthPx,
    }),
    [activeDestination, closeSidebar, openSidebar, phase, resizeSidebar, resolveSidebar, sidebarWidthPx],
  );

  return <SidebarContext.Provider value={value}>{children}</SidebarContext.Provider>;
}

function sidebarWidthProfile(destination: SidebarDestination): SidebarWidthProfile {
  if (destination.kind === "workflowInspect" || destination.kind === "workflowEditor") {
    return "workflowEditor";
  }
  return defaultSidebarWidthProfile;
}

function defaultSidebarWidth(): number {
  if (typeof window === "undefined") {
    return initialSidebarWidthForViewport(0);
  }
  return initialSidebarWidthForViewport(window.innerWidth);
}
