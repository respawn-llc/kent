import { afterEach, expect, it, vi } from "vitest";

import { recoverOrThrowDebugFailure } from "./debugFailure";
import type { AppLogger } from "./logging";

afterEach(() => {
  vi.unstubAllEnvs();
});

it("fails after recording diagnostics in development without running production recovery", async () => {
  vi.stubEnv("KENT_DEBUG", "true");
  const logger = { append: vi.fn().mockResolvedValue(undefined) };
  const recover = vi.fn();

  await expect(
    recoverOrThrowDebugFailure({
      context: { owner: "transcript" },
      error: new Error("broken invariant"),
      logger,
      message: "Invariant failure.",
      recover,
    }),
  ).rejects.toBeInstanceOf(Error);

  expect(logger.append).toHaveBeenCalledOnce();
  expect(recover).not.toHaveBeenCalled();
});

it("retains the original failure stack and cause when surfacing a debug invariant", async () => {
  vi.stubEnv("KENT_DEBUG", "true");
  const original = new Error("broken invariant");
  const logger = { append: vi.fn().mockResolvedValue(undefined) };
  const failure = recoverOrThrowDebugFailure({
    context: { owner: "transcript" },
    error: original,
    logger,
    message: "Invariant failure.",
    recover: vi.fn(),
  });
  await expect(failure).rejects.toHaveProperty("cause", original);
  expect(logger.append.mock.calls[0]?.[2]).toMatchObject({ stack: original.stack });
});

it("starts production recovery without waiting for diagnostic persistence", async () => {
  const diagnostic = deferred<undefined>();
  const logger = { append: vi.fn(async () => diagnostic.promise) };
  const recover = vi.fn();

  const pending = recoverOrThrowDebugFailure({
    context: { owner: "transcript" },
    error: new Error("broken invariant"),
    logger,
    message: "Invariant failure.",
    recover,
  });

  expect(recover).toHaveBeenCalledOnce();
  diagnostic.resolve(undefined);
  await pending;
});

it.each(["true", "false"])(
  "records structured failure evidence only with explicit debug=%s",
  async (debug) => {
    vi.stubEnv("KENT_DEBUG", debug);
    const evidence = {
      locator: { event_sequence: 42, row_ordinal: 1 },
      resident: { Text: "before" },
      incoming: { Text: "after" },
    };
    const logger = { append: vi.fn<AppLogger["append"]>().mockResolvedValue(undefined) };
    const pending = recoverOrThrowDebugFailure({
      context: {},
      error: new Error("conflicting payload", { cause: evidence }),
      logger,
      message: "Invariant failure.",
      recover: vi.fn(),
    });
    if (debug === "true") await expect(pending).rejects.toBeInstanceOf(Error);
    else await pending;
    const context = logger.append.mock.calls[0]?.[2];
    if (debug === "true") {
      expect(context?.evidenceID).toBeDefined();
      expect(logger.append.mock.calls[1]?.[2]).toMatchObject({
        evidenceID: context?.evidenceID,
        evidenceIndex: "0",
        evidenceCount: "1",
        evidenceChunk: JSON.stringify(evidence),
      });
    } else {
      expect(context).not.toHaveProperty("evidenceID");
      expect(logger.append).toHaveBeenCalledOnce();
    }
  },
);

it("records large debug evidence in bounded local log entries", async () => {
  vi.stubEnv("KENT_DEBUG", "true");
  const evidence = { Text: "\u0000".repeat(40_000) };
  const logger = {
    append: vi.fn<AppLogger["append"]>().mockImplementation(async (_level, message, context) => {
      if (new TextEncoder().encode(JSON.stringify({ message, context })).length > 64 * 1024) {
        throw new Error("Log entry too large");
      }
    }),
  };
  await expect(
    recoverOrThrowDebugFailure({
      context: { sessionID: "session" },
      error: new Error("payload conflict", { cause: evidence }),
      logger,
      message: "Invariant failure.",
      recover: vi.fn(),
    }),
  ).rejects.toHaveProperty("cause");
  const chunks = logger.append.mock.calls.flatMap(([, , context]) =>
    context?.evidenceChunk === undefined ? [] : [context.evidenceChunk],
  );
  expect(JSON.parse(chunks.join(""))).toEqual(evidence);
});

function deferred<Value>() {
  let resolve!: (value: Value) => void;
  const promise = new Promise<Value>((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}
