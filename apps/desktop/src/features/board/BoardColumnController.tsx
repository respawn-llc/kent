import { useMemo, useState, type DragEvent, type RefObject } from "react";

import type { BoardColumn, WorkflowBoard } from "../../api";
import { KanbanColumn } from "./BoardColumns";
import { toKanbanCardVM, toKanbanColumnVM } from "./BoardColumnViewModel";
import type { BoardCardDragPayload, BoardColumnDropState } from "./BoardDragTypes";
import { useBoardNodeCards } from "./useBoardData";
import { useColumnVisibility } from "./useColumnVisibility";

export type BoardColumnControllerProps = Readonly<{
  actionsDisabled: boolean;
  board: WorkflowBoard;
  column: BoardColumn;
  dropState: BoardColumnDropState;
  isFirstActive: boolean;
  onCardClick: (taskID: string) => void;
  onCardDragEnd: () => void;
  onCardDragStart: (payload: BoardCardDragPayload) => void;
  onDropTask: (event: DragEvent<HTMLElement>, column: BoardColumn) => void;
  onInterruptTask: (taskID: string, runID: string) => void;
  onResumeTask: (taskID: string, runID: string) => void;
  scrollportRef: RefObject<HTMLElement | null>;
}>;

export function BoardColumnController({
  actionsDisabled,
  board,
  column,
  dropState,
  isFirstActive,
  onCardClick,
  onCardDragEnd,
  onCardDragStart,
  onDropTask,
  onInterruptTask,
  onResumeTask,
  scrollportRef,
}: BoardColumnControllerProps) {
  const [columnElement, setColumnElement] = useState<HTMLElement | null>(null);
  const isVisible = useColumnVisibility(scrollportRef, columnElement);
  const queryEnabled = isVisible;
  const cardsQuery = useBoardNodeCards(board.projectID, board.selectedWorkflow.id, column.id, queryEnabled);
  const cards = useMemo(
    () => cardsQuery.data?.pages.flatMap((page) => page.cards) ?? [],
    [cardsQuery.data?.pages],
  );
  const columnVM = useMemo(() => toKanbanColumnVM(column), [column]);
  const cardVMs = useMemo(() => cards.map(toKanbanCardVM), [cards]);

  return (
    <KanbanColumn
      actionsDisabled={actionsDisabled}
      cards={cardVMs}
      column={columnVM}
      columnRef={setColumnElement}
      dropState={dropState}
      hasMoreCards={cardsQuery.hasNextPage}
      isFirstActive={isFirstActive}
      isLoadingMoreCards={(queryEnabled && cardsQuery.isPending) || cardsQuery.isFetchingNextPage}
      onCardClick={onCardClick}
      onCardDragEnd={onCardDragEnd}
      onCardDragStart={onCardDragStart}
      onDropTask={(event) => {
        onDropTask(event, column);
      }}
      onInterruptTask={onInterruptTask}
      onLoadMoreCards={() => {
        if (cardsQuery.hasNextPage && !cardsQuery.isFetchingNextPage) {
          void cardsQuery.fetchNextPage();
        }
      }}
      onResumeTask={onResumeTask}
    />
  );
}
