export type BoardCardDragPayload = Readonly<{
  taskID: string;
  canStart: boolean;
  activeNodeIDs: readonly string[];
  statusKind: string;
}>;

export type BoardColumnDropState = "idle" | "blocked";
