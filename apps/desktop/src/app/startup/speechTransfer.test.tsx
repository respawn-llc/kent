import { expect, it, vi } from "bun:test";
import { createTauriNativeBridge } from "@app/native-bridge";
import { createSpeechTransfer } from "./speechTransfer";

it("uses the native alert without playing the embedded sound when accepted", async () => {
  const bridge = createTauriNativeBridge("windows");
  if (bridge.speech === null) throw new Error("Native speech bridge required");
  const native = vi.spyOn(bridge.speech, "limitSound").mockResolvedValue(true);
  const play = vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue();
  try {
    await createSpeechTransfer(bridge, "ws://localhost:53082/rpc").limitSound();
    expect(native).toHaveBeenCalledTimes(1);
    expect(play).not.toHaveBeenCalled();
  } finally {
    native.mockRestore();
    play.mockRestore();
  }
});

it("reports both failures to the caller's status boundary when no alert can play", async () => {
  const bridge = createTauriNativeBridge("windows");
  if (bridge.speech === null) throw new Error("Native speech bridge required");
  const nativeFailure = new Error("native audio device failed");
  const playbackFailure = new Error("media decoding failed");
  const native = vi.spyOn(bridge.speech, "limitSound").mockRejectedValue(nativeFailure);
  const play = vi.spyOn(HTMLMediaElement.prototype, "play").mockRejectedValue(playbackFailure);
  const report = vi.fn();
  try {
    await createSpeechTransfer(bridge, "ws://localhost:53082/rpc").limitSound().catch(report);
    expect(play).toHaveBeenCalledTimes(1);
    expect(report).toHaveBeenCalledTimes(1);
    expect(report.mock.calls[0]?.[0]).toBeInstanceOf(AggregateError);
    const failure: unknown = report.mock.calls[0]?.[0];
    if (!(failure instanceof AggregateError)) throw new Error("Expected combined failure");
    expect(failure.errors).toEqual([nativeFailure, playbackFailure]);
  } finally {
    native.mockRestore();
    play.mockRestore();
  }
});

it("plays the embedded alert when native sound is unavailable", async () => {
  const bridge = createTauriNativeBridge("windows");
  if (bridge.speech === null) throw new Error("Native speech bridge required");
  const native = vi.spyOn(bridge.speech, "limitSound").mockResolvedValue(false);
  const play = vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue();
  try {
    await createSpeechTransfer(bridge, "ws://localhost:53082/rpc").limitSound();
    expect(play).toHaveBeenCalledTimes(1);
  } finally {
    native.mockRestore();
    play.mockRestore();
  }
});
