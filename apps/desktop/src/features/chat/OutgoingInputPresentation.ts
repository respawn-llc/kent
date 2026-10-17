import * as Atom from "effect/reactivity/Atom";
import * as Effect from "effect/Effect";
import type { PendingWorkItem, PendingWorkItemID, ChatInputMutationResult } from "@/api";
import type { ComposerDraftViewModel } from "./ComposerDraftViewModel";
import { mergeComposerText } from "./composerText";

type Disposition = Extract<ChatInputMutationResult["outcome"], { kind: "accepted" }>["disposition"];
export type OutgoingInput = Readonly<{
  inputID: PendingWorkItemID;
  text: string;
  intent: "send" | "queue";
  placement: "held" | "bubble" | "pending";
  confirmation: "awaiting" | "accepted";
  disposition: Disposition | null;
}>;
export type OutgoingFacts = Readonly<{
  committedIDs: readonly PendingWorkItemID[];
  pendingItems: readonly PendingWorkItem[];
  pendingFetching: boolean;
}>;
type Record = OutgoingInput &
  Readonly<{
    completion: "unresolved" | "resolved" | "restoring";
    authority: "pending" | "committed" | null;
  }>;
type State = Readonly<{
  records: ReadonlyMap<string, Record>;
  facts: Omit<OutgoingFacts, "committedIDs">;
}>;
type Event =
  | Readonly<{
      kind: "prepare";
      inputID: PendingWorkItemID;
      text: string;
      intent: "send" | "queue";
      optimistic: boolean;
    }>
  | Readonly<{ kind: "release"; inputID: PendingWorkItemID }>
  | Readonly<{
      kind: "resolve";
      inputID: PendingWorkItemID;
      outcome: ChatInputMutationResult["outcome"] | null;
    }>
  | Readonly<{ kind: "observe"; facts: OutgoingFacts }>;
const initialState: State = {
  records: new Map(),
  facts: { pendingItems: [], pendingFetching: false },
};

type DraftChanges = (Readonly<{ kind: "clear" }> | Readonly<{ kind: "append"; text: string }>)[];

function observeRecords(
  records: Map<string, Record>,
  event: Extract<Event, { kind: "observe" }>,
  draft: DraftChanges,
) {
  const facts = event.facts;
  const committed = new Set(event.facts.committedIDs.map((id) => id.toJSONValue()));
  const pending = new Set(facts.pendingItems.map((item) => item.id.toJSONValue()));
  for (const [id, record] of records) {
    const authority =
      record.authority === "committed" || committed.has(id)
        ? "committed"
        : pending.has(id)
          ? "pending"
          : record.authority;
    if (authority !== null) {
      if (record.placement === "held") draft.push({ kind: "clear" });
      records.set(id, {
        ...record,
        authority,
        confirmation: "accepted",
        completion: record.completion === "restoring" ? "resolved" : record.completion,
        placement: record.placement === "held" ? "pending" : record.placement,
      });
    }
  }
}

function prepareRecord(
  records: Map<string, Record>,
  event: Extract<Event, { kind: "prepare" }>,
  draft: DraftChanges,
) {
  const id = event.inputID.toJSONValue();
  if (records.has(id) || records.size >= 100) return false;
  const optimistic = event.intent === "send" && event.optimistic;
  records.set(id, {
    inputID: event.inputID,
    text: event.text,
    intent: event.intent,
    placement: optimistic ? "bubble" : "held",
    confirmation: "awaiting",
    disposition: null,
    completion: "unresolved",
    authority: null,
  });
  if (optimistic) draft.push({ kind: "clear" });
  return true;
}

function resolveRecord(
  records: Map<string, Record>,
  event: Extract<Event, { kind: "resolve" }>,
  record: Record,
  draft: DraftChanges,
) {
  const id = event.inputID.toJSONValue();
  if (record.completion !== "unresolved") return;
  if (event.outcome?.kind === "accepted") {
    if (record.placement === "held") draft.push({ kind: "clear" });
    records.set(id, {
      ...record,
      completion: "resolved",
      disposition: event.outcome.disposition,
      confirmation: "accepted",
      placement:
        record.intent === "send" && event.outcome.disposition === "started_turn" ? "bubble" : "pending",
    });
  } else if (record.authority !== null) {
    records.set(id, { ...record, completion: "resolved" });
  } else if (record.placement === "held") records.delete(id);
  else records.set(id, { ...record, completion: "restoring" });
}

