import { createContext, useContext, type ReactNode } from "react";

export type SidebarMode = "overlay" | "shift";
export type SidebarPhase = "closing" | "open";

export type SidebarCancelReason = "closed" | "replaced" | "route_change";

export type SidebarCanceledResult = Readonly<{
  status: "canceled";
  reason: SidebarCancelReason;
}>;

export type SidebarNewTaskResult = Readonly<{
  status: "submitted";
  destination: "newTask";
}>;

export type SidebarTaskDetailResult = Readonly<{
  status: "closed";
  destination: "taskDetail";
}>;

export type SidebarResult = SidebarCanceledResult | SidebarNewTaskResult | SidebarTaskDetailResult;

export type SidebarDestination =
  | Readonly<{
      kind: "newTask";
      mode?: SidebarMode;
      boardQueryWorkflowID: string;
      projectID: string;
      workflowID: string;
    }>
  | Readonly<{
      kind: "taskDetail";
      mode?: SidebarMode;
      taskID: string;
      resumeRunID: string;
      onMutated?: (() => void) | undefined;
    }>
  | Readonly<{
      kind: "custom";
      mode?: SidebarMode;
      title: string;
      content: ReactNode;
    }>;

export type SidebarController = Readonly<{
  activeDestination: SidebarDestination | null;
  closeSidebar(reason?: SidebarCancelReason): void;
  openSidebar(destination: SidebarDestination): Promise<SidebarResult>;
  phase: SidebarPhase;
  resolveSidebar(result: Exclude<SidebarResult, SidebarCanceledResult>): void;
  resizeSidebar(widthPx: number): void;
  sidebarWidthPx: number;
}>;

export const SidebarContext = createContext<SidebarController | null>(null);

export function useSidebar(): SidebarController {
  const value = useContext(SidebarContext);
  if (value === null) {
    throw new Error("SidebarProvider is required");
  }
  return value;
}
