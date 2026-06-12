import { useEffect, useRef, useState } from "react";
import { Save } from "lucide-react";
import { useTranslation } from "react-i18next";

import type { TaskComment } from "../../api";
import { errorMessage } from "../../api/errors";
import { formatRelativeTime } from "../../app/formatters";
import { useStatusController } from "../../app/useStatusController";
import { Button, homeListCardMaxWidthClassName, IslandSurface, MarkdownText, Spinner } from "../../ui";
import { fieldInputClassName } from "../../ui/fieldInputStyles";
import { cx } from "../../ui/classes";
import { fieldLabelClassName } from "../../ui/fieldStyles";
import type { useTaskMutations } from "./useTaskDetailData";

export function Comments({
  comments,
  disabled,
  mutations,
  openLink,
}: Readonly<{
  comments: readonly TaskComment[];
  disabled: boolean;
  mutations: ReturnType<typeof useTaskMutations>;
  openLink: (url: string) => void;
}>) {
  const { t } = useTranslation();
  const { push } = useStatusController();
  const [body, setBody] = useState("");
  const [editing, setEditing] = useState<Readonly<{ id: string; body: string }> | null>(null);
  const commentBody = editing?.body ?? body;
  const pending =
    mutations.addComment.isPending || mutations.replaceComment.isPending || mutations.deleteComment.isPending;
  const interactionDisabled = disabled || pending;

  async function submit(): Promise<void> {
    if (interactionDisabled || commentBody.trim().length === 0) {
      return;
    }
    try {
      if (editing === null) {
        await mutations.addComment.mutateAsync(body);
        setBody("");
        return;
      }
      await mutations.replaceComment.mutateAsync({ commentID: editing.id, body: editing.body });
      setEditing(null);
    } catch (error) {
      push({
        id: "task-comment-save-error",
        tone: "danger",
        title: t("task.commentSaveFailed"),
        body: errorMessage(error),
      });
    }
  }

  async function deleteComment(commentID: string): Promise<void> {
    if (interactionDisabled) {
      return;
    }
    try {
      await mutations.deleteComment.mutateAsync(commentID);
    } catch (error) {
      push({
        id: "task-comment-delete-error",
        tone: "danger",
        title: t("task.commentDeleteFailed"),
        body: errorMessage(error),
      });
    }
  }

  return (
    <section className="grid gap-[var(--space-3)]">
      <div className="grid gap-[var(--space-3)]">
        <label className={fieldLabelClassName} htmlFor="task-comment-body">
          {editing === null ? t("task.addComment") : t("task.editComment")}
        </label>
        <div className="grid" data-testid="task-comment-input-frame">
          <textarea
            className={cx(fieldInputClassName, "col-start-1 row-start-1 block min-h-[88px] pb-0")}
            disabled={interactionDisabled}
            id="task-comment-body"
            onChange={(event) => {
              if (disabled) {
                return;
              }
              if (editing === null) {
                setBody(event.target.value);
                return;
              }
              setEditing({ id: editing.id, body: event.target.value });
            }}
            value={commentBody}
          />
          <Button
            aria-label={editing === null ? t("task.submitComment") : t("task.saveComment")}
            className="col-start-1 row-start-1 grid h-9 w-9 place-items-center self-end justify-self-end rounded-full !p-0"
            data-testid="task-comment-save"
            disabled={interactionDisabled || commentBody.trim().length === 0}
            onClick={() => void submit()}
            style={{ marginBottom: "var(--space-2)", marginRight: "var(--space-2)" }}
            variant="primary"
          >
            <Save aria-hidden="true" size={18} strokeWidth={1.8} />
          </Button>
        </div>
      </div>
      {comments.map((comment) => (
        <article
          className="grid gap-[var(--space-2)] rounded-[var(--radius-l)] border border-[var(--color-outline)] bg-[var(--color-island-1)] p-[var(--space-3)]"
          key={comment.id}
        >
          <MarkdownText onOpenLink={openLink} value={comment.body} />
          <span>{formatRelativeTime(comment.createdAt)}</span>
          <Button
            disabled={interactionDisabled}
            onClick={() => {
              setEditing({ id: comment.id, body: comment.body });
            }}
            variant="ghost"
          >
            {t("task.editComment")}
          </Button>
          <Button
            disabled={interactionDisabled}
            onClick={() => void deleteComment(comment.id)}
            variant="danger"
          >
            {t("task.deleteComment")}
          </Button>
        </article>
      ))}
    </section>
  );
}

export function ActivityFeed({
  items,
  hasNextPage,
  isFetchingNextPage,
  onLoadMore,
}: Readonly<{
  items: readonly { id: string; summary: string; occurredAt: number }[];
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  onLoadMore: () => void;
}>) {
  const { t } = useTranslation();
  const sentinelRef = useRef<HTMLDivElement | null>(null);
  const lastLoadMoreItemsLengthRef = useRef(-1);

  useEffect(() => {
    if (!hasNextPage || isFetchingNextPage || lastLoadMoreItemsLengthRef.current === items.length) {
      return undefined;
    }
    const sentinel = sentinelRef.current;
    if (sentinel === null) {
      return undefined;
    }
    const loadMore = () => {
      lastLoadMoreItemsLengthRef.current = items.length;
      onLoadMore();
    };
    if (typeof IntersectionObserver === "undefined") {
      loadMore();
      return undefined;
    }
    const observer = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) {
        if (lastLoadMoreItemsLengthRef.current === items.length) {
          return;
        }
        loadMore();
      }
    });
    observer.observe(sentinel);
    return () => {
      observer.disconnect();
    };
  }, [hasNextPage, isFetchingNextPage, items.length, onLoadMore]);

  return (
    <section className="grid justify-items-center gap-[var(--space-3)]" aria-label={t("task.activity")}>
      {items.length === 0 ? (
        <p className="m-0 text-[var(--color-muted)]">{t("task.noActivityTitle")}</p>
      ) : null}
      {items.map((item) => (
        <ActivityRow item={item} key={item.id} />
      ))}
      <div
        aria-label={isFetchingNextPage ? t("app.loadingMore") : undefined}
        aria-live="polite"
        className="grid min-h-10 place-items-center"
        ref={sentinelRef}
        role={isFetchingNextPage ? "status" : undefined}
      >
        {isFetchingNextPage ? (
          <>
            <Spinner size="sm" />
            <span className="sr-only">{t("app.loadingMore")}</span>
          </>
        ) : null}
      </div>
    </section>
  );
}

function ActivityRow({ item }: Readonly<{ item: { id: string; summary: string; occurredAt: number } }>) {
  return (
    <IslandSurface
      as="article"
      className={cx(
        "grid w-full gap-[var(--space-1)] rounded-[var(--radius-l)] p-[var(--space-3)]",
        homeListCardMaxWidthClassName,
      )}
      level={1}
    >
      <span>{item.summary}</span>
      <time className="text-sm text-[var(--color-muted)]">{formatRelativeTime(item.occurredAt)}</time>
    </IslandSurface>
  );
}
