import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Pencil } from "lucide-react";

import type { TranscriptRenderItem } from "@/app-facade";
import { CollapsibleMarkdownViewport, IconTooltipButton, Island, StaticMarkdown } from "@/ui";

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
  neighbors?: MessageNeighbors;
  edit: ChatMessageEditControl;
}>) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(false);
  return (
    <div
      className="chat-message-row chat-message-user"
      data-previous={neighbors?.previous}
      data-next={neighbors?.next}
    >
      <div className="chat-message-width">
        <Island className="chat-message-island" level={1} unpadded>
          <CollapsibleMarkdownViewport
            collapsedHeightClamp={{ kind: "lines", minimumLines: 10, maximumLines: 10, viewportPercent: 100 }}
            expanded={expanded}
            expandLabel={t("app.expand")}
            onExpand={() => {
              setExpanded(true);
            }}
          >
            <StaticMarkdown value={item.value.Text} />
          </CollapsibleMarkdownViewport>
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
