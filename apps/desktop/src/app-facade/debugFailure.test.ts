import { afterEach, expect, it, vi } from "vitest";

import { recoverOrThrowDebugFailure } from "./debugFailure";

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

function deferred<Value>() {
  let resolve!: (value: Value) => void;
  const promise = new Promise<Value>((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}
