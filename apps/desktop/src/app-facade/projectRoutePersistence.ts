import { z } from "zod";

import { workflowIDSchema } from "@/api";
import {
  appStorageKey,
  readBrowserStorage,
  removeBrowserStorage,
  writeBrowserStorage,
} from "./browserStorage";
import type { AppStorageNamespace } from "./services";
import type { DesktopSettings } from "./desktopSettings";

const lastProjectRouteStorageKey = "desktop.lastProjectRoute";
export const projectContentTabs = ["tasks", "sessions", "subagents"] as const;
export type ProjectContentTab = (typeof projectContentTabs)[number];
const projectContentTabSchema = z.enum(projectContentTabs);
const storedProjectRouteSchema = z.discriminatedUnion("kind", [
  z.object({
    kind: z.literal("home_project"),
    projectId: z.string(),
  }),
  z.object({
    kind: z.literal("workflow_board"),
    projectId: z.string(),
    workflowId: workflowIDSchema.optional(),
  }),
]);
type StoredProjectRoute = z.output<typeof storedProjectRouteSchema>;

export function readLastProjectRoute(namespace: AppStorageNamespace | null): StoredProjectRoute | null {
  if (namespace === null) return null;
  const stored = readBrowserStorage("local", appStorageKey(namespace, lastProjectRouteStorageKey));
  if (!stored.ok) {
    return null;
  }
  const raw = stored.value ?? "null";
  try {
    const parsed: unknown = JSON.parse(raw);
    const route = storedProjectRouteSchema.safeParse(parsed);
    return route.success ? route.data : null;
  } catch {
    return null;
  }
}
export function writeLastProjectRoute(
  namespace: AppStorageNamespace | null,
  route: StoredProjectRoute,
): void {
  if (namespace === null) return;
  writeBrowserStorage("local", appStorageKey(namespace, lastProjectRouteStorageKey), JSON.stringify(route));
}

export function writeLastProjectContentTab(
  namespace: AppStorageNamespace | null,
  projectID: string,
  contentTab: ProjectContentTab,
): void {
  if (namespace === null) return;
  writeBrowserStorage("local", projectTabKey(namespace, projectID), contentTab);
}

export function projectOpeningTab(
  namespace: AppStorageNamespace | null,
  projectID: string,
  preference: DesktopSettings["projectOpening"],
  explicit: ProjectContentTab | null,
): ProjectContentTab {
  if (explicit !== null) return explicit;
  if (preference !== "lastUsed") return preference;
  if (namespace === null) return "tasks";
  const stored = readBrowserStorage("local", projectTabKey(namespace, projectID));
  const tab = projectContentTabSchema.safeParse(stored.ok ? stored.value : null);
  return tab.success ? tab.data : "tasks";
}

function projectTabKey(namespace: AppStorageNamespace, projectID: string): string {
  return appStorageKey(namespace, `desktop.projectTab:${encodeURIComponent(projectID)}`);
}

export function clearLastProjectRoute(namespace: AppStorageNamespace | null, projectID: string): void {
  if (namespace === null) return;
  removeBrowserStorage("local", projectTabKey(namespace, projectID));
  if (readLastProjectRoute(namespace)?.projectId === projectID) {
    removeBrowserStorage("local", appStorageKey(namespace, lastProjectRouteStorageKey));
  }
}
