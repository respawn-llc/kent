import { ClockArrowRight, CornerDownRight, GitBranch, Minimize2, X } from "lucide-react";
import { AnimatePresence } from "motion/react";
import { useLayoutEffect, useRef } from "react";
import { useAtomValue } from "@effect/atom-react";
import { useTranslation } from "react-i18next";

import type { PendingWorkItem } from "@/api";
import { AnimatedReveal, IconTooltipButton, ScrollRegion, Spinner } from "@/ui";
import type { useComposerPendingWork } from "./useComposerPendingWork";

export function ComposerPendingSheet({
  pending,
  visible,
}: Readonly<{
  pending: ReturnType<typeof useComposerPendingWork>;
  visible: boolean;
}>) {
  const container = useRef<HTMLDivElement>(null);
  const content = useRef<HTMLDivElement>(null);
  const followsNewest = useRef(true);
  useLayoutEffect(() => {
    const element = container.current;
    const body = content.current;
    if (element === null || body === null) return;
    const onScroll = () => {
      followsNewest.current = element.scrollHeight - element.scrollTop - element.clientHeight <= 1;
    };
    const follow = () => {
      if (followsNewest.current) element.scrollTop = element.scrollHeight - element.clientHeight;
    };
    element.addEventListener("scroll", onScroll);
    const observer = new ResizeObserver(follow);
    observer.observe(body);
    follow();
    return () => {
      observer.disconnect();
      element.removeEventListener("scroll", onScroll);
    };
  }, []);
  return (
    <div className={visible ? undefined : "invisible pointer-events-none absolute inset-x-0 top-0"}>
      <ScrollRegion className="chat-composer-sheet" ref={container}>
        <div ref={content}>
          <AnimatePresence initial={false}>
            {pending.items.map((item) => (
              <AnimatedReveal key={item.id.toJSONValue()}>
                <PendingRow item={item} pending={pending} />
              </AnimatedReveal>
            ))}
          </AnimatePresence>
        </div>
      </ScrollRegion>
    </div>
  );
}

function PendingRow({
  item,
  pending,
}: Readonly<{
  item: PendingWorkItem;
  pending: ReturnType<typeof useComposerPendingWork>;
}>) {
  const { t } = useTranslation();
  const loading = useAtomValue(pending.discardPending(item.id.toJSONValue()));
  const Icon =
    item.kind === "manual_compaction"
      ? Minimize2
      : item.kind === "worktree_transition"
        ? GitBranch
        : item.lane === "queue"
          ? ClockArrowRight
          : CornerDownRight;
  return (
    <div className="chat-composer-pending-row">
      <Icon size={16} className="shrink-0 text-[var(--color-muted)]" />
      <span className="min-w-0 flex-1 line-clamp-2 whitespace-pre-wrap break-words">
        {item.canonicalInput}
      </span>
      <IconTooltipButton
        label={t("chatComposer.discard")}
        onClick={() => {
          pending.discard(item.id);
        }}
        size="icon-sm"
      >
        {loading ? <Spinner size="sm" /> : <X size={14} />}
      </IconTooltipButton>
    </div>
  );
}
