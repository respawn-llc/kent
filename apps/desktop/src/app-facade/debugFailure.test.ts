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
