import { RegistryProvider, useAtomMount, useAtomValue } from "@effect/atom-react";
import { QueryClient } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "bun:test";
import type { ReactNode } from "react";
import * as Effect from "effect/Effect";
import { SpeechTransportError, type SpeechAvailable } from "@/api";
import type { SpeechCapture, SpeechSource } from "@/app-facade";
import type { SpeechMicrophoneEvent } from "@/app-facade";
import { queryKeys } from "@/app-facade";
import { createTestServices } from "@/test-support/app-services";
import { deferred } from "@/test-support/chat-runtime";
import { createSpeechDestination, useSpeechActions } from "./model";
import { createSpeechTarget } from "./insertion";
import { installTestMicrophone } from "@/test-support/microphone";

const target = createSpeechTarget({ read: () => "", write: () => undefined, editor: () => null });
const field = Symbol("origin");
const siblingField = Symbol("sibling");
const globals = new Map<string, PropertyDescriptor | undefined>();
let microphone: ReturnType<typeof installTestMicrophone> | undefined;
function stubGlobal(name: string, value: unknown) {
  if (!globals.has(name)) globals.set(name, Object.getOwnPropertyDescriptor(globalThis, name));
  Object.defineProperty(globalThis, name, { configurable: true, writable: true, value });
}

afterEach(() => {
  microphone?.restore();
  microphone = undefined;
  for (const [name, descriptor] of globals) {
    if (descriptor === undefined) Reflect.deleteProperty(globalThis, name);
    else Object.defineProperty(globalThis, name, descriptor);
  }
  globals.clear();
  vi.restoreAllMocks();
});

const available: SpeechAvailable = {
  uploadUrl: "/speech?test",
  encodings: [{ mimeType: "audio/webm", extension: "webm" }],
  maxBytes: 25_000_000,
  maxDurationSeconds: 600,
};

it("releases a microphone permission result that arrives after destination disposal", async () => {
  const permission = deferred<{ getTracks(): readonly { stop(): void }[] }>();
  const track = { stop: vi.fn() };
  const stream = { getTracks: () => [track] };
  stubGlobal("navigator", { mediaDevices: { getUserMedia: async () => permission.promise } });
  stubGlobal("MediaRecorder", { isTypeSupported: () => true });
  const services = createTestServices([]);
  vi.spyOn(services.api, "speechAvailability").mockResolvedValue(available);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  client.setQueryData(queryKeys.speechAvailability, available);
  const model = createSpeechDestination({
    services,
    client,
    deliver: vi.fn(() => Effect.succeed({ kind: "inserted" } as const)),
    recover: vi.fn(),
    report: vi.fn(),
    onLimit: vi.fn(),
  });
  const view = renderHook(
    () => {
      useAtomMount(model.start);
      return {
        state: useAtomValue(model.state),
        actions: useSpeechActions(model, field, () => target),
      };
    },
    { wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider> },
  );
  act(() => {
    view.result.current.actions.start();
  });
  await waitFor(() => {
    expect(view.result.current.state.phase).toBe("preparing");
  });
  view.unmount();
  await act(async () => {
    permission.resolve(stream);
  });
  await waitFor(() => {
    expect(track.stop).toHaveBeenCalled();
  });
  client.clear();
});

function recordingFixture(manualStart = false, captureMode: "browser" | "native" = "browser") {
  microphone = installTestMicrophone(manualStart);
  const { startRequested, track, ready } = microphone;
  const source: SpeechSource = {
    kind: "native",
    original: { path: "/temporary/test-recording.webm", mimeType: "audio/webm", bytes: 2000 },
    durationMs: 2000,
  };
  const baseServices = createTestServices([]);
  const services = { ...baseServices, speech: { ...baseServices.speech, microphone: captureMode } };
  vi.spyOn(services.api, "speechAvailability").mockResolvedValue(available);
  const capture: SpeechCapture = {
    append: vi.fn(async () => ({ bytes: 2000, limitReached: false })),
    finalize: vi.fn(async () => source),
    result: vi.fn(async () => {
      throw new SpeechTransportError("disconnected");
    }),
    cancel: vi.fn(async () => undefined),
  };
  vi.spyOn(services.speech, "begin").mockResolvedValue(capture);
  const retry = vi.spyOn(services.speech, "retry").mockResolvedValue({
    result: async () => ({ kind: "transcript", text: "kept speaking" }),
    cancel: async () => undefined,
  });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  client.setQueryData(queryKeys.speechAvailability, available);
  const deliver = vi.fn(() => Effect.succeed({ kind: "inserted" } as const));
  const recover = vi.fn();
  const report = vi.fn();
  const model = createSpeechDestination({
    services,
    client,
    deliver,
    recover,
    report,
    onLimit: vi.fn(),
  });
  const view = renderHook(
    () => {
      useAtomMount(model.start);
      return {
        state: useAtomValue(model.state),
        actions: useSpeechActions(model, field, () => target),
        siblingActions: useSpeechActions(model, siblingField, () => target),
      };
    },
    { wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider> },
  );
  return {
    view,
    track,
    services,
    source,
    capture,
    retry,
    client,
    deliver,
    recover,
    report,
    startRequested,
    ready: () => {
      ready();
    },
  };
}

