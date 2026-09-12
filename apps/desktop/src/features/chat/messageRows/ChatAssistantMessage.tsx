import type { TranscriptRenderItem } from "@/app-facade";
import { Island, StaticMarkdown, StreamingMarkdown } from "@/ui";

import { MessageFooter } from "./MessageFooter";
import type { MessageNeighbors } from "./messageNeighbors";
import "./messageRows.css";

export function ChatAssistantMessage({
  item,
  neighbors,
}: Readonly<{
  item: Extract<TranscriptRenderItem, { kind: "assistant" }>;
  neighbors?: MessageNeighbors;
}>) {
  return (
    <div
      className="chat-message-row chat-message-assistant"
      data-previous={neighbors?.previous}
      data-next={neighbors?.next}
    >
      <div className="chat-message-width">
        <Island className="chat-message-island" level={1} unpadded>
          {item.state === "live" ? (
            <StreamingMarkdown value={item.value.Text} />
          ) : (
            <StaticMarkdown value={item.value.Text} />
          )}
          {item.state === "committed" && (
            <MessageFooter text={item.value.Text} committedAt={item.value.committed_at_unix_ms} />
          )}
        </Island>
      </div>
    </div>
  );
}
