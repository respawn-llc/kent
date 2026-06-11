import type { BoardColumn, WorkflowBoard } from "../../api";
import type { KanbanCardVM } from "./BoardColumnViewModel";
import type { BoardSection } from "./BoardModel";

export type BoardCardColumnsSnapshot = ReadonlyMap<string, readonly KanbanCardVM[]>;
export type BoardCardColumnCountSnapshot = ReadonlyMap<string, number>;

export type BoardCardMotionParticipants = Readonly<{
  namesByCardID: ReadonlyMap<string, string>;
  revealCardIDs: ReadonlySet<string>;
}>;

export function boardCardViewTransitionName(taskID: string): string {
  const encoded = Array.from(taskID, (char) => {
    const codePoint = char.codePointAt(0);
    return codePoint === undefined ? "" : codePoint.toString(16).padStart(2, "0");
  }).join("-");
  return `board-card-${encoded.length > 0 ? encoded : "empty"}`;
}

export function boardCardSnapshotsEqual(
  left: BoardCardColumnsSnapshot,
  right: BoardCardColumnsSnapshot,
): boolean {
  if (left.size !== right.size) {
    return false;
  }
  for (const [columnID, leftCards] of left) {
    const rightCards = right.get(columnID);
    if (rightCards === undefined || !cardListsEqual(leftCards, rightCards)) {
      return false;
    }
  }
  return true;
}

export function boardCardSnapshotFromEntries(
  entries: Iterable<readonly [string, readonly KanbanCardVM[]]>,
): BoardCardColumnsSnapshot {
  return new Map(Array.from(entries, ([columnID, cards]) => [columnID, cards]));
}

export function boardCardMotionParticipants(
  oldSnapshot: BoardCardColumnsSnapshot,
  newSnapshot: BoardCardColumnsSnapshot,
  visibleOldCardIDs: ReadonlySet<string>,
): BoardCardMotionParticipants {
  const oldCounts = cardIDCounts(oldSnapshot);
  const newCounts = cardIDCounts(newSnapshot);
  const namesByCardID = new Map<string, string>();
  const revealCardIDs = new Set<string>();
  const cardIDs = new Set([...oldCounts.keys(), ...newCounts.keys()]);

  for (const cardID of cardIDs) {
    const oldCount = oldCounts.get(cardID) ?? 0;
    const newCount = newCounts.get(cardID) ?? 0;
    if (oldCount > 1 || newCount > 1) {
      continue;
    }
    if (oldCount === 1 && visibleOldCardIDs.has(cardID) && (newCount === 1 || newCount === 0)) {
      namesByCardID.set(cardID, boardCardViewTransitionName(cardID));
      continue;
    }
    if (oldCount === 0 && newCount === 1) {
      revealCardIDs.add(cardID);
    }
  }

  return { namesByCardID, revealCardIDs };
}

export function dirtyBoardCardColumnIDs(
  currentDisplayed: BoardCardColumnsSnapshot,
  nextDisplayed: BoardCardColumnsSnapshot,
): readonly string[] {
  const columnIDs = new Set([...currentDisplayed.keys(), ...nextDisplayed.keys()]);
  return Array.from(columnIDs).filter((columnID) => {
    const currentCards = currentDisplayed.get(columnID) ?? [];
    const nextCards = nextDisplayed.get(columnID) ?? [];
    return !boardCardSnapshotsEqual(new Map([[columnID, currentCards]]), new Map([[columnID, nextCards]]));
  });
}

export function dirtyBoardCardCountColumnIDs(
  currentCounts: BoardCardColumnCountSnapshot,
  nextCounts: BoardCardColumnCountSnapshot,
): readonly string[] {
  const columnIDs = new Set([...currentCounts.keys(), ...nextCounts.keys()]);
  return Array.from(columnIDs).filter((columnID) => (currentCounts.get(columnID) ?? 0) !== (nextCounts.get(columnID) ?? 0));
}

export function boardCardColumnCountSnapshot(
  board: Readonly<{ columns: readonly Pick<BoardColumn, "id" | "taskCount">[] }>,
): BoardCardColumnCountSnapshot {
  return new Map(board.columns.map((column) => [column.id, column.taskCount]));
}

export function boardCardColumnIDsWithCards(snapshot: BoardCardColumnsSnapshot): ReadonlySet<string> {
  return new Set(Array.from(snapshot, ([columnID, cards]) => (cards.length > 0 ? columnID : "")).filter(Boolean));
}

export function boardRailLayoutSignature(
  board: WorkflowBoard,
  sections: readonly BoardSection[],
  firstActiveID: string | undefined,
): string {
  const sectionSignature = sections
    .map((section) =>
      section.kind === "group"
        ? `group:${section.group.id}:${section.columns.map((column) => column.id).join(",")}`
        : `column:${section.column.id}`,
    )
    .join("|");
  return `${board.projectID}:${board.selectedWorkflow.id}:${firstActiveID ?? ""}:${sectionSignature}`;
}

export function cardBelongsToColumn(column: BoardColumn, card: KanbanCardVM): boolean {
  if (column.isBacklog) {
    return card.statusKind === "backlog";
  }
  if (column.isDone) {
    return card.statusKind === "done" || card.statusKind === "canceled" || card.activeNodeIDs.includes(column.id);
  }
  return card.activeNodeIDs.includes(column.id);
}

function cardListsEqual(left: readonly KanbanCardVM[], right: readonly KanbanCardVM[]): boolean {
  if (left.length !== right.length) {
    return false;
  }
  return left.every((card, index) => cardsEqual(card, right[index]));
}

function cardsEqual(left: KanbanCardVM, right: KanbanCardVM | undefined): boolean {
  return (
    right?.id === left.id &&
    cardContentEqual(left, right) &&
    cardActionsEqual(left, right)
  );
}

function cardContentEqual(left: KanbanCardVM, right: KanbanCardVM): boolean {
  return (
    left.shortID === right.shortID &&
    left.title === right.title &&
    left.bodyPreview === right.bodyPreview &&
    left.updatedAt === right.updatedAt &&
    left.statusKind === right.statusKind &&
    arrayEqual(left.statusRunIDs, right.statusRunIDs) &&
    left.sourceWorkspaceName === right.sourceWorkspaceName &&
    arrayEqual(left.activeNodeIDs, right.activeNodeIDs)
  );
}

function cardActionsEqual(left: KanbanCardVM, right: KanbanCardVM): boolean {
  return (
    left.actions.canInterrupt === right.actions.canInterrupt &&
    left.actions.canResume === right.actions.canResume &&
    left.actions.canStart === right.actions.canStart &&
    left.actions.interruptRunID === right.actions.interruptRunID &&
    left.actions.resumeRunID === right.actions.resumeRunID &&
    arrayEqual(left.actions.manualMoveTargetNodeIDs, right.actions.manualMoveTargetNodeIDs)
  );
}

function arrayEqual(left: readonly string[], right: readonly string[]): boolean {
  return left.length === right.length && left.every((value, index) => value === right[index]);
}

function cardIDCounts(snapshot: BoardCardColumnsSnapshot): ReadonlyMap<string, number> {
  const counts = new Map<string, number>();
  for (const cards of snapshot.values()) {
    for (const card of cards) {
      counts.set(card.id, (counts.get(card.id) ?? 0) + 1);
    }
  }
  return counts;
}
