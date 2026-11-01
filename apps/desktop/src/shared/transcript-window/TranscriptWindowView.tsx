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
  TranscriptReasoningDisplayGroup,
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
  | Exclude<TranscriptRenderItem, { kind: "reasoning_trace" }>
  | TranscriptReasoningDisplayGroup
  | Readonly<{ kind: "local-message"; key: string; content: ReactElement }>
  | Readonly<{ kind: "status"; key: "thinking-status-tail" }>
  | Readonly<{ kind: "failure"; key: "transcript-failure-tail" }>;
type TranscriptSourceItem =
  | TranscriptRenderItem
  | Readonly<{ kind: "local-message"; key: string; content: ReactElement }>
  | Readonly<{ kind: "status"; key: "thinking-status-tail" }>
  | Readonly<{ kind: "failure"; key: "transcript-failure-tail" }>;
type ReasoningGroupIdentity = Readonly<{ key: string; memberKeys: readonly string[] }>;
type ReasoningTraceItem = TranscriptReasoningDisplayGroup["members"][number];
type ReasoningMembers = Readonly<{
  kind: "reasoning_members";
  members: readonly [ReasoningTraceItem, ...ReasoningTraceItem[]];
}>;
type GroupableTranscriptItem = Exclude<TranscriptSourceItem, { kind: "reasoning_trace" }> | ReasoningMembers;
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
  const sourceItems = transcriptViewItems(snapshot, tailFailure !== undefined, localMessages);
  const items = useReasoningDisplayGroups(sourceItems);
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
    { retryLabel, errorMessage: boundaryErrorMessage, suppressed: tailFailure !== undefined },
    emitInput,
  );
  const nextBoundary = transcriptBoundary(
    newer,
    "newer",
    { retryLabel, errorMessage: boundaryErrorMessage, suppressed: tailFailure !== undefined },
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
      item.kind === "tool" || item.kind === "tool_call" || item.kind === "notice"
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
    case "tool_call":
      return slots.tool(item);
    case "reasoning_group":
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
): readonly TranscriptSourceItem[] {
  const committedIDs = new Set(
    snapshot?.items.flatMap((item) => (item.kind === "user" ? item.value.QueueItemIDs : [])) ?? [],
  );
  const previews: readonly TranscriptSourceItem[] =
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
  const items = !snapshot.showsLive ? [...snapshot.items, ...snapshot.activeAssistants] : snapshot.items;
  if (failed) return [...items, ...previews, failureItem];
  return snapshot.showsLive ? [...items, ...previews, statusItem] : [...items, ...previews];
}

function useReasoningDisplayGroups(items: readonly TranscriptSourceItem[]): readonly ViewItem[] {
  const [previousGroups, setPreviousGroups] = useState<readonly ReasoningGroupIdentity[]>([]);
  const projection = projectReasoningDisplayGroups(items, previousGroups);
  if (!sameReasoningGroupIdentities(previousGroups, projection.groups)) {
    setPreviousGroups(projection.groups);
  }
  return projection.items;
}

function projectReasoningDisplayGroups(
  items: readonly TranscriptSourceItem[],
  previousGroups: readonly ReasoningGroupIdentity[],
): Readonly<{ items: readonly ViewItem[]; groups: readonly ReasoningGroupIdentity[] }> {
  const previousGroupByMember = indexPreviousGroupMembers(previousGroups);
  const groupedItems = groupAdjacentReasoningMembers(items);
  const currentGroupByMember = indexCurrentReasoningGroups(groupedItems);
  const previousGroupsByCurrent = indexPreviousGroupOwners(previousGroups, currentGroupByMember);
  const groups: ReasoningGroupIdentity[] = [];
  const projected: ViewItem[] = [];

  groupedItems.forEach((item, index) => {
    if (item.kind !== "reasoning_members") {
      projected.push(item);
      return;
    }

    const { members } = item;
    const continued = continuingReasoningGroup(
      members,
      previousGroupByMember,
      previousGroupsByCurrent,
      index,
    );
    const key = continued?.key ?? members[0].key;
    groups.push({ key, memberKeys: members.map((member) => member.key) });
    projected.push({ kind: "reasoning_group", key, members });
  });

  return { items: projected, groups };
}

