import { useCallback, useState, type ReactElement, type ReactNode } from "react";

import {
  AnimatedReveal,
  VirtualizedInfiniteList,
  type VirtualizedEndAnchoring,
  type VirtualizedInfiniteListBoundaryState,
  useStableCallback,
} from "@/ui";
import type {
  ChatTranscriptPresentationUpdate,
  TranscriptDirection,
  TranscriptRenderItem,
  TranscriptRenderSlots,
  TranscriptWindowInput,
  TranscriptWindowSnapshot,
} from "@/app-facade";
import "./transcriptWindow.css";

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
  bottomInset?: number | undefined;
  overlay?: ((following: boolean) => ReactNode) | undefined;
  tailFailure?: ReactElement | undefined;
}>;

type ViewItem =
  | TranscriptRenderItem
  | Readonly<{ kind: "status"; key: "thinking-status-tail" }>
  | Readonly<{ kind: "failure"; key: "transcript-failure-tail" }>;
const statusItem = { kind: "status", key: "thinking-status-tail" } as const;
const failureItem = { kind: "failure", key: "transcript-failure-tail" } as const;
const transcriptContentClassName = "mx-auto w-full max-w-[var(--chat-content-max-width)]";

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
  bottomInset = 0,
  tailFailure,
}: TranscriptWindowViewProps) {
  const [atEnd, setAtEnd] = useState(false);
  const [arrivals, setArrivals] = useState(() => ({
    update: presentationUpdate,
    keys: new Set(snapshot.items.map((item) => item.key)),
    entering: new Set<string>(),
  }));
  let entering = arrivals.entering;
  if (arrivals.update !== presentationUpdate) {
    const keys = new Set(snapshot.items.map((item) => item.key));
    entering =
      presentationUpdate?.kind === "content"
        ? new Set([...keys].filter((key) => !arrivals.keys.has(key)))
        : new Set<string>();
    setArrivals({ update: presentationUpdate, keys, entering });
  }
  const emitInput = useStableCallback(onInput);
  const olderAvailable = snapshot.older.cursor !== null;
  const newerAvailable = snapshot.newer.cursor !== null;
  const following = atEnd && !newerAvailable;
  const nativeRequest = useTranscriptEndRequest(
    { opening: snapshot.opening.kind, update: presentationUpdate, explicit: scrollRequest, bottomInset },
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
    { loadingLabel, retryLabel, errorMessage: boundaryErrorMessage, suppressed: tailFailure !== undefined },
    emitInput,
  );
  const nextBoundary = transcriptBoundary(
    snapshot.newer,
    "newer",
    { loadingLabel, retryLabel, errorMessage: boundaryErrorMessage, suppressed: tailFailure !== undefined },
    emitInput,
  );
  const items = transcriptViewItems(snapshot, tailFailure !== undefined);

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
        paddingEnd={bottomInset}
        rowSpacing="tight"
        getItemKey={(item) => item.key}
        getItemWrapperProps={(item) => ({
          className:
            item.kind === "tool" || item.kind === "notice"
              ? "transcript-window-disclosure-row"
              : item.kind === "user" || item.kind === "assistant"
                ? "transcript-window-message-row"
                : undefined,
        })}
        hasNextPage={tailFailure === undefined && newerAvailable && snapshot.newer.kind !== "error"}
        hasPreviousPage={tailFailure === undefined && olderAvailable && snapshot.older.kind !== "error"}
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
          <AnimatedReveal className={transcriptContentClassName} enter={entering.has(item.key)}>
            {item.kind === "failure" ? (
              tailFailure
            ) : item.kind === "status" ? (
              slots.thinkingStatus(snapshot.thinkingStatus)
            ) : (
              <TranscriptFamilySlot item={item} slots={slots} />
            )}
          </AnimatedReveal>
        )}
      />
      {overlay?.(following)}
    </div>
  );
}

type TranscriptEndInput = Readonly<{
  opening: TranscriptWindowSnapshot["opening"]["kind"];
  update: ChatTranscriptPresentationUpdate | undefined;
  explicit: symbol | null | undefined;
  bottomInset: number;
}>;

function transcriptViewItems(snapshot: TranscriptWindowSnapshot, failed: boolean): readonly ViewItem[] {
  if (failed) return [...snapshot.items, failureItem];
  return snapshot.showsLive ? [...snapshot.items, statusItem] : snapshot.items;
}

function useTranscriptEndRequest(
  input: TranscriptEndInput,
  following: boolean,
): VirtualizedEndAnchoring["scrollRequest"] {
  const [previous, setPrevious] = useState(input);
  const [request, setRequest] = useState<VirtualizedEndAnchoring["scrollRequest"]>(() =>
    input.opening === "ready" ? { behavior: "auto" } : null,
  );
  if (
    previous.update === input.update &&
    previous.explicit === input.explicit &&
    previous.opening === input.opening &&
    previous.bottomInset === input.bottomInset
  ) {
    return request;
  }
  setPrevious(input);
  const behavior = transcriptEndBehavior(previous, input, following);
  if (behavior !== null) setRequest({ behavior });
  return request;
}

function transcriptEndBehavior(
  previous: TranscriptEndInput,
  current: TranscriptEndInput,
  following: boolean,
): "auto" | "smooth" | null {
  if (previous.opening !== "ready" && current.opening === "ready") return "auto";
  if (following && previous.bottomInset !== current.bottomInset) return "auto";
  if (current.explicit != null && previous.explicit !== current.explicit) return "smooth";
  if (!following) return null;
  if (previous.update !== current.update && current.update?.kind === "content") return "smooth";
  return null;
}

function transcriptBoundary(
  boundary: TranscriptWindowSnapshot[TranscriptDirection],
  direction: TranscriptDirection,
  copy: Readonly<{
    loadingLabel: string;
    retryLabel: string;
    errorMessage: (error: Error) => string;
    suppressed: boolean;
  }>,
  onInput: (input: TranscriptWindowViewInput) => void,
): VirtualizedInfiniteListBoundaryState | undefined {
  if (copy.suppressed) return undefined;
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
