import { expect, it, vi } from "vitest";

import type { ChatApi, ChatSessionTarget, ChatTranscriptPage } from "@/api";
import { page, row } from "@/test-support/transcript-window";

import { ChatTranscriptHost } from "./chatTranscriptHost";

const target: ChatSessionTarget = {
  projectID: "project-1",
  workspace: { workspaceID: "workspace-1" },
  sessionID: "123e4567-e89b-42d3-a456-426614174000",
};

it("executes opening and directional TranscriptWindow page requests", async () => {
  const pages = [page([row(30)], 300), page([row(20)], 200, 300)];
  const getTranscriptPage = vi.fn(async (): Promise<ChatTranscriptPage> => {
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
  await vi.waitFor(() => {
    expect(host.snapshot.opening.kind).toBe("ready");
  });
  host.dispatch({ kind: "edge-visit", direction: "older", older: true, newer: false });
  await vi.waitFor(() => {
    expect(host.snapshot.older).toEqual({ kind: "idle", cursor: 200 });
  });

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
  const getTranscriptPage = vi.fn(async (): Promise<ChatTranscriptPage> => {
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
  await vi.waitFor(() => {
    expect(host.snapshot.opening.kind).toBe("ready");
  });

  host.dispatch({ kind: "edge-visit", direction: "older", older: true, newer: false });
  await vi.waitFor(() => {
    expect(host.snapshot.older.kind).toBe("error");
  });
  host.dispatch({ kind: "retry", direction: "older" });
  await vi.waitFor(() => {
    expect(host.snapshot.older).toEqual({ kind: "idle", cursor: 200 });
  });

  expect(getTranscriptPage.mock.calls.slice(1)).toEqual([
    [target, { direction: "older", value: 300 }],
    [target, { direction: "older", value: 300 }],
  ]);
});
