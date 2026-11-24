export type WorkflowOutputRoot =
  Readonly<{ kind: "managed" | "source"; path: string }> | Readonly<{ kind: "unavailable" }>;
