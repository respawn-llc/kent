import { MessageCircle } from "lucide-react";
import { memo } from "react";
import { useTranslation } from "react-i18next";

import type { AttentionItem } from "@/api";
import {
  formatRelativeTime,
  taskDetailInitialFocusFromAttentionItem,
  useAppNavigation,
  type SidebarMode,
  type SidebarRootController,
} from "@/app-facade";
import { desktopChatEnabled } from "@/shared/feature-flags";
import { IconTooltipButton, Item, ItemContent, PromptAccessTargets } from "@/ui";
import { attentionChatTarget } from "./attentionChatTarget";

export const AttentionRow = memo(function AttentionRow({
  item,
  openSidebar,
  sidebarMode,
}: Readonly<{
  item: AttentionItem;
  openSidebar: SidebarRootController["open"];
  sidebarMode: SidebarMode;
}>) {
  const { t } = useTranslation();
  const navigation = useAppNavigation();
  const message =
    item.message ??
    (item.kind === "approval"
      ? t("app.attention.approvalFallback")
      : item.kind === "interrupted_current_node"
        ? t("app.attention.interruptedCurrentNodeFallback")
        : null);
  const chatTarget = desktopChatEnabled ? attentionChatTarget(item) : null;
  return (
    <div className="flex min-w-0 items-start" data-testid="attention-row">
      <Item
        className="min-w-0 flex-1 px-[var(--space-2)] py-[var(--space-3)]"
        aria-label={`${item.taskShortID} ${item.taskTitle}`}
        onClick={() => {
          openTaskDetail(item, openSidebar, sidebarMode);
        }}
      >
        <ItemContent className="min-w-0">
          <AttentionHeader item={item} />
          <AttentionBody item={item} message={message} />
        </ItemContent>
      </Item>
      {chatTarget === null ? null : (
        <IconTooltipButton
          className="mt-[var(--space-2)] text-[var(--color-muted)]"
          label={t("task.openChat", { name: item.taskTitle })}
          onClick={() => {
            void navigation.openSessionChat(chatTarget);
          }}
        >
          <MessageCircle className="size-4" strokeWidth={1.5} />
        </IconTooltipButton>
      )}
    </div>
  );
}, attentionRowPropsEqual);

function AttentionHeader({ item }: Readonly<{ item: AttentionItem }>) {
  return (
    <div className="flex min-w-0 flex-1 items-center gap-[var(--space-2)]">
      {item.taskShortID.length > 0 ? (
        <span className="min-w-0 shrink-0 truncate font-mono text-sm text-[var(--color-muted)]">
          {item.taskShortID}
        </span>
      ) : null}
      {item.taskTitle.length > 0 ? (
        <strong className="min-w-0 flex-1 truncate">{item.taskTitle}</strong>
      ) : null}
    </div>
  );
}

function AttentionBody({ item, message }: Readonly<{ item: AttentionItem; message: string | null }>) {
  return (
    <>
      {item.kind === "question" &&
      item.question.kind === "approval" &&
      item.question.accessTargets.length > 0 ? (
        <div className="min-w-0 line-clamp-2 break-words text-sm text-[var(--color-muted)]">
          <PromptAccessTargets targets={item.question.accessTargets} />
        </div>
      ) : (
        <span className="min-w-0 line-clamp-2 break-words text-sm text-[var(--color-muted)]">{message}</span>
      )}
      <span className="text-xs text-[var(--color-muted)]">{formatRelativeTime(item.occurredAt)}</span>
    </>
  );
}

function openTaskDetail(
  item: AttentionItem,
  openSidebar: SidebarRootController["open"],
  sidebarMode: SidebarMode,
): void {
  openSidebar({
    kind: "taskDetail",
    initialFocus: taskDetailInitialFocusFromAttentionItem(item),
    inboxNav: true,
    mode: sidebarMode,
    onMutated: undefined,
    taskID: item.taskID,
  });
}

function attentionRowPropsEqual(
  previous: Readonly<{
    item: AttentionItem;
    openSidebar: SidebarRootController["open"];
    sidebarMode: SidebarMode;
  }>,
  next: Readonly<{
    item: AttentionItem;
    openSidebar: SidebarRootController["open"];
    sidebarMode: SidebarMode;
  }>,
): boolean {
  return (
    previous.openSidebar === next.openSidebar &&
    previous.sidebarMode === next.sidebarMode &&
    attentionItemsEqual(previous.item, next.item)
  );
}

function attentionItemsEqual(previous: AttentionItem, next: AttentionItem): boolean {
  return (
    previous.id === next.id &&
    previous.kind === next.kind &&
    previous.taskID === next.taskID &&
    previous.taskShortID === next.taskShortID &&
    previous.taskTitle === next.taskTitle &&
    previous.message === next.message &&
    previous.occurredAt === next.occurredAt &&
    attentionChatTargetsEqual(attentionChatTarget(previous), attentionChatTarget(next))
  );
}

function attentionChatTargetsEqual(
  previous: ReturnType<typeof attentionChatTarget>,
  next: ReturnType<typeof attentionChatTarget>,
): boolean {
  return previous?.projectID === next?.projectID && previous?.sessionID === next?.sessionID;
}
