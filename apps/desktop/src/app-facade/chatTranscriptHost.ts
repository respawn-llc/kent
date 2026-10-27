import * as Atom from "effect/reactivity/Atom";
import * as Effect from "effect/Effect";

import type {
  ChatApi,
  ChatSessionTarget,
  ChatTranscriptMessage,
  ChatTranscriptPage,
  ChatTranscriptPayloadByKind,
} from "@/api";
import { ContractError } from "@/api";
import { ChatTranscriptFailure } from "./chatTranscriptFailure";
import {
  TranscriptWindow,
  type TranscriptWindowInput,
  type TranscriptWindowResult,
  type TranscriptLiveFact,
} from "./transcript-window";

export type ChatTranscriptPageInput =
  Readonly<{ kind: "newest" }> | Readonly<{ kind: "older" | "newer"; cursor: number }>;
export type ChatTranscriptHostInput = Extract<
  TranscriptWindowInput,
  { kind: "edge-visit" | "opening-retry" | "retry" }
>;
export type ChatTranscriptPresentationUpdate = Readonly<{ kind: "content" | "membership" | "replacement" }>;

type TranscriptPageApi = Pick<ChatApi, "getTranscriptPage">;
type PageRequest =
  | Readonly<{ kind: "opening"; permit: symbol }>
  | (Extract<TranscriptWindowResult, { kind: "accepted" }>["effects"][number] & { kind: "page-request" });

function isLiveFact(event: ChatTranscriptMessage): event is ChatTranscriptMessage & TranscriptLiveFact {
  return (
    event.kind === "assistant_delta" ||
    event.kind === "assistant_stream_abort" ||
    event.kind === "assistant_source_complete" ||
    event.kind === "tool_start" ||
    event.kind === "tool_abort" ||
    event.kind === "reasoning_trace_update" ||
    event.kind === "reasoning_trace_reset" ||
    event.kind === "thinking_status_update" ||
    event.kind === "step_state"
  );
}

export async function executeChatTranscriptPage(
  api: TranscriptPageApi,
  target: ChatSessionTarget,
  input: ChatTranscriptPageInput,
): Promise<ChatTranscriptPage> {
  switch (input.kind) {
    case "newest":
      return api.getTranscriptPage(target);
    case "older":
    case "newer":
      return api.getTranscriptPage(target, { direction: input.kind, value: input.cursor });
  }
}