function transitionRecord(
  records: Map<string, Record>,
  event: Extract<Event, { kind: "release" | "resolve" }>,
  draft: DraftChanges,
) {
  const id = event.inputID.toJSONValue();
  const record = records.get(id);
  if (record === undefined) return;
  if (event.kind === "resolve") {
    resolveRecord(records, event, record, draft);
    return;
  }
  if (record.placement !== "held") return;
  draft.push({ kind: "clear" });
  records.set(id, { ...record, placement: "pending" });
}

function settleRecords(records: Map<string, Record>, facts: State["facts"], draft: DraftChanges) {
  // An older rejection must not edit a newer held source that is about to be consumed.
  if (![...records.values()].some((record) => record.placement === "held")) {
    for (const [id, record] of records) {
      if (record.completion === "restoring") {
        draft.push({ kind: "append", text: record.text });
        records.delete(id);
      }
    }
  }
  const pending = new Set(facts.pendingItems.map((item) => item.id.toJSONValue()));
  for (const [id, record] of records) {
    if (
      record.authority !== null &&
      record.completion === "resolved" &&
      !facts.pendingFetching &&
      !pending.has(id)
    )
      records.delete(id);
  }
}

function reduce(state: State, event: Event) {
  const records = new Map(state.records);
  const facts =
    event.kind === "observe"
      ? { pendingItems: event.facts.pendingItems, pendingFetching: event.facts.pendingFetching }
      : state.facts;
  const draft: DraftChanges = [];
  let admitted = false;
  switch (event.kind) {
    case "observe":
      observeRecords(records, event, draft);
      break;
    case "prepare":
      admitted = prepareRecord(records, event, draft);
      break;
    case "release":
    case "resolve":
      transitionRecord(records, event, draft);
  }
  settleRecords(records, facts, draft);
  return { state: { records, facts }, draft, admitted };
}

export function createOutgoingInputPresentation(draft: ComposerDraftViewModel) {
  const state = Atom.make(initialState);
  const apply = Atom.fn<Event>()(
    (event, get) =>
      Effect.gen(function* () {
        const transition = reduce(get(state), event);
        get.set(state, transition.state);
        const restorations = transition.draft.flatMap((change) =>
          change.kind === "append" ? [change.text] : [],
        );
        if (transition.draft.some((change) => change.kind === "clear")) get.set(draft.submit, restorations);
        else if (restorations.length > 0)
          yield* get.setResult(draft.restore, {
            text: restorations.reduce(
              (current, incoming) => mergeComposerText(current, incoming, "append"),
              "",
            ),
            direction: "append",
          });
        return transition.admitted;
      }),
    { concurrent: true },
  );
  const items = Atom.make((get): readonly OutgoingInput[] => {
    const current = get(state);
    const pending = new Set(current.facts.pendingItems.map((item) => item.id.toJSONValue()));
    return [...current.records.values()].filter(
      (record) =>
        record.completion !== "restoring" &&
        record.authority !== "committed" &&
        !pending.has(record.inputID.toJSONValue()),
    );
  });
  const heldReadonly = Atom.make((get) =>
    [...get(state).records.values()].some((record) => record.placement === "held"),
  );
  const canPrepare = Atom.make((get) => get(state).records.size < 100);
  const completion = Atom.make((get) => {
    // Query callbacks outlive individual concurrent action contexts, not this destination.
    get.mount(state);
    get.mount(apply);
    return {
      resolve(inputID: PendingWorkItemID, outcome: ChatInputMutationResult["outcome"] | null) {
        const confirmed = get.once(state).records.get(inputID.toJSONValue())?.authority != null;
        get.set(apply, { kind: "resolve", inputID, outcome });
        return confirmed;
      },
      release(inputID: PendingWorkItemID) {
        get.set(apply, { kind: "release", inputID });
      },
    };
  });
  const pendingItems = Atom.make((get) => {
    const current = get(state);
    return current.facts.pendingItems.filter(
      (item) => current.records.get(item.id.toJSONValue())?.authority !== "committed",
    );
  });
  const observe = Atom.fn<OutgoingFacts>()((facts, get) => get.setResult(apply, { kind: "observe", facts }), {
    concurrent: true,
  });
  const observeCommits = Atom.fn<readonly PendingWorkItemID[]>()(
    (committedIDs, get) =>
      get.setResult(apply, { kind: "observe", facts: { ...get(state).facts, committedIDs } }),
    { concurrent: true },
  );
  return {
    items,
    heldReadonly,
    canPrepare,
    pendingItems,
    completion,
    apply,
    observe,
    observeCommits,
  } as const;
}
export type OutgoingInputPresentation = ReturnType<typeof createOutgoingInputPresentation>;
