import { useMemo } from "react";
import type { ChatSessionTarget } from "@/api";
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
import { useChatMessageMotion } from "./ChatMessageMotion";

const estimateSize = () => 120;

export function ChatTranscriptContent({
  edit,
  openingVisible,
  bottomInset,
  target,
  effectiveWorkdir,
}: Readonly<{
  edit: ChatMessageEditControl;
  openingVisible: boolean;
  bottomInset: number;
  target: ChatSessionTarget;
  effectiveWorkdir: string | null;
}>) {
  const { transcript } = useChatRuntimeSnapshot();
  const motion = useChatMessageMotion();
  const neighbors = useMemo(() => messageNeighbors(transcript.items), [transcript.items]);
  return (
    <ChatTranscriptTail
      bottomInset={bottomInset}
      openingVisible={openingVisible}
      estimateSize={estimateSize}
      ownsEntryMotion={(item) => item.kind === "user" && motion.hasSource(item.value.QueueItemIDs)}
      slots={{
        user: (item) => <ChatUserMessage item={item} neighbors={neighbors.get(item.key)} edit={edit} />,
        assistant: (item) => <ChatAssistantMessage item={item} neighbors={neighbors.get(item.key)} />,
        tool: (item) => <Tool item={item} target={target} effectiveWorkdir={effectiveWorkdir} />,
        reasoning: (item) => <TranscriptReasoningSlot item={item} />,
        notice: (item) => <TranscriptNoticeReviewerSlot row={item.row} />,
        thinkingStatus: (presentation) => <TranscriptThinkingStatus presentation={presentation} />,
      }}
    />
  );
}

function Tool({
  item,
  target,
  effectiveWorkdir,
}: Readonly<{
  item: Extract<TranscriptRenderItem, { kind: "tool" }>;
  target: ChatSessionTarget;
  effectiveWorkdir: string | null;
}>) {
  if (item.state === "committed" && isAskQuestionToolRow(item.row))
    return <TranscriptAskQuestionSlot row={item.row} />;
  const presentation = item.value.Presentation;
  if (presentation?.Presentation === "ask_question") return null;
  const nonQuestion =
    presentation == null ? null : { ...presentation, Presentation: presentation.Presentation };
  return item.state === "live" ? (
    <TranscriptToolSlot
      target={target}
      effectiveWorkdir={effectiveWorkdir}
      item={{ kind: "live", tool: { ...item.value, Presentation: nonQuestion } }}
    />
  ) : (
    <TranscriptToolSlot
      target={target}
      effectiveWorkdir={effectiveWorkdir}
      item={{
        kind: "committed",
        row: { ...item.row, Kind: "tool", Tool: { ...item.value, Presentation: nonQuestion } },
      }}
    />
  );
}
