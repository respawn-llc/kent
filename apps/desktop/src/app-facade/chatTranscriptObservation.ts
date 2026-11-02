import * as Cause from "effect/Cause";
import * as Effect from "effect/Effect";
import * as Stream from "effect/Stream";

import type { ChatApi, ChatSessionTarget, ChatTranscriptMessage, ChatTranscriptPayloadByKind } from "@/api";
import { ContractError } from "@/api";
import { ChatTranscriptFailure } from "./chatTranscriptFailure";

export type ChatTranscriptHydrationKind = "initial" | "scratch" | "reattachment";
export type ChatTranscriptObservationState =
  Readonly<{ kind: "loading" | "observing" }> | Readonly<{ kind: "error"; error: Error }>;
export type ChatTranscriptDelivery =
  | Readonly<{
      kind: "hydration";
      hydrationKind: ChatTranscriptHydrationKind;
      hydration: ChatTranscriptPayloadByKind["hydration"];
    }>
  | Readonly<{ kind: "event"; event: Exclude<ChatTranscriptMessage, { kind: "hydration" }> }>;

type CommittedRowLocator = Readonly<{ eventSequence: number; rowOrdinal: number }>;

export function observeChatTranscript(
  api: Pick<ChatApi, "subscribeTranscript">,
  target: ChatSessionTarget,
  initialKind: ChatTranscriptHydrationKind,
): Stream.Stream<ChatTranscriptDelivery, ChatTranscriptFailure> {
  return Stream.unwrap(
    Effect.sync(() => {
      let hydrationKind = initialKind;
      return Stream.unwrap(
        Effect.sync(() => {
          let sequence: number | null = null;
          let hydratedCommittedEventSequence: number | null = null;
          let lastLiveCommittedLocator: CommittedRowLocator | null = null;
          let terminalUnavailable = false;
          return api
            .subscribeTranscript(target, async () => {
              throw new ContractError("Transcript observation exceeded its 1,000-event capacity.");
            })
            .pipe(
              Stream.takeUntil((observation) => observation.kind === "complete"),
              Stream.flatMap((observation): Stream.Stream<ChatTranscriptDelivery, ChatTranscriptFailure> => {
                if (observation.kind === "open") return Stream.empty;
                if (observation.kind === "error")
                  return Stream.fail(new ChatTranscriptFailure(observation.error));
                if (observation.kind === "complete") {
                  if (observation.completion.code === null && terminalUnavailable) {
                    hydrationKind = "reattachment";
                    return Stream.empty;
                  }
                  return Stream.fail(
                    new ChatTranscriptFailure(
                      new ContractError(
                        `Transcript observation completed with code ${String(observation.completion.code)}: ${String(observation.completion.message)}`,
                      ),
                    ),
                  );
                }
                return Stream.fromEffect(
                  Effect.try({
                    try: (): ChatTranscriptDelivery => {
                      const event = observation.event;
                      if (sequence === null) {
                        if (event.kind !== "hydration" || event.sequence !== 1)
                          throw new ContractError(
                            "Transcript observation must begin with sequence-1 hydration.",
                          );
                        hydratedCommittedEventSequence =
                          event.payload.TailSegment.Entries.at(-1)?.Locator.event_sequence ?? null;
                        sequence = 1;
                        return { kind: "hydration", hydrationKind, hydration: event.payload };
                      }
                      if (event.kind === "hydration" || event.sequence !== sequence + 1)
                        throw new ContractError("Transcript observation sequence is not continuous.");
                      if (event.kind === "committed_row") {
                        lastLiveCommittedLocator = advanceCommittedLocator(
                          event.payload,
                          lastLiveCommittedLocator,
                          hydratedCommittedEventSequence,
                        );
                      }
                      sequence = event.sequence;
                      terminalUnavailable ||=
                        event.kind === "runtime_read_model_update" &&
                        event.payload.Activity.State === "unavailable";
                      return { kind: "event", event };
                    },
                    catch: (error) => new ChatTranscriptFailure(error),
                  }),
                );
              }),
            );
        }),
      ).pipe(
        // Only successful normal Runtime stop completes this stream. Failures never continue it.
        Stream.forever,
        Stream.catchCause((cause) => {
          const error = Cause.squash(cause);
          return error instanceof ContractError
            ? Stream.fail(new ChatTranscriptFailure(error))
            : Stream.failCause(cause);
        }),
      );
    }),
  );
}

function advanceCommittedLocator(
  row: ChatTranscriptPayloadByKind["committed_row"],
  previous: CommittedRowLocator | null,
  hydratedEventSequence: number | null,
): CommittedRowLocator | null {
  const next = {
    eventSequence: row.Locator.event_sequence,
    rowOrdinal: row.Locator.row_ordinal,
  };
  const atOrBeforeFrontier =
    previous === null
      ? hydratedEventSequence !== null && next.eventSequence <= hydratedEventSequence
      : next.eventSequence < previous.eventSequence ||
        (next.eventSequence === previous.eventSequence && next.rowOrdinal <= previous.rowOrdinal);
  // Native tool completion replaces its call row; the window validates that
  // replacement without moving this observation's append frontier backwards.
  if (atOrBeforeFrontier && row.Kind === "tool" && row.Tool?.ToolCallID != null) return previous;
  validateCommittedLocator(next, previous, hydratedEventSequence);
  return next;
}

function validateCommittedLocator(
  next: CommittedRowLocator,
  previous: CommittedRowLocator | null,
  hydratedEventSequence: number | null,
) {
  if (previous === null) {
    if (
      (hydratedEventSequence !== null && next.eventSequence <= hydratedEventSequence) ||
      next.rowOrdinal !== 1
    )
      throw new ContractError("Live committed transcript rows must advance beyond hydration.");
  } else if (
    next.eventSequence < previous.eventSequence ||
    (next.eventSequence === previous.eventSequence && next.rowOrdinal !== previous.rowOrdinal + 1) ||
    (next.eventSequence > previous.eventSequence && next.rowOrdinal !== 1)
  ) {
    throw new ContractError("Live committed transcript row locators are not continuous.");
  }
}
