import { useCallback, useState, type ReactElement, type ReactNode } from "react";

import { VirtualizedInfiniteList, type VirtualizedInfiniteListBoundaryState, useStableCallback } from "@/ui";
import type {
  ChatTranscriptPresentationUpdate,
  TranscriptDirection,
  TranscriptRenderItem,
  TranscriptRenderSlots,
  TranscriptWindowInput,
  TranscriptWindowSnapshot,
} from "@/app-facade";

type TranscriptWindowViewInput = Extract<TranscriptWindowInput, { kind: "edge-visit" | "retry" }>;

export type TranscriptWindowViewProps = Readonly<{
  snapshot: TranscriptWindowSnapshot;
  slots: TranscriptRenderSlots<ReactElement | null>;
  estimateSize: () => number;
  loadingLabel: string;
  retryLabel: string;
  boundaryErrorMessage: (error: Error) => string;
  onInput: (input: TranscriptWindowViewInput) => void;
  presentationUpdate?: ChatTranscriptPresentationUpdate | undefined;
  scrollRequest?: symbol | null | undefined;
  overlay?: ((following: boolean) => ReactNode) | undefined;
}>;

type ViewItem = TranscriptRenderItem | Readonly<{ kind: "status"; key: "thinking-status-tail" }>;
const statusItem = { kind: "status", key: "thinking-status-tail" } as const;
const transcriptContentClassName = "mx-auto w-full max-w-[1200px]";

export function TranscriptWindowView({
  snapshot,
  slots,
  estimateSize,
  loadingLabel,
  retryLabel,
  boundaryErrorMessage,
  onInput,
  presentationUpdate,
  scrollRequest,
  overlay,
}: TranscriptWindowViewProps) {
  const [atEnd, setAtEnd] = useState(false);
  const emitInput = useStableCallback(onInput);
  const olderAvailable = snapshot.older.cursor !== null;
  const newerAvailable = snapshot.newer.cursor !== null;
  const following = atEnd && !newerAvailable;
  const nativeRequest = useTranscriptEndRequest(
    snapshot.opening.kind,
    presentationUpdate,
    scrollRequest,
    following,
  );
  const visit = useCallback(
    (direction: TranscriptDirection) => {
      emitInput({ kind: "edge-visit", direction, older: olderAvailable, newer: newerAvailable });
    },
    [emitInput, newerAvailable, olderAvailable],
  );
  const previousBoundary = transcriptBoundary(
    snapshot.older,
    "older",
    { loadingLabel, retryLabel, errorMessage: boundaryErrorMessage },
    emitInput,
  );
  const nextBoundary = transcriptBoundary(
    snapshot.newer,
    "newer",
    { loadingLabel, retryLabel, errorMessage: boundaryErrorMessage },
    emitInput,
  );
  const items: readonly ViewItem[] = snapshot.showsLive ? [...snapshot.items, statusItem] : snapshot.items;

  return (
    <div className="relative h-full min-h-0">
      <VirtualizedInfiniteList
        className="h-full min-h-0 w-full min-w-0 overflow-x-hidden overflow-y-auto"
        endAnchoring={{
          followEnabled: !newerAvailable,
          threshold: 80,
          scrollRequest: nativeRequest,
          onEndChange: setAtEnd,
        }}
        estimateSize={estimateSize}
        rowSpacing="tight"
        getItemKey={(item) => item.key}
        hasNextPage={newerAvailable && snapshot.newer.kind !== "error"}
        hasPreviousPage={olderAvailable && snapshot.older.kind !== "error"}
        isFetchingNextPage={snapshot.newer.kind === "loading"}
        isFetchingPreviousPage={snapshot.older.kind === "loading"}
        items={items}
        loadingLabel={loadingLabel}
        nextBoundary={nextBoundary}
        onLoadMore={() => {
          visit("newer");
        }}
        onLoadPrevious={() => {
          visit("older");
        }}
        previousBoundary={previousBoundary}
        renderItem={(item) => (
          <div className={transcriptContentClassName}>
            {item.kind === "status" ? (
              slots.thinkingStatus(snapshot.thinkingStatus)
            ) : (
              <TranscriptFamilySlot item={item} slots={slots} />
            )}
          </div>
        )}
      />
      {overlay?.(following)}
    </div>
  );
}

function useTranscriptEndRequest(
  opening: TranscriptWindowSnapshot["opening"]["kind"],
  update: ChatTranscriptPresentationUpdate | undefined,
  explicit: symbol | null | undefined,
  following: boolean,
): symbol | null {
  const [previous, setPrevious] = useState({ update, explicit, opening });
  const [request, setRequest] = useState<symbol | null>(() =>
    opening === "ready" ? Symbol("open transcript") : null,
  );
  if (previous.update === update && previous.explicit === explicit && previous.opening === opening) {
    return request;
  }
  setPrevious({ update, explicit, opening });
  const deliberate = explicit != null && previous.explicit !== explicit;
  const opened = previous.opening !== "ready" && opening === "ready";
  const arrived = previous.update !== update && update?.kind === "content" && following;
  if (deliberate || opened || arrived) setRequest(Symbol("transcript end"));
  return request;
}

function transcriptBoundary(
  boundary: TranscriptWindowSnapshot[TranscriptDirection],
  direction: TranscriptDirection,
  copy: Readonly<{
    loadingLabel: string;
    retryLabel: string;
    errorMessage: (error: Error) => string;
  }>,
  onInput: (input: TranscriptWindowViewInput) => void,
): VirtualizedInfiniteListBoundaryState | undefined {
  switch (boundary.kind) {
    case "idle":
      return undefined;
    case "loading":
      return { state: "loading", label: copy.loadingLabel };
    case "error":
      return {
        state: "error",
        message: copy.errorMessage(boundary.error),
        retryLabel: copy.retryLabel,
        onRetry: () => {
          onInput({ kind: "retry", direction });
        },
      };
  }
}

function TranscriptFamilySlot({
  item,
  slots,
}: Readonly<{
  item: TranscriptRenderItem;
  slots: TranscriptRenderSlots<ReactElement | null>;
}>): ReactElement | null {
  switch (item.kind) {
    case "user":
      return slots.user(item);
    case "assistant":
      return slots.assistant(item);
    case "tool":
      return slots.tool(item);
    case "reasoning_trace":
      return slots.reasoning(item);
    case "notice":
    case "reviewer_feedback":
    case "reviewer_error":
      return slots.notice(item);
  }
}
