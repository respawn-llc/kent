import { afterEach, expect, it, vi } from "vitest";

import { createAppLogger, type AppLogLevel } from "./logging";

afterEach(() => {
  vi.unstubAllEnvs();
});

it("records native observation overflow as a structured warning", async () => {
  vi.stubEnv("KENT_DEBUG", "false");
  const entries: Readonly<{ context: Readonly<Record<string, string>>; level: AppLogLevel }>[] = [];
  const logger = createAppLogger(async (level, _message, context = {}) => {
    entries.push({ context, level });
  });

  await logger.reportObservationOverflow("focus");

  expect(entries).toHaveLength(1);
  expect(entries[0]).toMatchObject({ context: { observation: "focus" }, level: "warn" });
});