function nativeRecordingFixture() {
  const f = recordingFixture(false, "native");
  f.client.setQueryData(queryKeys.speechAvailability, {
    ...available,
    encodings: [{ mimeType: "audio/mp4", extension: "m4a" }],
  });
  let receive: ((event: SpeechMicrophoneEvent) => void) | undefined;
  const start = vi.fn(async (listener: (event: SpeechMicrophoneEvent) => void) => {
    receive = listener;
  });
  const stop = vi.fn(async () => {
    receive?.({ kind: "finished", error: null });
  });
  vi.spyOn(f.services.speech, "begin").mockResolvedValue({
    ...f.capture,
    microphone: {
      start,
      stop,
    },
  });
  return {
    ...f,
    start,
    stop,
    emit: (event: SpeechMicrophoneEvent) => {
      receive?.(event);
    },
  };
}

it("keeps native capture preparing until a real silent audio buffer arrives without opening WebKit capture", async () => {
  const f = nativeRecordingFixture();
  act(() => {
    f.view.result.current.actions.start();
  });
  await waitFor(() => {
    expect(f.start).toHaveBeenCalled();
  });
  expect(f.view.result.current.state.phase).toBe("preparing");
  expect(f.view.result.current.state.recording).toBeNull();
  expect(f.startRequested).not.toHaveBeenCalled();
  act(() => {
    f.emit({ kind: "ready" });
    f.emit({ kind: "meter", power: 0 });
  });
  await waitFor(() => {
    expect(f.view.result.current.state.phase).toBe("recording");
  });
  f.view.unmount();
  f.client.clear();
});

it("leaves native preparation after acquisition fails without hanging destination cleanup", async () => {
  const f = nativeRecordingFixture();
  f.start.mockRejectedValueOnce(new Error("native acquisition failed"));
  act(() => {
    f.view.result.current.actions.start();
  });
  await waitFor(() => {
    expect(f.report).toHaveBeenCalled();
  });
  expect(f.view.result.current.state.phase).toBe("idle");
  expect(f.capture.cancel).toHaveBeenCalled();
  f.view.unmount();
  f.client.clear();
});

it("reports denied native microphone access without offering recovery for an empty original", async () => {
  const f = nativeRecordingFixture();
  vi.spyOn(f.capture, "finalize").mockResolvedValue({
    kind: "native",
    original: { path: "/temporary/empty.mp4", mimeType: "audio/mp4", bytes: 0 },
    durationMs: 0,
  });
  const discard = vi.spyOn(f.services.speech, "discard").mockResolvedValue();
  act(() => {
    f.view.result.current.actions.start();
  });
  await waitFor(() => {
    expect(f.start).toHaveBeenCalled();
  });
  await act(async () => {
    f.emit({ kind: "finished", error: "permission denied" });
  });
  await waitFor(() => {
    expect(f.report).toHaveBeenCalled();
  });
  expect(f.recover).not.toHaveBeenCalled();
  expect(discard).toHaveBeenCalledTimes(1);
  expect(f.view.result.current.state.phase).toBe("idle");
  f.view.unmount();
  f.client.clear();
});

it.each([0, 0.4])(
  "keeps a native encoder failure's original in recovery at microphone power %s",
  async (power) => {
    const f = nativeRecordingFixture();
    act(() => {
      f.view.result.current.actions.start();
    });
    await waitFor(() => {
      expect(f.start).toHaveBeenCalled();
    });
    act(() => {
      f.emit({ kind: "ready" });
      f.emit({ kind: "meter", power });
      f.emit({ kind: "finished", error: "encoder failed" });
    });
    await waitFor(() => {
      expect(f.recover).toHaveBeenCalled();
    });
    expect(f.recover.mock.calls[0]?.[0]).toBe(f.source);
    expect(f.capture.result).not.toHaveBeenCalled();
    expect(f.view.result.current.state.phase).toBe("idle");
    f.view.unmount();
    f.client.clear();
  },
);

it("finishes recording only after its transcript has been delivered", async () => {
  const f = recordingFixture();
  const delivery = deferred<{ kind: "inserted" }>();
  f.deliver.mockImplementation(() => Effect.promise(async () => delivery.promise));
  act(() => {
    f.view.result.current.actions.start();
  });
  await waitFor(() => {
    expect(f.view.result.current.state.phase).toBe("recording");
  });
  let completed = false;
  let finishing: Promise<boolean>;
  act(() => {
    finishing = f.view.result.current.actions.finish().then((result) => {
      completed = true;
      return result;
    });
  });
  await waitFor(() => {
    expect(f.deliver).toHaveBeenCalled();
  });
  expect(completed).toBe(false);
  await act(async () => {
    delivery.resolve({ kind: "inserted" });
    expect(await finishing).toBe(true);
  });
  expect(f.view.result.current.state.phase).toBe("idle");
  f.view.unmount();
  f.client.clear();
});

