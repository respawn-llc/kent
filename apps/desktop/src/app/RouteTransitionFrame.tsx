import { Outlet, useLocation } from "@tanstack/react-router";

import { cx } from "../ui/classes";

const edgeToEdgeRoutePatterns = new Set(["/projects/$projectId"]);

export function RouteTransitionFrame() {
  const location = useLocation();
  const transitionKey = routeTransitionKey(location.pathname, location.searchStr);
  return (
    <div
      className={cx(
        "route-transition-frame h-full min-h-0",
        routeUsesEdgeToEdgeLayout(location.pathname) ? undefined : "p-[var(--space-2)]",
      )}
      data-testid="route-transition-frame"
      key={transitionKey}
    >
      <Outlet />
    </div>
  );
}

function routeUsesEdgeToEdgeLayout(pathname: string): boolean {
  return edgeToEdgeRoutePatterns.has(routePattern(pathname));
}

function routePattern(pathname: string): string {
  const segments = pathname.split("/").filter((segment) => segment.length > 0);
  if (segments.length === 2 && segments[0] === "projects") {
    return "/projects/$projectId";
  }
  return pathname;
}

function routeTransitionKey(pathname: string, searchStr: string): string {
  if (pathname.startsWith("/projects/")) {
    const workflowID = new URLSearchParams(searchStr).get("workflowId") ?? "";
    return `${pathname}?workflowId=${workflowID}`;
  }
  return `${pathname}?${searchStr}`;
}
