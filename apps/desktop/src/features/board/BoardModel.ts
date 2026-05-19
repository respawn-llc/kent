import type { BoardColumn, BoardGroup, WorkflowBoard } from "../../api";

export type BoardSection = Readonly<
  | { kind: "column"; id: string; column: BoardColumn }
  | { kind: "group"; id: string; group: BoardGroup; columns: readonly BoardColumn[] }
>;

export function boardSections(board: WorkflowBoard): readonly BoardSection[] {
  const columnsByID = new Map(board.columns.map((column) => [column.id, column]));
  const consumed = new Set<string>();
  const backlog = board.columns.filter((column) => column.isBacklog);
  const done = board.columns.filter((column) => column.isDone);
  const groups = [...board.groups].sort((left, right) => left.sortOrder - right.sortOrder);
  const sections: BoardSection[] = [];

  for (const column of backlog) {
    consumed.add(column.id);
    sections.push({ kind: "column", id: column.id, column });
  }

  for (const group of groups) {
    const groupedColumns = group.nodeIDs
      .map((nodeID) => columnsByID.get(nodeID))
      .filter((column): column is BoardColumn => column !== undefined && !column.isBacklog && !column.isDone);
    const fallbackColumns = board.columns.filter(
      (column) => column.groupID === group.id && !column.isBacklog && !column.isDone,
    );
    const columns = groupedColumns.length > 0 ? groupedColumns : fallbackColumns;
    for (const column of columns) {
      consumed.add(column.id);
    }
    if (columns.length > 0) {
      sections.push({ kind: "group", id: group.id, group, columns });
    }
  }

  for (const column of board.columns) {
    if (!column.isBacklog && !column.isDone && !consumed.has(column.id)) {
      sections.push({ kind: "column", id: column.id, column });
    }
  }

  for (const column of done) {
    consumed.add(column.id);
    sections.push({ kind: "column", id: column.id, column });
  }

  return sections;
}