function groupAdjacentReasoningMembers(
  items: readonly TranscriptSourceItem[],
): readonly GroupableTranscriptItem[] {
  const grouped: GroupableTranscriptItem[] = [];
  for (let index = 0; index < items.length;) {
    const item = items[index];
    if (item === undefined) throw new Error("Transcript view item disappeared during grouping.");
    if (item.kind !== "reasoning_trace") {
      grouped.push(item);
      index += 1;
      continue;
    }
    const members: [ReasoningTraceItem, ...ReasoningTraceItem[]] = [item];
    let nextIndex = index + 1;
    while (nextIndex < items.length) {
      const next = items[nextIndex];
      if (next === undefined) throw new Error("Transcript view item disappeared during grouping.");
      if (next.kind !== "reasoning_trace" || next.value.StepID !== item.value.StepID) break;
      members.push(next);
      nextIndex += 1;
    }
    grouped.push({ kind: "reasoning_members", members });
    index = nextIndex;
  }
  return grouped;
}

function indexPreviousGroupMembers(
  groups: readonly ReasoningGroupIdentity[],
): ReadonlyMap<string, ReasoningGroupIdentity> {
  const indexed = new Map<string, ReasoningGroupIdentity>();
  for (const group of groups) {
    for (const key of group.memberKeys) indexed.set(key, group);
  }
  return indexed;
}

function indexCurrentReasoningGroups(items: readonly GroupableTranscriptItem[]): ReadonlyMap<string, number> {
  const indexed = new Map<string, number>();
  items.forEach((item, index) => {
    if (item.kind !== "reasoning_members") return;
    for (const member of item.members) indexed.set(member.key, index);
  });
  return indexed;
}

function indexPreviousGroupOwners(
  previousGroups: readonly ReasoningGroupIdentity[],
  currentGroupByMember: ReadonlyMap<string, number>,
): ReadonlyMap<number, readonly ReasoningGroupIdentity[]> {
  const owners = new Map<number, ReasoningGroupIdentity[]>();
  for (const group of previousGroups) {
    const owner =
      currentGroupByMember.get(group.key) ?? firstCurrentGroup(group.memberKeys, currentGroupByMember);
    if (owner === undefined) continue;
    const groups = owners.get(owner) ?? [];
    groups.push(group);
    owners.set(owner, groups);
  }
  return owners;
}

function firstCurrentGroup(
  memberKeys: readonly string[],
  currentGroupByMember: ReadonlyMap<string, number>,
): number | undefined {
  for (const key of memberKeys) {
    const currentGroup = currentGroupByMember.get(key);
    if (currentGroup !== undefined) return currentGroup;
  }
  return undefined;
}

function continuingReasoningGroup(
  members: TranscriptReasoningDisplayGroup["members"],
  previousGroupByMember: ReadonlyMap<string, ReasoningGroupIdentity>,
  previousGroupsByCurrent: ReadonlyMap<number, readonly ReasoningGroupIdentity[]>,
  currentGroupIndex: number,
): ReasoningGroupIdentity | undefined {
  const availablePreviousGroups = previousGroupsByCurrent.get(currentGroupIndex) ?? [];
  for (const member of members) {
    const previousGroup = previousGroupByMember.get(member.key);
    if (previousGroup !== undefined && availablePreviousGroups.includes(previousGroup)) return previousGroup;
  }
  return undefined;
}

function sameReasoningGroupIdentities(
  previous: readonly ReasoningGroupIdentity[],
  current: readonly ReasoningGroupIdentity[],
): boolean {
  if (previous.length !== current.length) return false;
  for (let index = 0; index < previous.length; index += 1) {
    const previousGroup = previous[index];
    const currentGroup = current[index];
    if (previousGroup === undefined || currentGroup === undefined) return false;
    if (
      previousGroup.key !== currentGroup.key ||
      previousGroup.memberKeys.length !== currentGroup.memberKeys.length
    ) {
      return false;
    }
    for (let memberIndex = 0; memberIndex < previousGroup.memberKeys.length; memberIndex += 1) {
      if (previousGroup.memberKeys[memberIndex] !== currentGroup.memberKeys[memberIndex]) return false;
    }
  }
  return true;
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
      return { state: "loading" };
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
