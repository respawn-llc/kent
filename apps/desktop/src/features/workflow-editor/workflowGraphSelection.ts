export type WorkflowGraphSelection =
  | Readonly<{ kind: "node"; nodeID: string }>
  | Readonly<{ kind: "edge"; edgeID: string }>
  | Readonly<{ kind: "group"; groupID: string }>;
