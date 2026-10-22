import { expect, test } from "bun:test";
import { presentUsageWindow } from "./presentation";

test("ordinary windows show remaining allowance", () => {
  expect(presentUsageWindow({ usedPercent: 23, contributions: null }, "chatgpt")).toEqual({
    percent: 77,
    kind: "remaining",
    segments: [{ id: "remaining", percent: 77 }],
    contributions: null,
  });
});

test("Grok uses supplied complete consistent contributions, preserving zero versus absence", () => {
  expect(
    presentUsageWindow({ usedPercent: 30, contributions: { build: 0, chat: 30 } }, "grok"),
  ).toMatchObject({
    percent: 30,
    kind: "consumed",
    segments: [
      { id: "build", percent: 0 },
      { id: "chat", percent: 30 },
    ],
    contributions: { build: 0, chat: 30 },
  });
  expect(
    presentUsageWindow({ usedPercent: 30, contributions: { build: null, chat: 30 } }, "grok"),
  ).toMatchObject({ segments: [{ id: "consumed", percent: 30 }], contributions: null });
  expect(
    presentUsageWindow({ usedPercent: 30, contributions: { build: 20, chat: 30 } }, "grok"),
  ).toMatchObject({ segments: [{ id: "consumed", percent: 30 }], contributions: null });
});
