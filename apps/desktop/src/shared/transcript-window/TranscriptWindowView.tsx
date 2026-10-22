import { useMemo, useState, type ReactElement, type ReactNode } from "react";

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
  snapshot: TranscriptWindowSnapshot | null;
  localMessages?: readonly Readonly<{ inputID: string; content: ReactElement }>[] | undefined;
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
  | Readonly<{ kind: "local-message"; key: string; content: ReactElement }>
  | Readonly<{ kind: "status"; key: "thinking-status-tail" }>
  | Readonly<{ kind: "failure"; key: "transcript-failure-tail" }>;
const statusItem = { kind: "status", key: "thinking-status-tail" } as const;
const failureItem = { kind: "failure", key: "transcript-failure-tail" } as const;
const transcriptContentClassName = "mx-auto w-full max-w-[var(--chat-content-max-width)]";
const noLocalMessages: NonNullable<TranscriptWindowViewProps["localMessages"]> = [];

function presentationKey(item: ViewItem): string {
  const id = item.kind === "user" ? item.value.QueueItemIDs[0] : undefined;
  return id === undefined ? item.key : `input:${id}`;
}

export function TranscriptWindowView({
  snapshot,
  localMessages = noLocalMessages,
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
  const items = transcriptViewItems(snapshot, tailFailure !== undefined, localMessages);
  const entering = useArrivals(items, presentationUpdate, localMessages);
  const emitInput = useStableCallback(onInput);
  const edges = windowEdges(snapshot);
  const { olderAvailable, newerAvailable, older, newer, opening, thinkingStatus } = edges;
  const olderPage = transcriptPagination(older, tailFailure !== undefined);
  const newerPage = transcriptPagination(newer, tailFailure !== undefined);
  const following = atEnd && !newerAvailable;
  const nativeRequest = useTranscriptEndRequest(
    {
      opening,
      update: presentationUpdate,
      explicit: scrollRequest,
      bottomInset,
      newerAvailable,
      localMessages,
    },
    following,
  );
  const visit = useStableCallback((direction: TranscriptDirection) => {
    emitInput({ kind: "edge-visit", direction, older: olderAvailable, newer: newerAvailable });
  });
  const previousBoundary = transcriptBoundary(
    older,
    "older",
    { loadingLabel, retryLabel, errorMessage: boundaryErrorMessage, suppressed: tailFailure !== undefined },
    emitInput,
  );
  const nextBoundary = transcriptBoundary(
    newer,
    "newer",
    { loadingLabel, retryLabel, errorMessage: boundaryErrorMessage, suppressed: tailFailure !== undefined },
    emitInput,
  );
  const pinnedItemKeys = useMemo(
    () => new Set(snapshot?.activeAssistants.map((item) => item.key)),
    [snapshot?.activeAssistants],
  );

  return (
    <div className="relative h-full min-h-0">
      <VirtualizedInfiniteList
        className="hide-scrollbar h-full min-h-0 w-full min-w-0 overflow-x-hidden overflow-y-auto"
        endAnchoring={{
          followEnabled: !newerAvailable,
          threshold: 80,
          scrollRequest: nativeRequest,
          onEndChange: setAtEnd,
        }}
        estimateSize={estimateSize}
        paddingEnd={bottomInset}
        rowSpacing="tight"
        getItemKey={presentationKey}
        getItemWrapperProps={(item) => itemWrapperProps(item, snapshot)}
        hasNextPage={newerPage.available}
        hasPreviousPage={olderPage.available}
        isFetchingNextPage={newerPage.fetching}
        isFetchingPreviousPage={olderPage.fetching}
        items={items}
        pinnedItemKeys={pinnedItemKeys}
        loadingLabel={loadingLabel}
        loadMoreKey={newerPage.key}
        nextBoundary={nextBoundary}
        onLoadMore={() => {
          visit("newer");
        }}
        onLoadPrevious={() => {
          visit("older");
        }}
        previousBoundary={previousBoundary}
        previousLoadKey={olderPage.key}
        renderItem={(item) => (
          <AnimatedReveal className={transcriptContentClassName} enter={entering.has(presentationKey(item))}>
            <ViewItemContent
              item={item}
              tailFailure={tailFailure}
              slots={slots}
              thinkingStatus={thinkingStatus}
            />
          </AnimatedReveal>
        )}
      />
      {overlay?.(following)}
    </div>
  );
}

function windowEdges(snapshot: TranscriptWindowSnapshot | null) {
  return {
    olderAvailable: snapshot !== null && snapshot.older.cursor !== null,
    newerAvailable: snapshot !== null && snapshot.newer.cursor !== null,
    older: snapshot?.older ?? null,
    newer: snapshot?.newer ?? null,
    opening: snapshot?.opening.kind ?? "local",
    thinkingStatus: snapshot?.thinkingStatus ?? null,
  } as const;
}

function useArrivals(
  items: readonly ViewItem[],
  presentationUpdate: TranscriptWindowViewProps["presentationUpdate"],
  localMessages: NonNullable<TranscriptWindowViewProps["localMessages"]>,
) {
  const [arrivals, setArrivals] = useState(() => ({
    update: presentationUpdate,
    localMessages,
    keys: new Set(items.map(presentationKey)),
    entering: new Set<string>(),
  }));
  let entering = arrivals.entering;
  if (arrivals.update !== presentationUpdate || arrivals.localMessages !== localMessages) {
    const keys = new Set(items.map(presentationKey));
    entering =
      presentationUpdate?.kind === "content" || arrivals.localMessages !== localMessages
        ? new Set([...keys].filter((key) => !arrivals.keys.has(key)))
        : new Set<string>();
    setArrivals({ update: presentationUpdate, localMessages, keys, entering });
  }
  return entering;
}

function itemWrapperProps(item: ViewItem, snapshot: TranscriptWindowSnapshot | null) {
  return {
    // display:none would restart character animations when returning to live output.
    style:
      item.kind === "assistant" && item.state === "live" && snapshot?.showsLive === false
        ? { height: 0, overflow: "hidden", padding: 0, visibility: "hidden" as const }
        : undefined,
    className:
      item.kind === "tool" || item.kind === "notice"
        ? "transcript-window-disclosure-row"
        : item.kind === "user" || item.kind === "assistant" || item.kind === "local-message"
          ? "transcript-window-message-row"
          : undefined,
  };
}

function ViewItemContent({
  item,
  tailFailure,
  slots,
  thinkingStatus,
}: Readonly<{
  item: ViewItem;
  tailFailure: ReactElement | undefined;
  slots: TranscriptWindowViewProps["slots"];
  thinkingStatus: TranscriptWindowSnapshot["thinkingStatus"];
}>) {
  switch (item.kind) {
    case "failure":
      return tailFailure;
    case "local-message":
      return item.content;
    case "status":
      return slots.thinkingStatus(thinkingStatus);
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

function transcriptPagination(boundary: TranscriptWindowSnapshot["older"] | null, suppressed: boolean) {
  return {
    available: !suppressed && boundary !== null && boundary.cursor !== null && boundary.kind !== "error",
    fetching: boundary?.kind === "loading",
    key: boundary?.cursor?.toString(),
  };
}

type TranscriptEndInput = Readonly<{
  opening: TranscriptWindowSnapshot["opening"]["kind"] | "local";
  update: ChatTranscriptPresentationUpdate | undefined;
  explicit: symbol | null | undefined;
  bottomInset: number;
  newerAvailable: boolean;
  localMessages: NonNullable<TranscriptWindowViewProps["localMessages"]>;
}>;

function sameLocalMessageMembership(
  previous: TranscriptEndInput["localMessages"],
  current: TranscriptEndInput["localMessages"],
): boolean {
  return (
    previous.length === current.length &&
    previous.every((message, index) => message.inputID === current[index]?.inputID)
  );
}

function transcriptViewItems(
  snapshot: TranscriptWindowSnapshot | null,
  failed: boolean,
  localMessages: NonNullable<TranscriptWindowViewProps["localMessages"]>,
): readonly ViewItem[] {
  const committedIDs = new Set(
    snapshot?.items.flatMap((item) => (item.kind === "user" ? item.value.QueueItemIDs : [])) ?? [],
  );
  const previews: readonly ViewItem[] =
    snapshot?.newer.cursor != null
      ? []
      : localMessages
          .filter((message) => !committedIDs.has(message.inputID))
          .map((message) => ({
            kind: "local-message",
            key: `input:${message.inputID}`,
            content: message.content,
          }));
  if (snapshot === null) return failed ? [...previews, failureItem] : previews;
  const items =
    !snapshot.showsLive
      ? [...snapshot.items, ...snapshot.activeAssistants]
      : snapshot.items;
  if (failed) return [...items, ...previews, failureItem];
  return snapshot.showsLive ? [...items, ...previews, statusItem] : [...items, ...previews];
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
    previous.bottomInset === input.bottomInset &&
    previous.newerAvailable === input.newerAvailable &&
    sameLocalMessageMembership(previous.localMessages, input.localMessages)
  ) {
    return request;
  }
  setPrevious(input);
  const behavior = transcriptEndBehavior(previous, input, following && !previous.newerAvailable);
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
  if (following && !sameLocalMessageMembership(previous.localMessages, current.localMessages)) return "auto";
  if (current.explicit != null && previous.explicit !== current.explicit) return "smooth";
  if (!following || previous.update === current.update || current.update === undefined) return null;
  const behavior: Record<ChatTranscriptPresentationUpdate["kind"], "auto" | "smooth" | null> = {
    replacement: "auto",
    content: "smooth",
    membership: null,
  };
  return behavior[current.update.kind];
}

function transcriptBoundary(
  boundary: TranscriptWindowSnapshot[TranscriptDirection] | null,
  direction: TranscriptDirection,
  copy: Readonly<{
    loadingLabel: string;
    retryLabel: string;
    errorMessage: (error: Error) => string;
    suppressed: boolean;
  }>,
  onInput: (input: TranscriptWindowViewInput) => void,
): VirtualizedInfiniteListBoundaryState | undefined {
  if (copy.suppressed || boundary === null) return undefined;
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
