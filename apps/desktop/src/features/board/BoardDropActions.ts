import type { BoardColumn, WorkflowOutputField } from "../../api";
import type { BoardCardDragPayload } from "./BoardDragTypes";

export type BoardDropAction =
  | Readonly<{ kind: "start" }>
  | Readonly<{ kind: "move"; allowMissingEdge?: boolean; autoApprove?: boolean }>
  | Readonly<{ kind: "confirmRollback" }>
  | Readonly<{ kind: "missingInput" }>;

export type PendingDrop = Readonly<{
  taskID: string;
  targetColumn: BoardColumn;
}>;

export type PendingMissingInputDrop = Readonly<{
  taskID: string;
  targetColumn: BoardColumn;
  fields: readonly WorkflowOutputField[];
  values: Readonly<Record<string, string>>;
}>;

export function classifyDrop(
  column: BoardColumn,
  dragPayload: BoardCardDragPayload,
  firstActiveColumnID: string | undefined,
): BoardDropAction {
  if (dragPayload.canStart && column.id === firstActiveColumnID) {
    return { kind: "start" };
  }
  if (dragPayload.manualMoveTargetNodeIDs.includes(column.id)) {
    return { kind: "move" };
  }
  if (column.isBacklog) {
    return { kind: "move", allowMissingEdge: true };
  }
  if (dragPayload.statusKind === "done" && column.kind === "agent") {
    return { kind: "confirmRollback" };
  }
  if (column.transitionOutputFields.length > 0) {
    return { kind: "missingInput" };
  }
  if (column.kind === "agent") {
    return { kind: "move", allowMissingEdge: true, autoApprove: true };
  }
  return { kind: "move", allowMissingEdge: true };
}

export function missingInputValues(fields: readonly WorkflowOutputField[]): Readonly<Record<string, string>> {
  return Object.fromEntries(fields.map((field) => [field.name, ""]));
}
