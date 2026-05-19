export const boardCardDragPayloadType = "application/x-builder-board-card+json";

export type BoardCardDragPayload = Readonly<{
  taskID: string;
  canStart: boolean;
  manualMoveTargetNodeIDs: readonly string[];
}>;

export type BoardColumnDropState = "idle" | "allowed" | "blocked";

export function encodeBoardCardDragPayload(payload: BoardCardDragPayload): string {
  return JSON.stringify({
    taskID: payload.taskID,
    canStart: payload.canStart,
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
  if (!isDragPayloadObject(parsed) || !isStringArray(parsed.manualMoveTargetNodeIDs)) {
    return null;
  }
  if (parsed.taskID.length === 0 || parsed.manualMoveTargetNodeIDs.some((nodeID) => nodeID.length === 0)) {
    return null;
  }
  return {
    taskID: parsed.taskID,
    canStart: parsed.canStart,
    manualMoveTargetNodeIDs: parsed.manualMoveTargetNodeIDs,
  };
}

function isDragPayloadObject(
  value: unknown,
): value is Readonly<{ taskID: string; canStart: boolean; manualMoveTargetNodeIDs: unknown }> {
  return (
    typeof value === "object" &&
    value !== null &&
    !Array.isArray(value) &&
    "taskID" in value &&
    "canStart" in value &&
    "manualMoveTargetNodeIDs" in value &&
    typeof value.taskID === "string" &&
    typeof value.canStart === "boolean"
  );
}

function isStringArray(value: unknown): value is readonly string[] {
  return Array.isArray(value) && value.every((item) => typeof item === "string");
}
