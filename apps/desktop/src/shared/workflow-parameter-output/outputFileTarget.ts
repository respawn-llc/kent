export type WorkflowOutputRoot =
  Readonly<{ kind: "managed" | "source"; path: string }> | Readonly<{ kind: "unavailable" }>;

export function outputFileTarget(path: string, root: WorkflowOutputRoot) {
  if (path.startsWith("/")) return { kind: "resolvable", target: { path } } as const;
  if (root.kind === "unavailable") return { kind: "unavailable" } as const;
  return { kind: "resolvable", target: { path, basePath: root.path } } as const;
}
