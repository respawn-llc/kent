import { RegistryProvider, useAtomMount, useAtomValue } from "@effect/atom-react";
import { QueryClient } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import * as Effect from "effect/Effect";
import { SpeechTransportError, type SpeechAvailable } from "@/api";
import type { SpeechCapture, SpeechSource } from "@/app-facade";
import { queryKeys } from "@/app-facade";
import { createTestServices } from "@/test-support/app-services";
import { deferred } from "@/test-support/chat-runtime";
import { createSpeechDestination, useSpeechActions } from "./model";
import { createSpeechTarget } from "./insertion";

const target = createSpeechTarget({ read: () => "", write: () => undefined, editor: () => null });
const field = Symbol("origin");
const siblingField = Symbol("sibling");

afterEach(() => {
  vi.unstubAllGlobals();
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
  vi.stubGlobal("navigator", { mediaDevices: { getUserMedia: async () => permission.promise } });
  vi.stubGlobal("MediaRecorder", { isTypeSupported: () => true });
  const services = createTestServices([]);
  vi.spyOn(services.api, "speechAvailability").mockResolvedValue(available);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  client.setQueryData(queryKeys.speechAvailability, available);
  const model = createSpeechDestination({
    services,
    client,
    deliver: vi.fn(() => Effect.void),
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

function recordingFixture(manualStart = false) {
  const startRequested = vi.fn();
  let ready = () => undefined;
  const track = { stop: vi.fn() };
  vi.stubGlobal("navigator", { mediaDevices: { getUserMedia: async () => ({ getTracks: () => [track] }) } });
  vi.stubGlobal(
    "MediaRecorder",
    class extends EventTarget {
      static isTypeSupported() {
        return true;
      }
      state: RecordingState = "inactive";
      start() {
        this.state = "recording";
        startRequested();
        ready = () => {
          this.dispatchEvent(new Event("start"));
        };
        if (!manualStart) queueMicrotask(ready);
      }
      requestData() {
        return undefined;
      }
      stop() {
        this.state = "inactive";
        queueMicrotask(() => {
          this.dispatchEvent(new Event("stop"));
        });
      }
    },
  );
  vi.stubGlobal(
    "AudioContext",
    class {
      async resume() {
        return undefined;
      }
      createAnalyser() {
        return { fftSize: 256, getByteTimeDomainData: (data: Uint8Array) => data.fill(140) };
      }
      createMediaStreamSource() {
        return {
          connect() {
            return undefined;
          },
        };
      }
      async close() {
        return undefined;
      }
    },
  );
  const source: SpeechSource = {
    kind: "native",
    original: { path: "/temporary/test-recording.webm", mimeType: "audio/webm", bytes: 2000 },
    durationMs: 2000,
  };
  const services = createTestServices([]);
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
  const deliver = vi.fn(() => Effect.void);
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
  act(() => { f.view.result.current.actions.start(); });
  await waitFor(() => { expect(f.startRequested).toHaveBeenCalled(); });
  f.view.unmount();
  await waitFor(() => { expect(f.track.stop).toHaveBeenCalled(); });
  await waitFor(() => { expect(f.recover).toHaveBeenCalled(); });
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
  vi.mocked(f.capture.finalize).mockReturnValue(finalization.promise);
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
    if (cancelError !== null) vi.mocked(f.capture.cancel).mockRejectedValueOnce(cancelError);
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
