import { expect, it, vi } from "bun:test";

import type { ChatApi, ChatSessionTarget, ChatTranscriptPage } from "@/api";
import { hydration, page, row } from "@/test-support/transcript-window";
import { deferred } from "@/test-support/chat-runtime";

import { ChatTranscriptHost } from "./chatTranscriptHost";

const target: ChatSessionTarget = {
  projectID: "project-1",
  sessionID: "123e4567-e89b-42d3-a456-426614174000",
};

it("keeps replacement hydration authoritative after a delayed newest result", async () => {
  const delayed = deferred<ChatTranscriptPage>();
  const getTranscriptPage = vi
    .fn<Pick<ChatApi, "getTranscriptPage">["getTranscriptPage"]>()
    .mockResolvedValueOnce(page([row(20)], 200, 300))
    .mockResolvedValueOnce(page([row(10)], 100, 200))
    .mockReturnValueOnce(delayed.promise);
  const host = new ChatTranscriptHost({ getTranscriptPage }, target, {
    onContractFailure: vi.fn(),
    onOpeningFailure: vi.fn(),
    onScratchRehydration: vi.fn(),
  });
  host.hydration("initial", hydration([row(30)]));
  host.dispatch({ kind: "edge-visit", direction: "older", older: true, newer: false });
  await waitForHost(host, (snapshot) => snapshot.older.kind === "idle" && snapshot.older.cursor === 200);
  host.dispatch({ kind: "edge-visit", direction: "older", older: true, newer: false });
  await waitForHost(host, (snapshot) => snapshot.older.kind === "idle" && snapshot.older.cursor === 100);

  const jump = host.jumpToLatest();
  host.hydration("scratch", hydration([row(50)], 1));
  expect(jump).resolves.toEqual({ kind: "tail-ready" });
  delayed.resolve(page([row(40)], 400));
  await delayed.promise;
  expect(
    host.snapshot.items.flatMap((item) => ("row" in item ? [item.row.Locator.event_sequence] : [])),
  ).toEqual([50]);
  host.dispose();
});

it("executes opening and directional TranscriptWindow page requests", async () => {
  const pages = [page([row(30)], 300), page([row(20)], 200, 300)];
  const getTranscriptPage = vi.fn<ChatApi["getTranscriptPage"]>(async (): Promise<ChatTranscriptPage> => {
    const next = pages.shift();
    if (next === undefined) throw new Error("Unexpected transcript page request.");
    return next;
  });
  const host = new ChatTranscriptHost(
    { getTranscriptPage } satisfies Pick<ChatApi, "getTranscriptPage">,
    target,
    {
      onContractFailure: vi.fn(),
      onOpeningFailure: vi.fn(),
      onScratchRehydration: vi.fn(),
    },
  );

  host.open();
  await waitForHost(host, (snapshot) => snapshot.opening.kind === "ready");
  host.dispatch({ kind: "edge-visit", direction: "older", older: true, newer: false });
  await waitForHost(host, (snapshot) => snapshot.older.kind === "idle" && snapshot.older.cursor === 200);

  expect(getTranscriptPage.mock.calls).toEqual([[target], [target, { direction: "older", value: 300 }]]);
  expect(
    host.snapshot.items.flatMap((item) => ("row" in item ? [item.row.Locator.event_sequence] : [])),
  ).toEqual([20, 30]);
});

it("keeps a failed directional boundary retryable with the same opaque cursor", async () => {
  const pages: (() => Promise<ChatTranscriptPage>)[] = [
    async () => page([row(30)], 300),
    async () => Promise.reject(new Error("Page unavailable")),
    async () => page([row(20)], 200, 300),
  ];
  const getTranscriptPage = vi.fn<ChatApi["getTranscriptPage"]>(async (): Promise<ChatTranscriptPage> => {
    const next = pages.shift();
    if (next === undefined) throw new Error("Unexpected transcript page request.");
    return next();
  });
  const host = new ChatTranscriptHost(
    { getTranscriptPage } satisfies Pick<ChatApi, "getTranscriptPage">,
    target,
    {
      onContractFailure: vi.fn(),
      onOpeningFailure: vi.fn(),
      onScratchRehydration: vi.fn(),
    },
  );
  host.open();
  await waitForHost(host, (snapshot) => snapshot.opening.kind === "ready");

  host.dispatch({ kind: "edge-visit", direction: "older", older: true, newer: false });
  await waitForHost(host, (snapshot) => snapshot.older.kind === "error");
  host.dispatch({ kind: "retry", direction: "older" });
  await waitForHost(host, (snapshot) => snapshot.older.kind === "idle" && snapshot.older.cursor === 200);

  expect(getTranscriptPage.mock.calls.slice(1)).toEqual([
    [target, { direction: "older", value: 300 }],
    [target, { direction: "older", value: 300 }],
  ]);
});

async function waitForHost(
  host: ChatTranscriptHost,
  matches: (snapshot: ChatTranscriptHost["snapshot"]) => boolean,
): Promise<void> {
  if (matches(host.snapshot)) return Promise.resolve();
  return new Promise<void>((resolve) => {
    const unsubscribe = host.subscribe(() => {
      if (!matches(host.snapshot)) return;
      unsubscribe();
      resolve();
    });
  });
}
