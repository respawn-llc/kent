import { useCallback, useLayoutEffect, useState, type RefObject } from "react";

import type { BoardColumn } from "@/api";
import type { KanbanCardVM } from "./BoardColumnViewModel";
import { useBoardDragAutoScroll } from "./BoardDragAutoScroll";
import { classifyDrop } from "./BoardDropActions";
import type { BoardCardDragPayload } from "./BoardDragTypes";
import type { BoardColumnDropState } from "./BoardDragTypes";
import type { BoardCardInstance } from "./BoardCardInstance";
import type { PendingBoardCardMove } from "./BoardCardMotionModel";

export type ActiveBoardCardDrag = Readonly<{
  instance: BoardCardInstance;
  lastCardIndex: number;
  payload: BoardCardDragPayload;
  snapshot: KanbanCardVM;
}>;

export function classifyBoardColumnDropState({
  column,
  drag,
  dragBlocked,
  firstActiveID,
}: Readonly<{
  column: BoardColumn;
  drag: ActiveBoardCardDrag | null;
  dragBlocked: boolean;
  firstActiveID: string | undefined;
}>): BoardColumnDropState {
  if (drag === null) {
    return dragBlocked ? "blocked" : "idle";
  }
  return classifyDrop(column, drag.payload, firstActiveID).kind === "reject" ? "blocked" : "idle";
}

type BoardDragGesture =
  | Readonly<{ kind: "dragging"; drag: ActiveBoardCardDrag }>
  | Readonly<{ kind: "dropped"; move: PendingBoardCardMove }>;

export function useBoardDragLifecycle({
  disabled,
  rootRef,
  actionPending,
}: Readonly<{
  disabled: boolean;
  rootRef: RefObject<HTMLDivElement | null>;
  actionPending: boolean;
}>) {
  const [gesture, setGesture] = useState<BoardDragGesture | null>(null);
  const activeDrag = !disabled && gesture?.kind === "dragging" ? gesture.drag : null;
  const pendingCardMove = gesture?.kind === "dropped" && actionPending ? gesture.move : null;
  const autoScroll = useBoardDragAutoScroll({ active: activeDrag !== null, rootRef });
  const stopAutoScroll = autoScroll.stop;
  const cancel = useCallback(() => {
    stopAutoScroll();
    setGesture(null);
  }, [stopAutoScroll]);
  const start = useCallback(
    (drag: ActiveBoardCardDrag) => {
      if (!disabled) {
        setGesture({ kind: "dragging", drag });
      }
    },
    [disabled],
  );
  const drop = useCallback(
    (targetColumnID: string, dropRect: DOMRectReadOnly) => {
      stopAutoScroll();
      setGesture((current) =>
        current?.kind === "dragging"
          ? { kind: "dropped", move: { card: current.drag.snapshot, targetColumnID, dropRect } }
          : current,
      );
    },
    [stopAutoScroll],
  );
  useLayoutEffect(() => {
    if (gesture === null || (gesture.kind === "dragging" ? !disabled : actionPending)) return;
    stopAutoScroll();
    queueMicrotask(() => {
      setGesture((current) => (current === gesture ? null : current));
    });
  }, [actionPending, disabled, gesture, stopAutoScroll]);
  return {
    activeDrag,
    pendingCardMove,
    autoScroll,
    cancel,
    drop,
    dragBlocked: disabled && gesture?.kind === "dragging",
    start,
  };
}
