import { useState } from "react";
import { useTranslation } from "react-i18next";

import type { TaskComment } from "../../api";
import { formatRelativeTime } from "../../app/formatters";
import { Button, MarkdownText, TextArea, VirtualizedInfiniteList } from "../../ui";
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
  const [body, setBody] = useState("");
  const [editing, setEditing] = useState<Readonly<{ id: string; body: string }> | null>(null);

  async function submit(): Promise<void> {
    if (editing === null) {
      await mutations.addComment.mutateAsync(body);
      setBody("");
      return;
    }
    await mutations.replaceComment.mutateAsync({ commentID: editing.id, body: editing.body });
    setEditing(null);
  }

  return (
    <section className="grid gap-[var(--space-3)]">
      <h3>{t("task.comments")}</h3>
      <TextArea
        label={editing === null ? t("task.addComment") : t("task.editComment")}
        onChange={(event) => {
          if (editing === null) {
            setBody(event.target.value);
            return;
          }
          setEditing({ id: editing.id, body: event.target.value });
        }}
        rows={3}
        value={editing?.body ?? body}
      />
      <Button
        disabled={disabled || (editing?.body ?? body).trim().length === 0}
        onClick={() => void submit()}
        variant="primary"
      >
        {editing === null ? t("task.addComment") : t("task.save")}
      </Button>
      {comments.map((comment) => (
        <article
          className="grid gap-[var(--space-2)] rounded-[var(--radius-l)] border border-[var(--color-outline)] bg-[var(--color-island-1)] p-[var(--space-3)]"
          key={comment.id}
        >
          <MarkdownText onOpenLink={openLink} value={comment.body} />
          <span>{formatRelativeTime(comment.createdAt)}</span>
          <Button
            disabled={disabled}
            onClick={() => {
              setEditing({ id: comment.id, body: comment.body });
            }}
            variant="ghost"
          >
            {t("task.editComment")}
          </Button>
          <Button
            disabled={disabled}
            onClick={() => void mutations.deleteComment.mutateAsync(comment.id)}
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
  return (
    <section className="grid gap-[var(--space-3)]">
      <VirtualizedInfiniteList
        className="h-[min(520px,56vh)] min-h-0 overflow-auto px-[var(--space-1)] hide-scrollbar contain-strict [-webkit-overflow-scrolling:touch]"
        empty={<p>{t("task.noActivityTitle")}</p>}
        estimateSize={() => 76}
        getItemKey={(item) => item.id}
        hasNextPage={hasNextPage}
        header={<h3>{t("task.activity")}</h3>}
        isFetchingNextPage={isFetchingNextPage}
        items={items}
        loadingLabel={t("app.loadingMore")}
        onLoadMore={onLoadMore}
        renderItem={(item) => <ActivityRow item={item} />}
      />
    </section>
  );
}

function ActivityRow({ item }: Readonly<{ item: { id: string; summary: string; occurredAt: number } }>) {
  return (
    <article className="grid gap-[var(--space-1)] rounded-[var(--radius-m)] border border-[var(--color-outline)] bg-[var(--color-island-1)] p-[var(--space-3)]">
      <span>{item.summary}</span>
      <time className="text-sm text-[var(--color-muted)]">{formatRelativeTime(item.occurredAt)}</time>
    </article>
  );
}
