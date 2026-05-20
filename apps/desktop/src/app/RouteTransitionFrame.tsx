import { Outlet, useLocation } from "@tanstack/react-router";

import { cx } from "../ui/classes";
import { routeUsesEdgeToEdgeLayout } from "./routeLayout";

export function RouteTransitionFrame() {
  const location = useLocation();
  const transitionKey = routeTransitionKey(location.pathname, location.searchStr);
  return (
    <div
      className={cx(
        "route-transition-frame h-full min-h-0 min-w-0 w-full",
        routeUsesEdgeToEdgeLayout(location.pathname) ? undefined : "p-[var(--space-2)]",
      )}
      data-testid="route-transition-frame"
      key={transitionKey}
    >
      <Outlet />
    </div>
  );
}

function routeTransitionKey(pathname: string, searchStr: string): string {
  if (pathname.startsWith("/projects/")) {
    const workflowID = new URLSearchParams(searchStr).get("workflowId") ?? "";
    return `${pathname}?workflowId=${workflowID}`;
  }
  return `${pathname}?${searchStr}`;
}
