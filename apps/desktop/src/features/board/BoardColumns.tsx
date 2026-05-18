import type { DragEvent } from "react";
import { useTranslation } from "react-i18next";

import type { BoardCard, BoardColumn, BoardGroup, WorkflowBoard } from "../../api";
import { formatRelativeTime } from "../../app/formatters";
import { Badge, Button } from "../../ui";
import { cardsForColumn } from "./BoardModel";

export type KanbanColumnProps = Readonly<{
  cards: readonly BoardCard[];
  canRunTasks: boolean;
  column: BoardColumn;
  canToggleDone: boolean;
  doneExpanded: boolean;
  hasMoreCards: boolean;
  isLoadingMoreCards: boolean;
  isFirstActive: boolean;
  actionsDisabled: boolean;
  onCardClick: (taskID: string) => void;
  onDropTask: (event: DragEvent<HTMLElement>, column: BoardColumn) => void;
  onInterruptTask: (taskID: string, runID: string) => void;
  onLoadMoreCards: () => void;
  onResumeTask: (taskID: string, runID: string) => void;
  onToggleDone: () => void;
}>;

export function KanbanGroup({
  board,
  canRunTasks,
  columns,
  doneExpanded,
  canToggleDone,
  firstActiveColumnID,
  group,
  hasMoreCards,
  isLoadingMoreCards,
  actionsDisabled,
  onCardClick,
  onDropTask,
  onInterruptTask,
  onLoadMoreCards,
  onResumeTask,
  onToggleDone,
}: Readonly<{
  board: WorkflowBoard;
  canRunTasks: boolean;
  columns: readonly BoardColumn[];
  doneExpanded: boolean;
  firstActiveColumnID: string;
  group: BoardGroup;
  hasMoreCards: boolean;
  isLoadingMoreCards: boolean;
  onCardClick: (taskID: string) => void;
  onDropTask: (event: DragEvent<HTMLElement>, column: BoardColumn) => void;
  onInterruptTask: (taskID: string, runID: string) => void;
  onLoadMoreCards: () => void;
  onResumeTask: (taskID: string, runID: string) => void;
  onToggleDone: () => void;
  actionsDisabled: boolean;
  canToggleDone: boolean;
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
      <div className="grid h-full min-h-0 grid-flow-col auto-cols-[min(560px,80vw)] gap-[var(--space-3)]">
        {columns.map((column) => (
          <KanbanColumn
            cards={cardsForColumn(board, column, doneExpanded)}
            canRunTasks={canRunTasks}
            canToggleDone={canToggleDone}
            column={column}
            doneExpanded={doneExpanded}
            hasMoreCards={hasMoreCards}
            isLoadingMoreCards={isLoadingMoreCards}
            isFirstActive={column.id === firstActiveColumnID}
            actionsDisabled={actionsDisabled}
            key={column.id}
            onCardClick={onCardClick}
            onDropTask={onDropTask}
            onInterruptTask={onInterruptTask}
            onLoadMoreCards={onLoadMoreCards}
            onResumeTask={onResumeTask}
            onToggleDone={onToggleDone}
          />
        ))}
      </div>
    </section>
  );
}

