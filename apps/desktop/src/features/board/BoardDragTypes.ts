export const boardDragTypeCanStart = "application/x-builder-board-can-start";

export function boardDragTypeManualTarget(nodeID: string): string {
    return `application/x-builder-board-manual-target-${nodeID}`;
}

export type BoardCardDragPayload = Readonly<{
    taskID: string;
    canStart: boolean;
    manualMoveTargetNodeIDs: readonly string[];
}>;

export type BoardColumnDropState = "idle" | "allowed" | "blocked";
