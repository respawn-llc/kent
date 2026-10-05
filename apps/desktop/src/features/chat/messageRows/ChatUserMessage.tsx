import { useTranslation } from "react-i18next";
import { Pencil } from "lucide-react";

import type { TranscriptRenderItem } from "@/app-facade";
import { IconTooltipButton, Island, StaticMarkdown } from "@/ui";

import "./messageRows.css";
import { MessageFooter } from "./MessageFooter";
import type { MessageNeighbors } from "./messageNeighbors";

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
  return (
    <div
      className="chat-message-row chat-message-user"
      data-previous={neighbors?.previous}
      data-next={neighbors?.next}
    >
      <div className="chat-message-width">
        <Island className="chat-message-island" level={1} radius="l" unpadded>
          <StaticMarkdown value={item.value.Text} />
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
        </Island>
      </div>
    </div>
  );
}