export function KanbanColumn({
  cards,
  canRunTasks,
  canToggleDone,
  column,
  doneExpanded,
  hasMoreCards,
  isLoadingMoreCards,
  isFirstActive,
  actionsDisabled,
  onCardClick,
  onDropTask,
  onInterruptTask,
  onLoadMoreCards,
  onResumeTask,
  onToggleDone,
}: KanbanColumnProps) {
  const { t } = useTranslation();
  return (
    <section
      aria-label={column.name}
      className="island-glass grid h-full min-h-0 w-[min(560px,80vw)] shrink-0 grid-rows-[auto_auto_auto_minmax(0,1fr)] gap-[var(--space-3)] rounded-[var(--radius-xl)] p-[var(--space-3)] align-top"
      onDragOver={(event) => {
        event.preventDefault();
      }}
      onDrop={(event) => {
        onDropTask(event, column);
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
        <Badge tone={isFirstActive ? "info" : "neutral"}>{t("board.taskCount", { count: column.taskCount })}</Badge>
      </header>
      {isFirstActive ? (
        <p className="m-0 rounded-[var(--radius-m)] border border-dashed border-[var(--color-outline)] p-[var(--space-2)] text-sm text-[var(--color-muted)]">
          {t("board.dropToStart")}
        </p>
      ) : null}
      {column.isDone && canToggleDone ? (
        <Button onClick={onToggleDone} variant="ghost">
          {doneExpanded ? t("board.collapseDone") : t("board.expandDone")}
        </Button>
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
            canRunTasks={canRunTasks}
            actionsDisabled={actionsDisabled}
            key={card.id}
            onClick={() => {
              onCardClick(card.id);
            }}
            onInterrupt={(runID) => {
              onInterruptTask(card.id, runID);
            }}
            onResume={(runID) => {
              onResumeTask(card.id, runID);
            }}
          />
        ))}
        {isLoadingMoreCards ? (
          <p className="m-0 py-[var(--space-2)] text-sm text-[var(--color-muted)]">
            {t("app.loadingMore")}
          </p>
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
  canRunTasks,
  card,
  onClick,
  onInterrupt,
  onResume,
}: Readonly<{
  canRunTasks: boolean;
  card: BoardCard;
  actionsDisabled: boolean;
  onClick: () => void;
  onInterrupt: (runID: string) => void;
  onResume: (runID: string) => void;
}>) {
  const { t } = useTranslation();
  const draggable = canRunTasks && (card.actions.canStart || card.actions.manualMoveTargetNodeIDs.length > 0);
  return (
    <article
      aria-label={card.title}
      className="mb-[var(--space-3)] grid gap-[var(--space-2)] rounded-[var(--radius-l)] border border-[var(--color-outline)] bg-[var(--color-island-1)] p-[var(--space-3)]"
      draggable={draggable}
      onDragStart={(event) => {
        event.dataTransfer.setData("text/task-id", card.id);
      }}
    >
      <button
        className="grid gap-[var(--space-1)] bg-transparent p-0 text-left text-[var(--color-on-island)]"
        onClick={onClick}
        type="button"
      >
        <span className="font-mono text-[0.78rem] text-[var(--color-muted)]">{card.shortID}</span>
        <strong>{card.title}</strong>
        <span className="line-clamp-3 text-sm text-[var(--color-muted)]">{card.bodyPreview}</span>
      </button>
      <div className="flex flex-wrap items-center gap-[var(--space-2)] text-sm text-[var(--color-muted)]">
        <Badge tone={statusTone(card.status.kind)}>{card.status.label}</Badge>
        <Badge tone="neutral">{card.sourceWorkspace.name || t("board.workspace")}</Badge>
        {card.status.runIDs.length > 0 ? (
          <Badge tone="neutral">{t("task.runs")}: {card.status.runIDs.length}</Badge>
        ) : null}
        <span>{formatRelativeTime(card.updatedAt)}</span>
      </div>
      <TaskCardActions
        actionsDisabled={actionsDisabled}
        card={card}
        onClick={onClick}
        onInterrupt={onInterrupt}
        onResume={onResume}
      />
    </article>
  );
}

function TaskCardActions({
  card,
  actionsDisabled,
  onClick,
  onInterrupt,
  onResume,
}: Readonly<{
  card: BoardCard;
  actionsDisabled: boolean;
  onClick: () => void;
  onInterrupt: (runID: string) => void;
  onResume: (runID: string) => void;
}>) {
  const { t } = useTranslation();
  const needsDetail = card.actions.needsDetailForInterrupt || card.actions.needsDetailForResume;
  if (!card.actions.canInterrupt && !card.actions.canResume && !needsDetail) {
    return null;
  }
  return (
    <div className="flex flex-wrap gap-[var(--space-2)]">
      {card.actions.canResume ? (
        <Button
          onClick={() => {
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
          onClick={() => {
            onInterrupt(card.actions.interruptRunID);
          }}
          disabled={actionsDisabled}
          variant="secondary"
        >
          {t("board.interrupt")}
        </Button>
      ) : null}
      {needsDetail ? (
        <Button onClick={onClick} variant="secondary">
          {t("board.detail")}
        </Button>
      ) : null}
    </div>
  );
}

function statusTone(kind: string): "neutral" | "info" | "success" | "warning" | "danger" {
  switch (kind) {
    case "done":
      return "success";
    case "canceled":
      return "danger";
    case "running":
    case "waiting_question":
    case "waiting_approval":
    case "interrupted":
      return "warning";
    case "backlog":
      return "info";
    default:
      return "neutral";
  }
}
