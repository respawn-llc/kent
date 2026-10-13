import { useTranslation } from "react-i18next";
import { Pencil } from "lucide-react";
import { useLayoutEffect, useRef } from "react";

import type { TranscriptRenderItem } from "@/app-facade";
import { AnimatedSize, IconTooltipButton, Island, StaticMarkdown } from "@/ui";

import "./messageRows.css";
import { MessageFooter } from "./MessageFooter";
import type { MessageNeighbors } from "./messageNeighbors";
import { useChatMessageMotion } from "../ChatMessageMotion";

export type ChatUserMessageItem = Extract<TranscriptRenderItem, { kind: "user" }>;

export type ChatMessageEditControl = Readonly<{ onEdit(item: ChatUserMessageItem): void }>;

export function ChatUserMessage({
  item,
  neighbors,
  edit,
}: Readonly<{
  item: ChatUserMessageItem;
  neighbors?: MessageNeighbors | undefined;
  edit: ChatMessageEditControl;
}>) {
  const { t } = useTranslation();
  const motion = useChatMessageMotion();
  const text = useRef<HTMLDivElement>(null);
  const bubble = useRef<HTMLDivElement>(null);
  const sources = item.value.QueueItemIDs;
  useLayoutEffect(() => {
    if (text.current !== null && bubble.current !== null)
      motion.arrive(sources, text.current, bubble.current);
  }, [motion, sources]);
  return (
    <div
      className="chat-message-row chat-message-user"
      data-previous={neighbors?.previous}
      data-next={neighbors?.next}
    >
      <div className="chat-message-width" ref={bubble}>
        <Island className="chat-message-island" level={1} radius="l" unpadded>
          <AnimatedSize className="justify-start">
            <div className="chat-message-content">
              <div ref={text}>
                <StaticMarkdown value={item.value.Text} />
              </div>
              <MessageFooter
                text={item.value.Text}
                committedAt={item.value.committed_at_unix_ms}
                edit={
                  item.value.RollbackTargetID == null ? null : (
                    <IconTooltipButton
                      label={t("chatTranscript.edit")}
                      onClick={() => {
                        edit.onEdit(item);
                      }}
                      size="icon-sm"
                    >
                      <Pencil className="size-4" />
                    </IconTooltipButton>
                  )
                }
              />
            </div>
          </AnimatedSize>
        </Island>
      </div>
    </div>
  );
}
