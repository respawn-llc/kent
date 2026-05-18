import { Outlet, useLocation } from "@tanstack/react-router";

export function RouteTransitionFrame() {
  const location = useLocation();
  const transitionKey = routeTransitionKey(location.pathname, location.searchStr);
  return (
    <div
      className="route-transition-frame h-full min-h-0"
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
