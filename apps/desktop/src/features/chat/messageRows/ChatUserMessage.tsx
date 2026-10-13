import { useTranslation } from "react-i18next";
import { Pencil } from "lucide-react";
import { useLayoutEffect, useMemo, useRef } from "react";

import type { TranscriptRenderItem } from "@/app-facade";
import { AnimatedSize, IconTooltipButton, Island, Spinner, StaticMarkdown } from "@/ui";
import type { OutgoingInput } from "../OutgoingInputPresentation";

import "./messageRows.css";
import { MessageFooter } from "./MessageFooter";
import type { MessageNeighbors } from "./messageNeighbors";
import { useChatMessageMotion } from "../ChatMessageMotion";

export type ChatUserMessageItem = Extract<TranscriptRenderItem, { kind: "user" }>;

export type ChatMessageEditControl = Readonly<{ onEdit(item: ChatUserMessageItem): void }>;

export function ChatUserMessage(
  props:
    | Readonly<{
        item: ChatUserMessageItem;
        neighbors?: MessageNeighbors | undefined;
        edit: ChatMessageEditControl;
      }>
    | Readonly<{ preview: OutgoingInput; neighbors?: MessageNeighbors | undefined }>,
) {
  const item = "item" in props ? props.item : null;
  const preview = "preview" in props ? props.preview : null;
  const neighbors = props.neighbors;
  const { t } = useTranslation();
  const motion = useChatMessageMotion();
  const text = useRef<HTMLDivElement>(null);
  const previewID = preview?.inputID.toJSONValue() ?? null;
  const sources = useMemo(
    () => (item !== null ? item.value.QueueItemIDs : previewID === null ? [] : [previewID]),
    [item, previewID],
  );
  const value = "item" in props ? props.item.value.Text : props.preview.text;
  useLayoutEffect(() => {
    if (text.current === null) return;
    motion.arrive(sources, text.current);
    if (previewID !== null) return motion.register(previewID, text.current);
  }, [motion, sources, previewID]);
  return (
    <div
      className="chat-message-row chat-message-user"
      data-previous={neighbors?.previous}
      data-next={neighbors?.next}
    >
      <div className="chat-message-width">
        <Island className="chat-message-island" level={1} radius="l" unpadded>
          <AnimatedSize className="justify-start">
            <div className="chat-message-content">
              <div ref={text}>
                <StaticMarkdown value={value} />
              </div>
              {item === null ? (
                <footer className="chat-message-footer">
                  {preview?.confirmation === "awaiting" && (
                    <>
                      <Spinner size="sm" />
                      <span>{t("chatComposer.awaitingConfirmation")}</span>
                    </>
                  )}
                </footer>
              ) : (
                <MessageFooter
                  text={item.value.Text}
                  committedAt={item.value.committed_at_unix_ms}
                  edit={
                    item.value.RollbackTargetID == null ? null : (
                      <IconTooltipButton
                        label={t("chatTranscript.edit")}
                        onClick={() => {
                          if ("edit" in props) props.edit.onEdit(item);
                        }}
                        size="icon-sm"
                      >
                        <Pencil className="size-4" />
                      </IconTooltipButton>
                    )
                  }
                />
              )}
            </div>
          </AnimatedSize>
        </Island>
      </div>
    </div>
  );
}
