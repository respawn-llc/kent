export const boardCardDragPayloadType = "application/x-builder-board-card+json";

export type BoardCardDragPayload = Readonly<{
  taskID: string;
  canStart: boolean;
  activeNodeIDs: readonly string[];
  statusKind: string;
  manualMoveTargetNodeIDs: readonly string[];
}>;

export type BoardColumnDropState = "idle" | "allowed" | "blocked";

export function encodeBoardCardDragPayload(payload: BoardCardDragPayload): string {
  return JSON.stringify({
    taskID: payload.taskID,
    canStart: payload.canStart,
    activeNodeIDs: [...payload.activeNodeIDs],
    statusKind: payload.statusKind,
    manualMoveTargetNodeIDs: [...payload.manualMoveTargetNodeIDs],
  });
}

export function decodeBoardCardDragPayload(serialized: string): BoardCardDragPayload | null {
  if (serialized.length === 0) {
    return null;
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(serialized);
  } catch {
    return null;
  }
  if (
    !isDragPayloadObject(parsed) ||
    !isStringArray(parsed.activeNodeIDs) ||
    !isStringArray(parsed.manualMoveTargetNodeIDs)
  ) {
    return null;
  }
  if (
    parsed.taskID.length === 0 ||
    parsed.statusKind.length === 0 ||
    parsed.activeNodeIDs.some((nodeID) => nodeID.length === 0) ||
    parsed.manualMoveTargetNodeIDs.some((nodeID) => nodeID.length === 0)
  ) {
    return null;
  }
  return {
    taskID: parsed.taskID,
    canStart: parsed.canStart,
    activeNodeIDs: parsed.activeNodeIDs,
    statusKind: parsed.statusKind,
    manualMoveTargetNodeIDs: parsed.manualMoveTargetNodeIDs,
  };
}

function isDragPayloadObject(value: unknown): value is Readonly<{
  taskID: string;
  canStart: boolean;
  activeNodeIDs: unknown;
  statusKind: string;
  manualMoveTargetNodeIDs: unknown;
}> {
  return (
    typeof value === "object" &&
    value !== null &&
    !Array.isArray(value) &&
    "taskID" in value &&
    "canStart" in value &&
    "activeNodeIDs" in value &&
    "statusKind" in value &&
    "manualMoveTargetNodeIDs" in value &&
    typeof value.taskID === "string" &&
    typeof value.canStart === "boolean" &&
    typeof value.statusKind === "string"
  );
}

function isStringArray(value: unknown): value is readonly string[] {
  return Array.isArray(value) && value.every((item) => typeof item === "string");
}
