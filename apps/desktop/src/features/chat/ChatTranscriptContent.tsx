import { useMemo } from "react";
import type { ChatSessionTarget } from "@/api";
import { useOptionalChatRuntimeSnapshot, type TranscriptRenderItem } from "@/app-facade";
import { useComposerSurface } from "./ChatComposerSurface";
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
import { TranscriptToolSlot, type TranscriptToolSlotItem } from "./toolRows";

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
  target: ChatSessionTarget | null;
  effectiveWorkdir: string | null;
}>) {
  const snapshot = useOptionalChatRuntimeSnapshot();
  const { composer } = useComposerSurface();
  const items = snapshot?.transcript.items;
  const neighbors = useMemo(() => messageNeighbors(items ?? []), [items]);
  const localMessages = useMemo(
    () =>
      composer.outgoing
        .filter((input) => input.placement === "bubble")
        .map((input) => ({
          inputID: input.inputID.toJSONValue(),
          content: <ChatUserMessage preview={input} />,
        })),
    [composer.outgoing],
  );
  return (
    <ChatTranscriptTail
      bottomInset={bottomInset}
      openingVisible={openingVisible}
      estimateSize={estimateSize}
      localMessages={localMessages}
      slots={{
        user: (item) => <ChatUserMessage item={item} neighbors={neighbors.get(item.key)} edit={edit} />,
        assistant: (item) => <ChatAssistantMessage item={item} neighbors={neighbors.get(item.key)} />,
        tool: (item) => {
          if (target === null) throw new Error("Transcript tool requires a Session.");
          return <Tool item={item} target={target} effectiveWorkdir={effectiveWorkdir} />;
        },
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
  item: Extract<TranscriptRenderItem, { kind: "tool" | "tool_call" }>;
  target: ChatSessionTarget;
  effectiveWorkdir: string | null;
}>) {
  if (item.state === "committed" && isAskQuestionToolRow(item.row))
    return <TranscriptAskQuestionSlot row={item.row} />;
  const presentation = item.kind === "tool_call" ? item.value.Call.Presentation : item.value.Presentation;
  if (presentation?.Presentation === "ask_question") return null;
  const nonQuestion =
    presentation == null ? null : { ...presentation, Presentation: presentation.Presentation };
  let slotItem: TranscriptToolSlotItem;
  if (item.kind === "tool_call") {
    slotItem = {
      kind: "call",
      tool: { ...item.value.Call, Presentation: nonQuestion },
      activity: item.value.Activity === null ? "inactive" : "active",
    };
  } else if (item.state === "live") {
    slotItem = { kind: "call", tool: { ...item.value, Presentation: nonQuestion }, activity: "active" };
  } else {
    slotItem = {
      kind: "committed",
      row: { ...item.row, Kind: "tool", Tool: { ...item.value, Presentation: nonQuestion } },
    };
  }
  return <TranscriptToolSlot target={target} effectiveWorkdir={effectiveWorkdir} item={slotItem} />;
}
