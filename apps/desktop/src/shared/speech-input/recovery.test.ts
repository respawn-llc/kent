import { expect, it, vi } from "vitest";
import type { SpeechSource } from "@/app-facade";
import { createSpeechTarget } from "./insertion";
import { deliverSpeech } from "./recovery";

const source: SpeechSource = { kind: "browser", blob: new Blob(["audio"]), durationMs: 2000 };

it("delivers into the surviving current draft and releases the original", async () => {
  let draft = "edited";
  const target = createSpeechTarget({
    read: () => draft,
    write: (value) => {
      draft = value;
    },
    editor: () => null,
  });
  const discard = vi.fn(async () => undefined);
  const copy = vi.fn(async () => undefined);
  const outcome = await deliverSpeech(
    source,
    target,
    { kind: "transcript", text: "speech" },
    { discard, copy },
  );
  expect(draft).toBe("edited\n\nspeech");
  expect(outcome.kind).toBe("inserted");
  expect(discard).toHaveBeenCalledWith(source);
  expect(copy).not.toHaveBeenCalled();
});

it("retains the original after delivery to the clipboard when the target has ended", async () => {
  const target = createSpeechTarget({ read: () => null, write: vi.fn(), editor: () => null });
  const discard = vi.fn(async () => undefined);
  const copy = vi.fn(async () => undefined);
  const result = await deliverSpeech(source, target, { kind: "transcript", text: "原文" }, { discard, copy });
  expect(result.kind).toBe("copied");
  expect(copy).toHaveBeenCalledWith("原文");
  expect(discard).not.toHaveBeenCalled();
});

it("retains usable text and the original when clipboard delivery fails", async () => {
  const target = createSpeechTarget({ read: () => null, write: vi.fn(), editor: () => null });
  const error = new Error("clipboard denied");
  const discard = vi.fn(async () => undefined);
  const copy = vi.fn(async () => {
    throw error;
  });
  const result = await deliverSpeech(
    source,
    target,
    { kind: "transcript", text: "usable" },
    { discard, copy },
  );
  expect(result).toEqual({ kind: "copy-failed", text: "usable", error });
  expect(discard).not.toHaveBeenCalled();
});

it("releases a successful no-op without touching either delivery destination", async () => {
  const write = vi.fn();
  const target = createSpeechTarget({ read: () => "", write, editor: () => null });
  const discard = vi.fn(async () => undefined);
  const copy = vi.fn(async () => undefined);
  expect(await deliverSpeech(source, target, { kind: "noop" }, { discard, copy })).toEqual({ kind: "noop" });
  expect(discard).toHaveBeenCalledWith(source);
  expect(copy).not.toHaveBeenCalled();
  expect(write).not.toHaveBeenCalled();
});
