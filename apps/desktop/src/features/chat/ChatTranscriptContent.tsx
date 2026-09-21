import { useMemo } from "react";
import { useChatRuntimeSnapshot, type TranscriptRenderItem } from "@/app-facade";
import { ChatTranscriptTail } from "./ChatTailSurface";
import { ChatUserMessage, type ChatMessageEditControl } from "./messageRows/ChatUserMessage";
import { ChatAssistantMessage } from "./messageRows/ChatAssistantMessage";
import { messageNeighbors } from "./messageRows/messageNeighbors";
import {
  isAskQuestionToolRow,
  TranscriptAskQuestionSlot,
  TranscriptNoticeReviewerSlot,
  TranscriptReasoningSlot,
  TranscriptThinkingStatus,
} from "./transcriptRows";
import { TranscriptToolSlot } from "./toolRows";

const estimateSize = () => 120;

export function ChatTranscriptContent({
  edit,
  openingVisible,
}: Readonly<{ edit: ChatMessageEditControl; openingVisible: boolean }>) {
  const { transcript } = useChatRuntimeSnapshot();
  const neighbors = useMemo(() => messageNeighbors(transcript.items), [transcript.items]);
  return (
    <ChatTranscriptTail
      openingVisible={openingVisible}
      estimateSize={estimateSize}
      slots={{
        user: (item) => <ChatUserMessage item={item} neighbors={neighbors.get(item.key)} edit={edit} />,
        assistant: (item) => <ChatAssistantMessage item={item} neighbors={neighbors.get(item.key)} />,
        tool: (item) => <Tool item={item} />,
        reasoning: (item) => <TranscriptReasoningSlot item={item} />,
        notice: (item) => <TranscriptNoticeReviewerSlot row={item.row} />,
        thinkingStatus: (presentation) => <TranscriptThinkingStatus presentation={presentation} />,
      }}
    />
  );
}

function Tool({ item }: Readonly<{ item: Extract<TranscriptRenderItem, { kind: "tool" }> }>) {
  if (item.state === "committed" && isAskQuestionToolRow(item.row))
    return <TranscriptAskQuestionSlot row={item.row} />;
  const presentation = item.value.Presentation;
  if (presentation?.Presentation === "ask_question") return null;
  const nonQuestion =
    presentation == null ? null : { ...presentation, Presentation: presentation.Presentation };
  return item.state === "live" ? (
    <TranscriptToolSlot item={{ kind: "live", tool: { ...item.value, Presentation: nonQuestion } }} />
  ) : (
    <TranscriptToolSlot
      item={{
        kind: "committed",
        row: { ...item.row, Kind: "tool", Tool: { ...item.value, Presentation: nonQuestion } },
      }}
    />
  );
}