export function createChatTranscriptModel(
  api: TranscriptPageApi,
  target: ChatSessionTarget,
  onOpeningFailure: (error: Error) => void,
) {
  const window = Atom.make(() => new TranscriptWindow());
  const presentation = Atom.make<ChatTranscriptPresentationUpdate>({ kind: "membership" });
  const snapshot = Atom.make((get) => get(window).snapshot);
  const jumpRequest = Atom.make<Readonly<{ onReady(): void; onError(error: Error): void }> | null>(null);
  const jumpPending = Atom.make((get) => get(jumpRequest) !== null);

  const readPage = (input: ChatTranscriptPageInput) =>
    Effect.tryPromise({
      try: async () => executeChatTranscriptPage(api, target, input),
      catch: (error) => new ChatTranscriptFailure(error),
    });

  type Apply = (
    input: TranscriptWindowInput,
    presentationKind?: ChatTranscriptPresentationUpdate["kind"],
    admitted?: () => void,
  ) => Effect.Effect<void, ChatTranscriptFailure>;

  const scope = Atom.make((get) => {
    const transcript = get(window);
    get.mount(jumpRequest);
    get.mount(presentation);
    get.mount(snapshot);
    const finishJump = () => {
      const pending = get.once(jumpRequest);
      get.set(jumpRequest, null);
      pending?.onReady();
    };
    const apply: Apply = Effect.fnUntraced(function* (input, presentationKind = "membership", admitted) {
      const result = transcript.dispatch(input);
      if (result.kind === "contract-failure")
        return yield* Effect.fail(new ChatTranscriptFailure(result.error));
      if (result.kind === "accepted") get.set(presentation, { kind: presentationKind });
      get.refresh(snapshot);
      admitted?.();
      for (const effect of result.effects) {
        switch (effect.kind) {
          case "hydration-replaced":
            get.set(presentation, { kind: "replacement" });
            finishJump();
            break;
          case "opening-failed":
            onOpeningFailure(effect.error);
            break;
          case "opening-page-request":
            get.set(requestPage, { kind: "opening", permit: effect.permit });
            break;
          case "page-request":
            get.set(requestPage, effect);
            break;
          case "scratch-rehydration":
            get.set(rehydrate, undefined);
            break;
        }
      }
    });
    get.addFinalizer(() => {
      transcript.dispatch({ kind: "dispose" });
    });
    const ended = () => transcript.snapshot.opening.kind === "disposed";
    const jump = Effect.fnUntraced(function* (input: NonNullable<Atom.Type<typeof jumpRequest>>) {
      if (ended() || get.once(jumpPending)) return;
      if (transcript.snapshot.newer.cursor === null) {
        input.onReady();
        return;
      }
      const request = { ...input };
      get.set(jumpRequest, request);
      const response = yield* readPage({ kind: "newest" }).pipe(Effect.result);
      if (ended() || get.once(jumpRequest) !== request) return;
      get.set(jumpRequest, null);
      if (response._tag === "Failure") {
        if (response.failure.error instanceof ContractError) return yield* Effect.fail(response.failure);
        input.onError(response.failure.error);
        return;
      }
      yield* apply({ kind: "replace-window", page: response.success });
      input.onReady();
    });
    return { transcript, apply, jump };
  });
  // Runtime installs the destination's failure and replacement actions once.
  const failure = Atom.make<((error: Error) => Effect.Effect<void>) | null>(null);
  const replacement = Atom.make<(() => void) | null>(null);
  const rehydrate = Atom.fn((_, get) => Effect.sync(() => get(replacement)?.()), { concurrent: true });
  const handleFailure = (get: Atom.FnContext, failureCause: ChatTranscriptFailure) => {
    const error = failureCause.error;
    const handler = get(failure);
    return handler === null ? Effect.die(error) : handler(error);
  };
  const requestPage = Atom.fn<PageRequest>()(
    (request, get) =>
      Effect.gen(function* () {
        const { apply } = get(scope);
        const opening = request.kind === "opening";
        const response = yield* readPage(
          opening ? { kind: "newest" } : { kind: request.request.direction, cursor: request.request.cursor },
        ).pipe(Effect.result);
        if (response._tag === "Success") {
          yield* apply(
            opening
              ? { kind: "opening-success", permit: request.permit, page: response.success }
              : { kind: "page-success", request: request.request, page: response.success },
          );
        } else {
          yield* apply(
            opening
              ? { kind: "opening-failure", permit: request.permit, error: response.failure.error }
              : { kind: "page-failure", request: request.request, error: response.failure.error },
          );
        }
      }).pipe(Effect.catch((error) => handleFailure(get, error))),
    { concurrent: true },
  );
  const dispatch = Atom.fn<ChatTranscriptHostInput>()(
    (input, get) =>
      get(scope)
        .apply(input)
        .pipe(Effect.catch((error) => handleFailure(get, error))),
    { concurrent: true },
  );
  const jump = Atom.fn<NonNullable<Atom.Type<typeof jumpRequest>>>()(
    (input, get) =>
      get(scope)
        .jump(input)
        .pipe(Effect.catch((error) => handleFailure(get, error))),
    { concurrent: true },
  );
  const mount = Atom.make((get) => {
    get.mount(scope);
    get.mount(requestPage);
    get.mount(dispatch);
    get.mount(jump);
    get.set(requestPage, { kind: "opening", permit: get(scope).transcript.openingPermit });
    return null;
  });
  const outcomes = Atom.make((get) => [get(requestPage), get(dispatch), get(jump)] as const);
  const receive = Effect.fnUntraced(function* (
    get: Atom.AtomContext,
    kind: "initial" | "scratch" | "reattachment",
    hydration: ChatTranscriptPayloadByKind["hydration"],
  ) {
    yield* get.once(scope).apply({ kind: `${kind}-hydration`, hydration }, "content");
  });
  const event = Effect.fnUntraced(function* (
    get: Atom.AtomContext,
    event: Exclude<ChatTranscriptMessage, { kind: "hydration" }>,
    admitted: () => void,
  ) {
    const { apply } = get.once(scope);
    if (event.kind === "committed_row")
      return yield* apply({ kind: "committed-row", row: event.payload }, "content", admitted);
    if (event.kind === "runtime_read_model_update")
      return yield* apply(
        { kind: "runtime-activity", activity: event.payload.Activity },
        "content",
        admitted,
      );
    if (event.kind === "compaction_status")
      return yield* apply({ kind: "compaction-status", status: event.payload }, "content", admitted);
    if (isLiveFact(event)) return yield* apply({ kind: "live-fact", fact: event }, "content", admitted);
    admitted();
  });
  return {
    snapshot,
    presentation,
    jumpPending,
    dispatch,
    jump,
    mount,
    outcomes,
    scope,
    failure,
    replacement,
    receive,
    event,
  } as const;
}
