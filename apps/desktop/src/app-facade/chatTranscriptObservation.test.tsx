import { expect, it } from "bun:test";
import { act, renderHook, waitFor } from "@testing-library/react";
import { RegistryProvider, useAtomSuspense } from "@effect/atom-react";
import * as Atom from "effect/reactivity/Atom";
import type { ReactNode } from "react";
import * as Effect from "effect/Effect";
import * as Stream from "effect/Stream";
import { ContractError, type ChatTranscriptMessage } from "@/api";
import { hydration, runtimeApi, target } from "@/test-support/chat-runtime";
import { row } from "@/test-support/transcript-window";
import { observeChatTranscript, type ChatTranscriptDelivery } from "./chatTranscriptObservation";

async function observe() {
  const fixture = runtimeApi();
  const deliveries: ChatTranscriptDelivery[] = [];
  const errors: Error[] = [];
  const observation = Atom.make(
    observeChatTranscript(fixture.api, target, "initial").pipe(
      Stream.runForEach((delivery) =>
        Effect.sync(() => {
          deliveries.push(delivery);
        }),
      ),
      Effect.catch((failure) =>
        Effect.sync(() => {
          errors.push(failure.error);
        }),
      ),
      Effect.as(null),
    ),
    { initialValue: null },
  );
  const view = renderHook(() => useAtomSuspense(observation), {
    wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider>,
  });
  return {
    ...fixture,
    deliveries,
    errors,
    close: () => {
      view.unmount();
    },
  };
}

const initial = (sequence = 1): ChatTranscriptMessage => ({
  kind: "hydration",
  sequence,
  payload: { ...hydration(), TailSegment: { Entries: [row(10)], OlderCursor: null, HasMoreAbove: false } },
});
const unavailable: ChatTranscriptMessage = {
  sequence: 2,
  kind: "runtime_read_model_update",
  payload: {
    Version: { Epoch: "epoch", Generation: 1, Sequence: 2 },
    Activity: {
      State: "unavailable",
      ActiveStep: null,
      Reviewer: "inactive",
      QueueAccepting: false,
      DiagnosticRecovery: false,
    },
  },
};

it("continues silently after normal Runtime stop, resetting hydration and row progression", async () => {
  const view = await observe();
  await act(async () => {
    view.handlers[0]?.onEvent(initial());
    view.handlers[0]?.onEvent(unavailable);
    view.handlers[0]?.onComplete({ code: null, message: null, reason: null });
  });
  await waitFor(() => {
    expect(view.handlers).toHaveLength(2);
  });
  expect(view.errors).toEqual([]);
  await act(async () => {
    view.handlers[1]?.onEvent(initial());
    view.handlers[1]?.onEvent({ kind: "committed_row", sequence: 2, payload: row(11) });
  });
  await waitFor(() => {
    expect(view.deliveries).toHaveLength(4);
  });
  expect(
    view.deliveries.filter((value) => value.kind === "hydration").map((value) => value.hydrationKind),
  ).toEqual(["initial", "reattachment"]);
  expect(view.errors).toEqual([]);
  view.close();
  await act(async () => view.handlers[1]?.onEvent(unavailable));
  expect(view.deliveries).toHaveLength(4);
});

it.each([
  { name: "first-row ordinal gap", locators: [[11, 2]] },
  {
    name: "repeated locator",
    locators: [
      [11, 1],
      [11, 1],
    ],
  },
  {
    name: "same-event ordinal gap",
    locators: [
      [11, 1],
      [11, 3],
    ],
  },
  {
    name: "event regression",
    locators: [
      [11, 1],
      [10, 1],
    ],
  },
  { name: "hydration overlap", locators: [[10, 2]] },
])("ends $name without reopening", async ({ locators }) => {
  const view = await observe();
  await act(async () => {
    view.handlers[0]?.onEvent(initial());
    locators.forEach(([eventSequence = 1, rowOrdinal = 1], index) =>
      view.handlers[0]?.onEvent({
        sequence: index + 2,
        kind: "committed_row",
        payload: {
          ...row(eventSequence),
          Locator: { event_sequence: eventSequence, row_ordinal: rowOrdinal },
        },
      }),
    );
  });
  await waitFor(() => {
    expect(view.errors).toHaveLength(1);
  });
  expect(view.errors[0]).toBeInstanceOf(ContractError);
  expect(view.handlers).toHaveLength(1);
  view.close();
});

it.each(["gap", "repeated-hydration", "completion", "transport", "malformed"] as const)(
  "stops %s locally and ignores later deliveries",
  async (failure) => {
    const view = await observe();
    await act(async () => {
      const handler = view.handlers[0];
      if (handler === undefined) throw new Error("Observation did not mount");
      handler.onEvent(initial());
      switch (failure) {
        case "gap":
          handler.onEvent({ ...unavailable, sequence: 3 });
          break;
        case "repeated-hydration":
          handler.onEvent(initial(2));
          break;
        case "completion":
          handler.onComplete({ code: null, message: null, reason: null });
          break;
        case "transport":
          handler.onTransportLoss?.();
          break;
        case "malformed":
          handler.onError(new ContractError("Malformed event"));
          break;
      }
    });
    await waitFor(() => {
      expect(view.errors).toHaveLength(1);
    });
    await act(async () => {
      view.handlers[0]?.onEvent(unavailable);
      view.handlers[0]?.onComplete({ code: null, message: null, reason: null });
    });
    expect(view.handlers).toHaveLength(1);
    expect(view.deliveries).toHaveLength(1);
    view.close();
  },
);