it("does not report a failed transcription as ready to submit", async () => {
  const f = recordingFixture();
  f.retry.mockRejectedValue(new Error("transcription failed"));
  act(() => {
    f.view.result.current.actions.start();
  });
  await waitFor(() => {
    expect(f.view.result.current.state.phase).toBe("recording");
  });
  await act(async () => {
    expect(await f.view.result.current.actions.finish()).toBe(false);
  });
  expect(f.recover).toHaveBeenCalled();
  expect(f.deliver).not.toHaveBeenCalled();
  f.view.unmount();
  f.client.clear();
});

it("keeps preparation visible until the recorder confirms capture has started", async () => {
  const f = recordingFixture(true);
  act(() => {
    f.view.result.current.actions.start();
  });
  await waitFor(() => {
    expect(f.startRequested).toHaveBeenCalled();
  });
  expect(f.view.result.current.state.phase).toBe("preparing");
  act(() => {
    f.ready();
  });
  await waitFor(() => {
    expect(f.view.result.current.state.phase).toBe("recording");
  });
  f.view.unmount();
  f.client.clear();
});

it("finalizes owned capture if the destination closes while recorder startup is pending", async () => {
  const f = recordingFixture(true);
  act(() => {
    f.view.result.current.actions.start();
  });
  await waitFor(() => {
    expect(f.startRequested).toHaveBeenCalled();
  });
  f.view.unmount();
  await waitFor(() => {
    expect(f.track.stop).toHaveBeenCalled();
  });
  await waitFor(() => {
    expect(f.recover).toHaveBeenCalled();
  });
  expect(f.recover.mock.calls[0]?.[0]).toBe(f.source);
  expect(f.recover.mock.calls[0]?.[1]).toBeInstanceOf(Error);
  expect(f.recover.mock.calls[0]?.[2]).toBe(target);
  expect(f.deliver).not.toHaveBeenCalled();
  f.client.clear();
});

it("keeps capture running after transport loss and makes its fresh attempt only at Stop", async () => {
  const f = recordingFixture();
  act(() => {
    f.view.result.current.actions.start();
  });
  await waitFor(() => {
    expect(f.view.result.current.state.phase).toBe("recording");
  });
  expect(f.track.stop).not.toHaveBeenCalled();
  expect(f.retry).not.toHaveBeenCalled();
  act(() => {
    f.view.result.current.actions.stop();
  });
  await waitFor(() => {
    expect(f.deliver).toHaveBeenCalledWith(f.source, { kind: "transcript", text: "kept speaking" }, target);
  });
  expect(f.retry).toHaveBeenCalledTimes(1);
  expect(f.track.stop).toHaveBeenCalled();
  expect(f.recover).not.toHaveBeenCalled();
  f.view.unmount();
  f.client.clear();
});

it("keeps sibling capture unavailable until the destination transcription finishes", async () => {
  const f = recordingFixture();
  const finalization = deferred<SpeechSource>();
  vi.spyOn(f.capture, "finalize").mockReturnValue(finalization.promise);
  act(() => {
    f.view.result.current.actions.start();
  });
  await waitFor(() => {
    expect(f.view.result.current.state.phase).toBe("recording");
  });
  expect(f.view.result.current.state.busy).toBe(true);
  act(() => {
    f.view.result.current.actions.stop();
  });
  await waitFor(() => {
    expect(f.view.result.current.state.busy).toBe(true);
  });
  expect(f.view.result.current.state.busy).toBe(true);
  act(() => {
    f.view.result.current.siblingActions.start();
  });
  expect(f.services.speech.begin).toHaveBeenCalledTimes(1);
  await act(async () => {
    finalization.resolve(f.source);
  });
  await waitFor(() => {
    expect(f.view.result.current.state.busy).toBe(false);
  });
  expect(f.view.result.current.state.busy).toBe(false);
  f.view.unmount();
  f.client.clear();
});

it.each([null, new Error("cancel failed")])(
  "preserves a disposed destination's original when cancellation returns %s",
  async (cancelError) => {
    const f = recordingFixture();
    if (cancelError !== null) vi.spyOn(f.capture, "cancel").mockRejectedValueOnce(cancelError);
    act(() => {
      f.view.result.current.actions.start();
    });
    await waitFor(() => {
      expect(f.view.result.current.state.phase).toBe("recording");
    });
    f.view.unmount();
    await waitFor(() => {
      expect(f.recover).toHaveBeenCalled();
    });
    expect(f.recover.mock.calls[0]?.[0]).toBe(f.source);
    expect(f.recover.mock.calls[0]?.[1]).toBeInstanceOf(Error);
    expect(f.capture.cancel).toHaveBeenCalled();
    if (cancelError !== null) expect(f.report).toHaveBeenCalled();
    expect(f.track.stop).toHaveBeenCalled();
    expect(f.deliver).not.toHaveBeenCalled();
    f.client.clear();
  },
);
