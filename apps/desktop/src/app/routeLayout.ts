const edgeToEdgeRoutePatterns = new Set(["/projects/$projectId", "/workflows/$workflowId/editor"]);

export function routeUsesEdgeToEdgeLayout(pathname: string): boolean {
  return edgeToEdgeRoutePatterns.has(routePattern(pathname));
}

export function routeFramePaddingClassName(pathname: string): string | undefined {
  if (routeUsesEdgeToEdgeLayout(pathname)) {
    return undefined;
  }
  if (routePattern(pathname) === "/workflows") {
    return "px-[var(--space-2)] pb-[var(--space-2)]";
  }
  return "p-[var(--space-2)]";
}

function routePattern(pathname: string): string {
  const segments = pathname.split("/").filter((segment) => segment.length > 0);
  if (segments.length === 2 && segments[0] === "projects") {
    return "/projects/$projectId";
  }
  if (segments.length === 3 && segments[0] === "workflows" && segments[2] === "editor") {
    return "/workflows/$workflowId/editor";
  }
  return pathname;
}
