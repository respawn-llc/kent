import { QueryClient } from "@tanstack/react-query";
import { expect, it, vi } from "bun:test";
import { hydration, runtimeApi, runtimeHost, target } from "@/test-support/chat-runtime";
import { row } from "@/test-support/transcript-window";
import { ChatRuntimeOwner } from "./chatRuntime";

it("reports input commitment from hydration and live events without depending on row visibility", async () => {
  const fixture = runtimeApi();
  const inputID = "223e4567-e89b-42d3-a456-426614174000";
  const committed = { ...row(1), User: { Text: "sent", QueueItemIDs: [inputID] } };
  const committedInputs = vi.fn();
  const owner = new ChatRuntimeOwner(fixture.api, target, new QueryClient(), {
    ...runtimeHost(),
    onUserInputCommitted: committedInputs,
  });
  owner.start();
  fixture.handlers[0]?.onEvent({
    sequence: 1,
    kind: "hydration",
    payload: {
      ...hydration(),
      TailSegment: { Entries: [committed], OlderCursor: null, HasMoreAbove: false },
    },
  });
  expect(committedInputs).toHaveBeenCalledWith([inputID]);
  committedInputs.mockClear();
  fixture.handlers[0]?.onEvent({
    sequence: 2,
    kind: "committed_row",
    payload: { ...committed, Locator: { event_sequence: 2, row_ordinal: 1 } },
  });
  expect(committedInputs).toHaveBeenCalledWith([inputID]);
  await owner.dispose();
});
