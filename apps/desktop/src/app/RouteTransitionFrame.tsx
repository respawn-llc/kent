import { Outlet, useLocation } from "@tanstack/react-router";

import { cx } from "../ui/classes";

export function RouteTransitionFrame() {
  const location = useLocation();
  const transitionKey = routeTransitionKey(location.pathname, location.searchStr);
  return (
    <div
      className={cx("route-transition-frame h-full min-h-0", routeUsesEdgeToEdgeBoard(location.pathname) ? undefined : "p-[var(--space-2)]")}
      data-testid="route-transition-frame"
      key={transitionKey}
    >
      <Outlet />
    </div>
  );
}

function routeUsesEdgeToEdgeBoard(pathname: string): boolean {
  const segments = pathname.split("/").filter((segment) => segment.length > 0);
  return segments.length === 2 && segments[0] === "projects";
}

function routeTransitionKey(pathname: string, searchStr: string): string {
  if (pathname.startsWith("/projects/")) {
    const workflowID = new URLSearchParams(searchStr).get("workflowId") ?? "";
    return `${pathname}?workflowId=${workflowID}`;
  }
  return `${pathname}?${searchStr}`;
}
