import { GitBranch, ListEnd, Minimize2, Undo2, X } from "lucide-react";
import { useLayoutEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import type { PendingWorkItem } from "@/api";
import { IconTooltipButton, Spinner, VirtualizedInfiniteList } from "@/ui";
import type { useComposerPendingWork } from "./useComposerPendingWork";

const noPaging = () => {
  /* Pending Work is the approved whole-collection read. */
};
const estimateRow = () => 56;
const itemKey = (item: PendingWorkItem) => item.id.toJSONValue();

export function ComposerPendingSheet({
  pending,
  visible,
  disconnected,
}: Readonly<{
  pending: ReturnType<typeof useComposerPendingWork>;
  visible: boolean;
  disconnected: boolean;
}>) {
  const { t } = useTranslation();
  const [element, setElement] = useState<HTMLDivElement | null>(null);
  const [followsNewest, setFollowsNewest] = useState(true);
  useLayoutEffect(() => {
    if (element === null) return;
    const onScroll = () => {
      setFollowsNewest(element.scrollHeight - element.scrollTop - element.clientHeight <= 1);
    };
    element.addEventListener("scroll", onScroll);
    return () => {
      element.removeEventListener("scroll", onScroll);
    };
  }, [element]);
  return (
    <div className={visible ? undefined : "invisible pointer-events-none absolute inset-x-0 top-0"}>
      <VirtualizedInfiniteList
        className="chat-composer-sheet"
        estimateSize={estimateRow}
        getItemKey={itemKey}
        hasNextPage={false}
        hasPreviousPage={false}
        isFetchingNextPage={false}
        initialScrollKey={followsNewest ? pending.items.at(-1)?.id.toJSONValue() : undefined}
        initialScrollAlign="auto"
        items={pending.items}
        layoutChangeScrollBehavior="preserve-leading-item"
        loadingLabel={t("app.loading")}
        onLoadMore={noPaging}
        onScrollElementChange={setElement}
        rowSpacing="tight"
        renderItem={(item) => {
          const Icon =
            item.kind === "manual_compaction"
              ? Minimize2
              : item.kind === "worktree_transition"
                ? GitBranch
                : item.lane === "queue"
                  ? ListEnd
                  : Undo2;
          const loading = Array.from(pending.discarding.values()).some(
            (identity) => identity.toJSONValue() === item.id.toJSONValue(),
          );
          return (
            <div className="chat-composer-pending-row">
              <Icon size={16} className="shrink-0 text-[var(--color-muted)]" />
              <span className="min-w-0 flex-1 line-clamp-2 whitespace-pre-wrap break-words">
                {item.canonicalInput}
              </span>
              <IconTooltipButton
                label={disconnected ? t("common.readOnly") : t("chatComposer.discard")}
                disabled={disconnected}
                onClick={() => {
                  void pending.discard(item.id);
                }}
                size="icon-sm"
              >
                {loading ? <Spinner size="sm" /> : <X size={14} />}
              </IconTooltipButton>
            </div>
          );
        }}
      />
    </div>
  );
}
