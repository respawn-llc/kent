import type { DragEvent, KeyboardEvent, ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { formatRelativeTime } from "../../app/formatters";
import { Badge, Button, Spinner } from "../../ui";
import {
  type BoardCardDragPayload,
  type BoardColumnDropState,
  boardCardDragPayloadType,
  encodeBoardCardDragPayload,
} from "./BoardDragTypes";
import type { KanbanCardVM, KanbanColumnVM, KanbanGroupVM } from "./BoardColumnViewModel";

export type KanbanColumnProps = Readonly<{
  cards: readonly KanbanCardVM[];
  column: KanbanColumnVM;
  hasMoreCards: boolean;
  isLoadingMoreCards: boolean;
  isFirstActive: boolean;
  dropState: BoardColumnDropState;
  actionsDisabled: boolean;
  columnRef?: (element: HTMLElement | null) => void;
  onCardClick: (taskID: string) => void;
  onCardDragEnd: () => void;
  onCardDragStart: (payload: BoardCardDragPayload) => void;
  onDropTask: (event: DragEvent<HTMLElement>) => void;
  onInterruptTask: (taskID: string, runID: string) => void;
  onLoadMoreCards: () => void;
  onResumeTask: (taskID: string, runID: string) => void;
}>;

export function KanbanGroup({
  group,
  children,
}: Readonly<{
  group: KanbanGroupVM;
  children: ReactNode;
}>) {
  return (
    <section
      className="inline-grid h-full min-h-0 w-max grid-rows-[auto_minmax(0,1fr)] gap-[var(--space-3)] align-top"
      role="listitem"
    >
      <header>
        <p className="m-0 text-[0.72rem] font-extrabold uppercase tracking-[0.16em] text-[var(--color-muted)]">
          {group.key}
        </p>
        <h2 className="m-0 text-[1rem]">{group.name}</h2>
      </header>
      <div className="grid h-full min-h-0 grid-flow-col auto-cols-[min(480px,80vw)] gap-[var(--space-3)]">
        {children}
      </div>
    </section>
  );
}

export function KanbanColumn({
  cards,
  column,
  hasMoreCards,
  isLoadingMoreCards,
  isFirstActive,
  dropState,
  actionsDisabled,
  columnRef,
  onCardClick,
  onCardDragEnd,
  onCardDragStart,
  onDropTask,
  onInterruptTask,
  onLoadMoreCards,
  onResumeTask,
}: KanbanColumnProps) {
  const { t } = useTranslation();
  return (
    <section
      aria-label={column.name}
      className={`island-glass board-column-drop-${dropState} grid h-full min-h-0 w-[min(480px,80vw)] shrink-0 grid-rows-[auto_auto_auto_minmax(0,1fr)] gap-[var(--space-3)] rounded-[var(--radius-xl)] p-[var(--space-3)] align-top`}
      data-drop-state={dropState}
      ref={columnRef}
      onDragOver={(event) => {
        if (dropState === "idle") {
          return;
        }
        event.preventDefault();
        event.dataTransfer.dropEffect = dropState === "allowed" ? "move" : "none";
      }}
      onDrop={(event) => {
        onDropTask(event);
      }}
      role="listitem"
    >
      <header className="flex items-center justify-between gap-[var(--space-2)]">
        <div>
          <h2 className="m-0 text-[1rem]">{column.name}</h2>
          {column.assigneeRole.length > 0 ? (
            <p className="m-0 font-mono text-sm text-[var(--color-muted)]">{column.assigneeRole}</p>
          ) : null}
        </div>
        <Badge tone={isFirstActive ? "info" : "neutral"}>
          {t("board.taskCount", { count: column.taskCount })}
        </Badge>
      </header>
      {isFirstActive ? (
        <p className="m-0 rounded-[var(--radius-m)] border border-dashed border-[var(--color-outline)] p-[var(--space-2)] text-sm text-[var(--color-muted)]">
          {t("board.dropToStart")}
        </p>
      ) : null}
      <div
        className="min-h-0 overflow-y-auto pr-[var(--space-1)] hide-scrollbar"
        data-testid={`kanban-column-scroll-${column.id}`}
        onScroll={(event) => {
          if (!hasMoreCards || isLoadingMoreCards || !isNearScrollEnd(event.currentTarget)) {
            return;
          }
          onLoadMoreCards();
        }}
      >
        {cards.map((card) => (
          <TaskCard
            card={card}
            actionsDisabled={actionsDisabled}
            key={card.id}
            onClick={() => {
              onCardClick(card.id);
            }}
            onDragEnd={onCardDragEnd}
            onDragStart={onCardDragStart}
            onInterrupt={(runID) => {
              onInterruptTask(card.id, runID);
            }}
            onResume={(runID) => {
              onResumeTask(card.id, runID);
            }}
          />
        ))}
        {isLoadingMoreCards ? (
          <div
            aria-label={t("app.loadingMore")}
            className="grid place-items-center py-[var(--space-3)]"
            role="status"
          >
            <Spinner size="sm" />
            <span className="sr-only">{t("app.loadingMore")}</span>
          </div>
        ) : null}
      </div>
    </section>
  );
}

function isNearScrollEnd(element: HTMLElement): boolean {
  const remaining = element.scrollHeight - element.scrollTop - element.clientHeight;
  return remaining <= 96;
}

function TaskCard({
  actionsDisabled,
  card,
  onClick,
  onDragEnd,
  onDragStart,
  onInterrupt,
  onResume,
}: Readonly<{
  card: KanbanCardVM;
  actionsDisabled: boolean;
  onClick: () => void;
  onDragEnd: () => void;
  onDragStart: (payload: BoardCardDragPayload) => void;
  onInterrupt: (runID: string) => void;
  onResume: (runID: string) => void;
}>) {
  const { t } = useTranslation();
  const canDrag =
    !actionsDisabled && (card.actions.canStart || card.actions.manualMoveTargetNodeIDs.length > 0);
  return (
    <article
      aria-label={card.title}
      className="mb-[var(--space-3)] grid cursor-pointer gap-[var(--space-2)] rounded-[var(--radius-l)] border border-[var(--color-outline)] bg-[var(--color-island-1)] p-[var(--space-3)] outline-none focus-visible:border-[var(--color-primary)] focus-visible:shadow-[0_0_0_3px_color-mix(in_srgb,var(--color-primary)_26%,transparent)]"
      draggable={canDrag}
      onClick={onClick}
      onDragEnd={onDragEnd}
      onDragStart={(event) => {
        if (!canDrag) {
          event.preventDefault();
          return;
        }
        const payload = {
          taskID: card.id,
          canStart: card.actions.canStart,
          manualMoveTargetNodeIDs: card.actions.manualMoveTargetNodeIDs,
        };
        event.dataTransfer.setData("text/task-id", card.id);
        event.dataTransfer.setData(boardCardDragPayloadType, encodeBoardCardDragPayload(payload));
        event.dataTransfer.effectAllowed = "move";
        onDragStart(payload);
      }}
      onKeyDown={(event) => {
        activateCardFromKeyboard(event, onClick);
      }}
      tabIndex={0}
    >
      <div className="grid gap-[var(--space-1)] text-left text-[var(--color-on-island)]">
        <span className="flex min-w-0 items-center justify-between gap-[var(--space-2)]">
          <span className="shrink-0 font-mono text-[0.78rem] text-[var(--color-muted)]">{card.shortID}</span>
          <span className="min-w-0 truncate text-right text-sm text-[var(--color-muted)]">
            {formatRelativeTime(card.updatedAt)}
          </span>
        </span>
        <strong>{card.title}</strong>
        <span className="line-clamp-3 text-sm text-[var(--color-muted)]">{card.bodyPreview}</span>
      </div>
      <div className="flex items-start justify-between gap-[var(--space-2)]" data-testid="task-card-footer">
        <div
          className="task-card-chip-row flex min-w-0 flex-1 flex-wrap items-center gap-[var(--space-2)] text-sm text-[var(--color-muted)]"
          data-testid="task-card-chips"
        >
          <span className="task-card-chip-slot inline-flex items-center" data-testid="task-card-chip-slot">
            <Badge tone="neutral">{card.sourceWorkspaceName || t("board.workspace")}</Badge>
          </span>
        </div>
        <TaskCardActions
          actionsDisabled={actionsDisabled}
          card={card}
          onInterrupt={onInterrupt}
          onResume={onResume}
        />
      </div>
    </article>
  );
}

function activateCardFromKeyboard(event: KeyboardEvent<HTMLElement>, onClick: () => void): void {
  if (event.defaultPrevented) {
    return;
  }
  if (isInteractiveEventTarget(event.target)) {
    return;
  }
  if (event.key !== "Enter" && event.key !== " ") {
    return;
  }
  event.preventDefault();
  onClick();
}

function isInteractiveEventTarget(target: EventTarget): boolean {
  if (!(target instanceof Element)) {
    return false;
  }
  return target.closest("button,a,input,select,textarea,[role='button']") !== null;
}

function TaskCardActions({
  card,
  actionsDisabled,
  onInterrupt,
  onResume,
}: Readonly<{
  card: KanbanCardVM;
  actionsDisabled: boolean;
  onInterrupt: (runID: string) => void;
  onResume: (runID: string) => void;
}>) {
  const { t } = useTranslation();
  if (!card.actions.canInterrupt && !card.actions.canResume) {
    return null;
  }
  return (
    <div className="flex shrink-0 flex-wrap justify-end gap-[var(--space-2)]">
      {card.actions.canResume ? (
        <Button
          onClick={(event) => {
            event.stopPropagation();
            onResume(card.actions.resumeRunID);
          }}
          disabled={actionsDisabled}
          variant="primary"
        >
          {t("board.resume")}
        </Button>
      ) : null}
      {card.actions.canInterrupt ? (
        <Button
          onClick={(event) => {
            event.stopPropagation();
            onInterrupt(card.actions.interruptRunID);
          }}
          disabled={actionsDisabled}
          variant="danger"
        >
          {t("board.interrupt")}
        </Button>
      ) : null}
    </div>
  );
}
