import { expect, test } from "vitest";
import type { PendingPrompt } from "./promptModels";
import { orderPendingPrompts } from "./promptPresentation";

test("orders Step groups chronologically and Questions by prepared order", () => {
  const prompt = (stepID: string, toolCallID: string, seconds: number): PendingPrompt => ({
    kind: "ordinary",
    stepID,
    toolCallID,
    sessionID: "session",
    question: "Question",
    suggestions: [],
    recommendedOptionIndex: null,
    createdAt: new Date(seconds * 1000).toISOString(),
    batch: { toolCallIDs: ["first", "second"], unmaterializedCount: 0 },
  });
  const older = prompt("ffffffff-ffff-4fff-8fff-ffffffffffff", "second", 1);
  const olderFirst = prompt(older.stepID, "first", 3);
  const newer = prompt("11111111-1111-4111-8111-111111111111", "first", 2);
  expect(orderPendingPrompts([newer, older, olderFirst]).map((item) => item.toolCallID)).toEqual([
    "first",
    "second",
    "first",
  ]);
  expect(orderPendingPrompts([olderFirst, newer, older])[0]?.stepID).toBe(older.stepID);
});
