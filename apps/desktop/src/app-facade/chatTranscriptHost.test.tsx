import { act, waitFor } from "@testing-library/react";
import { expect, it, vi } from "bun:test";
import { type ChatTranscriptPage } from "@/api";
import { QueryClient } from "@tanstack/react-query";
import { hydration, page, row } from "@/test-support/transcript-window";
import { deferred, runtimeApi, runtimeHost, RuntimeHarness, target } from "@/test-support/chat-runtime";

async function history() {
  const response = deferred<ChatTranscriptPage>();
  const fixture = runtimeApi({
    pages: [
      Promise.resolve(page([row(30)], 300)),
      Promise.resolve(page([row(20)], 200, 300)),
      Promise.resolve(page([row(10)], 100, 200)),
      response.promise,
    ],
  });
  const owner = new RuntimeHarness(fixture.api, target, new QueryClient(), runtimeHost());
  owner.start();
  await waitForSnapshot(owner, () => owner.snapshot.transcript.opening.kind === "ready");
  for (const cursor of [200, 100]) {
    owner.transcript.dispatch({ kind: "edge-visit", direction: "older", older: true, newer: false });
    await waitForSnapshot(
      owner,
      () =>
        owner.snapshot.transcript.older.kind === "idle" && owner.snapshot.transcript.older.cursor === cursor,
    );
  }
  const onReady = vi.fn();
  const onError = vi.fn();
  const jumpInput = { onReady, onError };
  const jump = () => {
    owner.transcript.jump(jumpInput);
  };
  return { owner, fixture, response, jump, onReady, onError };
}

it("fulfills a pending Jump from replacement hydration and ignores its delayed newest response", async () => {
  const view = await history();
  view.jump();
  expect(view.owner.snapshot.jumpPending).toBe(true);
  await act(async () =>
    view.fixture.handlers[0]?.onEvent({ sequence: 1, kind: "hydration", payload: hydration([row(50)], 1) }),
  );
  expect(view.onReady).toHaveBeenCalledTimes(1);
  expect(view.owner.snapshot.jumpPending).toBe(false);
  await act(async () => {
    view.response.resolve(page([row(40)], 400));
  });
  expect(
    view.owner.snapshot.transcript.items.flatMap((item) =>
      "row" in item ? [item.row.Locator.event_sequence] : [],
    ),
  ).toEqual([50]);
  expect(view.onError).not.toHaveBeenCalled();
  view.owner.dispose();
});

it("prevents same-turn duplicate Jumps and restores loaded history after failure", async () => {
  const view = await history();
  view.jump();
  view.jump();
  expect(view.fixture.getTranscriptPage).toHaveBeenCalledTimes(4);
  await act(async () => {
    view.response.reject(new Error("Unavailable"));
  });
  expect(view.owner.snapshot.jumpPending).toBe(false);
  expect(view.onError).toHaveBeenCalledTimes(1);
  expect(view.onReady).not.toHaveBeenCalled();
  expect(view.owner.snapshot.transcript.newer.cursor).not.toBeNull();
  view.fixture.getTranscriptPage.mockResolvedValueOnce(page([row(80)], 800));
  view.jump();
  await waitFor(() => {
    expect(view.onReady).toHaveBeenCalledTimes(1);
  });
  expect(view.owner.snapshot.jumpPending).toBe(false);
  view.owner.dispose();
});

it("starts another independent newest read after hydration fulfills a still-unresolved Jump", async () => {
  const view = await history();
  view.jump();
  await act(async () =>
    view.fixture.handlers[0]?.onEvent({
      kind: "hydration",
      sequence: 1,
      payload: {
        ...hydration([row(50)], 1),
        TailSegment: { Entries: [row(50)], OlderCursor: 500, HasMoreAbove: true },
      },
    }),
  );
  view.fixture.getTranscriptPage
    .mockResolvedValueOnce(page([row(40)], 400, 500))
    .mockResolvedValueOnce(page([row(30)], 300, 400))
    .mockResolvedValueOnce(page([row(80)], 800));
  for (const cursor of [400, 300]) {
    view.owner.transcript.dispatch({ kind: "edge-visit", direction: "older", older: true, newer: false });
    await waitFor(() => {
      expect(view.owner.snapshot.transcript.older).toEqual({ kind: "idle", cursor });
    });
  }
  view.jump();
  await waitFor(() => {
    expect(view.onReady).toHaveBeenCalledTimes(2);
  });
  expect(view.fixture.getTranscriptPage).toHaveBeenCalledTimes(7);
  await act(async () => {
    view.response.reject(new Error("Obsolete newest read"));
  });
  expect(view.onError).not.toHaveBeenCalled();
  expect(
    view.owner.snapshot.transcript.items.flatMap((item) =>
      "row" in item ? [item.row.Locator.event_sequence] : [],
    ),
  ).toEqual([80]);
  view.owner.dispose();
});

it("does not wait for an adjacent read and ignores that read after Jump replaces the window", async () => {
  const view = await history();
  view.owner.transcript.dispatch({ kind: "edge-visit", direction: "older", older: true, newer: false });
  view.fixture.getTranscriptPage.mockResolvedValueOnce(page([row(80)], 800));
  view.jump();
  await waitFor(() => {
    expect(view.onReady).toHaveBeenCalledTimes(1);
  });
  await act(async () => {
    view.response.resolve(page([row(5)], null, 100));
  });
  expect(
    view.owner.snapshot.transcript.items.flatMap((item) =>
      "row" in item ? [item.row.Locator.event_sequence] : [],
    ),
  ).toEqual([80]);
  view.owner.dispose();
});

it("ends local Jump feedback on departure", async () => {
  const view = await history();
  view.jump();
  await act(async () => {
    view.owner.dispose();
  });
  await act(async () => {
    view.response.resolve(page([row(50)], 1));
  });
  expect(view.onReady).not.toHaveBeenCalled();
  expect(view.onError).not.toHaveBeenCalled();
});

it("keeps a failed directional boundary retryable with its opaque cursor", async () => {
  const failed = deferred<ChatTranscriptPage>();
  const fixture = runtimeApi({
    pages: [
      Promise.resolve(page([row(30)], 300)),
      failed.promise,
      Promise.resolve(page([row(20)], 200, 300)),
    ],
  });
  const owner = new RuntimeHarness(fixture.api, target, new QueryClient(), runtimeHost());
  owner.start();
  await waitForSnapshot(owner, () => owner.snapshot.transcript.opening.kind === "ready");
  owner.transcript.dispatch({ kind: "edge-visit", direction: "older", older: true, newer: false });
  failed.reject(new Error("Unavailable"));
  await waitForSnapshot(owner, () => owner.snapshot.transcript.older.kind === "error");
  owner.transcript.dispatch({ kind: "retry", direction: "older" });
  await waitForSnapshot(
    owner,
    () => owner.snapshot.transcript.older.kind === "idle" && owner.snapshot.transcript.older.cursor === 200,
  );
  expect(fixture.getTranscriptPage).toHaveBeenCalledTimes(3);
  owner.dispose();
});

async function waitForSnapshot(owner: RuntimeHarness, ready: () => boolean) {
  await waitFor(() => {
    expect(ready()).toBe(true);
  });
}
