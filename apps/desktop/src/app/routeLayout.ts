const edgeToEdgeRoutePatterns = new Set([
  "/projects/$projectId",
  "/projects/$projectId/workflows/$workflowId/editor",
]);

export function routeUsesEdgeToEdgeLayout(pathname: string): boolean {
  return edgeToEdgeRoutePatterns.has(routePattern(pathname));
}

function routePattern(pathname: string): string {
  const segments = pathname.split("/").filter((segment) => segment.length > 0);
  if (segments.length === 2 && segments[0] === "projects") {
    return "/projects/$projectId";
  }
  if (
    segments.length === 5 &&
    segments[0] === "projects" &&
    segments[2] === "workflows" &&
    segments[4] === "editor"
  ) {
    return "/projects/$projectId/workflows/$workflowId/editor";
  }
  return pathname;
}
