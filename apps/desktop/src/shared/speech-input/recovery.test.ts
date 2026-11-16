import { expect, it, vi } from "bun:test";
import { createSpeechTarget } from "./insertion";
import { deliverSpeech } from "./recovery";

it("delivers into the surviving current draft and retains the original", async () => {
  let draft = "edited";
  const target = createSpeechTarget({
    read: () => draft,
    write: (value) => {
      draft = value;
    },
    editor: () => null,
  });
  const copy = vi.fn(async () => undefined);
  const outcome = await deliverSpeech(target, { kind: "transcript", text: "speech" }, { copy });
  expect(draft).toBe("edited\n\nspeech");
  expect(outcome.kind).toBe("inserted");
  expect(copy).not.toHaveBeenCalled();
});

it("retains the original after delivery to the clipboard when the target has ended", async () => {
  const target = createSpeechTarget({ read: () => null, write: vi.fn(), editor: () => null });
  const copy = vi.fn(async () => undefined);
  const result = await deliverSpeech(target, { kind: "transcript", text: "原文" }, { copy });
  expect(result.kind).toBe("copied");
  expect(copy).toHaveBeenCalledWith("原文");
});

it("retains usable text and the original when clipboard delivery fails", async () => {
  const target = createSpeechTarget({ read: () => null, write: vi.fn(), editor: () => null });
  const error = new Error("clipboard denied");
  const copy = vi.fn(async () => {
    throw error;
  });
  const result = await deliverSpeech(target, { kind: "transcript", text: "usable" }, { copy });
  expect(result).toEqual({ kind: "copy-failed", text: "usable", error });
});

it("retains a successful no-op without touching either delivery destination", async () => {
  const write = vi.fn();
  const target = createSpeechTarget({ read: () => "", write, editor: () => null });
  const copy = vi.fn(async () => undefined);
  expect(await deliverSpeech(target, { kind: "noop" }, { copy })).toEqual({ kind: "noop" });
  expect(copy).not.toHaveBeenCalled();
  expect(write).not.toHaveBeenCalled();
});
