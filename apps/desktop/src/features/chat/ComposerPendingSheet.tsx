import { ClockArrowRight, CornerDownRight, GitBranch, Minimize2, X } from "lucide-react";
import { AnimatePresence, useIsPresent } from "motion/react";
import { useLayoutEffect, useRef } from "react";
import { useAtomValue } from "@effect/atom-react";
import { useTranslation } from "react-i18next";

import type { PendingWorkItem } from "@/api";
import { AnimatedReveal, IconTooltipButton, ScrollRegion, Spinner } from "@/ui";
import type { useComposerPendingWork } from "./useComposerPendingWork";
import { useChatMessageMotion } from "./ChatMessageMotion";
import type { OutgoingInput } from "./OutgoingInputPresentation";

export function ComposerPendingSheet({
  pending,
  visible,
  outgoing = [],
}: Readonly<{
  pending: ReturnType<typeof useComposerPendingWork>;
  visible: boolean;
  outgoing?: readonly OutgoingInput[];
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
              <AnimatedReveal key={item.id.toJSONValue()} variant="collapse-center">
                <PendingRow item={item} pending={pending} visible={visible} />
              </AnimatedReveal>
            ))}
            {outgoing.map((input) => (
              <AnimatedReveal key={input.inputID.toJSONValue()} variant="collapse-center">
                <PendingText input={input} visible={visible} />
              </AnimatedReveal>
            ))}
          </AnimatePresence>
        </div>
      </ScrollRegion>
    </div>
  );
}

function PendingText({ input, visible }: Readonly<{ input: OutgoingInput; visible: boolean }>) {
  const { t } = useTranslation();
  return (
    <PendingRowSurface
      id={input.inputID.toJSONValue()}
      value={input.text}
      visible={visible}
      icon={input.intent === "queue" ? <ClockArrowRight size={16} /> : <CornerDownRight size={16} />}
      action={
        input.confirmation === "awaiting" ? (
          <span title={t("chatComposer.awaitingConfirmation")}>
            <Spinner size="sm" />
          </span>
        ) : null
      }
    />
  );
}

function PendingRow({
  item,
  pending,
  visible,
}: Readonly<{
  item: PendingWorkItem;
  pending: ReturnType<typeof useComposerPendingWork>;
  visible: boolean;
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
    <PendingRowSurface
      id={item.id.toJSONValue()}
      value={item.canonicalInput}
      visible={visible}
      icon={<Icon size={16} />}
      action={
        <IconTooltipButton
          label={t("chatComposer.discard")}
          disabled={loading || pending.interactionRestricted}
          tooltip={pending.interactionRestricted ? t("chat.navigationPending") : t("chatComposer.discard")}
          onClick={() => {
            pending.discard(item.id);
          }}
          size="icon-sm"
        >
          {loading ? <Spinner size="sm" /> : <X size={14} />}
        </IconTooltipButton>
      }
    />
  );
}

function PendingRowSurface({
  id,
  value,
  visible,
  icon,
  action,
}: Readonly<{
  id: string;
  value: string;
  visible: boolean;
  icon: React.ReactNode;
  action: React.ReactNode;
}>) {
  const motion = useChatMessageMotion();
  const text = useRef<HTMLSpanElement>(null);
  const present = useIsPresent();
  useLayoutEffect(() => {
    if (visible && present && text.current !== null) return motion.register(id, text.current);
  }, [motion, id, visible, present]);
  return (
    <div className="chat-composer-pending-row">
      <span className="shrink-0 text-[var(--color-muted)]">{icon}</span>
      <span ref={text} className="min-w-0 flex-1 line-clamp-2 whitespace-pre-wrap break-words">
        {value}
      </span>
      {action}
    </div>
  );
}
