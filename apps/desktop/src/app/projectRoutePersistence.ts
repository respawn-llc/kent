const lastProjectRouteStorageKey = "builder.desktop.lastProjectRoute";
type StoredProjectRoute = Readonly<{ projectId: string; workflowId: string }>;
export function readLastProjectRoute(): StoredProjectRoute | null {
  const raw = localStorageSafe()?.getItem(lastProjectRouteStorageKey) ?? "null";
  try {
    const parsed: unknown = JSON.parse(raw);
    return isStoredProjectRoute(parsed) ? parsed : null;
  } catch {
    return null;
  }
}
export function writeLastProjectRoute(route: StoredProjectRoute): void {
  localStorageSafe()?.setItem(lastProjectRouteStorageKey, JSON.stringify(route));
}
export function clearLastProjectRoute(projectID: string): void {
  const storage = localStorageSafe();
  if (storage !== null && readLastProjectRoute()?.projectId === projectID) {
    storage.removeItem(lastProjectRouteStorageKey);
  }
}
function isStoredProjectRoute(value: unknown): value is StoredProjectRoute {
  if (typeof value !== "object" || value === null || !("projectId" in value) || !("workflowId" in value)) {
    return false;
  }
  return typeof value.projectId === "string" && typeof value.workflowId === "string";
}

function localStorageSafe(): Storage | null {
  try {
    return globalThis.localStorage;
  } catch {
    return null;
  }
}
