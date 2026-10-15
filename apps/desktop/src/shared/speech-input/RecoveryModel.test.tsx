import { RegistryProvider, useAtomValue } from "@effect/atom-react";
import { act, renderHook, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { createInstance } from "i18next";
import { createTestServices } from "@/test-support/app-services";
import type { SpeechSource } from "@/app-facade";
import { createSpeechTarget } from "./insertion";
import { createSpeechRecovery, useRecoveryActions } from "./RecoveryModel";
import { deferred } from "@/test-support/chat-runtime";
import type { SpeechResult } from "@/api";

const appI18n = createInstance();
await appI18n.init({ lng: "en", resources: {} });

it("Retry uploads the retained source as a new invocation and delivers to the surviving blurred draft", async () => {
  const services = createTestServices([]);
  vi.spyOn(services.api, "speechAvailability").mockResolvedValue({
    uploadUrl: "/speech",
    maxBytes: 10000,
    maxDurationSeconds: 600,
    encodings: [{ mimeType: "audio/webm", extension: "webm" }],
  });
  const source: SpeechSource = { kind: "browser", blob: new Blob(["audio"]), durationMs: 2000 };
  const upload = vi.spyOn(services.speech, "retry").mockResolvedValue({
    result: async () => ({ kind: "transcript", text: "retry text" }),
    cancel: async () => undefined,
  });
  const discard = vi.spyOn(services.speech, "discard").mockResolvedValue();
  let draft = "current";
  const target = createSpeechTarget({
    read: () => draft,
    write: (value) => {
      draft = value;
    },
    editor: () => null,
  });
  const model = createSpeechRecovery({ services, t: appI18n.t, push: vi.fn() });
  const view = renderHook(
    () => ({
      state: useAtomValue(model.state),
      actions: useRecoveryActions(model),
    }),
    { wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider> },
  );
  act(() => {
    view.result.current.actions.offer({ source, target, error: new Error("failed") });
  });
  act(() => {
    view.result.current.actions.retry();
  });
  await waitFor(() => {
    expect(draft).toBe("current\n\nretry text");
  });
  expect(upload).toHaveBeenCalledWith(source, expect.objectContaining({ maxBytes: 10000 }));
  expect(discard).toHaveBeenCalledWith(source);
  await waitFor(() => {
    expect(view.result.current.state.entry).toBeNull();
  });
  view.unmount();
});

it("Discard waits for Retry cancellation, releases the source, and prevents late delivery", async () => {
  const services = createTestServices([]);
  vi.spyOn(services.api, "speechAvailability").mockResolvedValue({
    uploadUrl: "/speech",
    maxBytes: 10000,
    maxDurationSeconds: 600,
    encodings: [{ mimeType: "audio/webm", extension: "webm" }],
  });
  const source: SpeechSource = { kind: "browser", blob: new Blob(["audio"]), durationMs: 2000 };
  const response = deferred<SpeechResult>();
  const canceled = deferred<undefined>();
  const cancel = vi.fn(async () => canceled.promise);
  vi.spyOn(services.speech, "retry").mockResolvedValue({ result: async () => response.promise, cancel });
  const discard = vi.spyOn(services.speech, "discard").mockResolvedValue();
  let draft = "";
  const target = createSpeechTarget({
    read: () => draft,
    write: (text) => {
      draft = text;
    },
    editor: () => null,
  });
  const model = createSpeechRecovery({ services, t: appI18n.t, push: vi.fn() });
  const view = renderHook(
    () => ({
      state: useAtomValue(model.state),
      actions: useRecoveryActions(model),
    }),
    { wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider> },
  );
  act(() => {
    view.result.current.actions.offer({ source, target, error: new Error("failed") });
  });
  act(() => {
    view.result.current.actions.retry();
  });
  await waitFor(() => {
    expect(services.speech.retry).toHaveBeenCalled();
  });
  act(() => {
    view.result.current.actions.discard();
  });
  await waitFor(() => {
    expect(cancel).toHaveBeenCalled();
  });
  expect(discard).not.toHaveBeenCalled();
  await act(async () => {
    canceled.resolve(undefined);
    response.resolve({ kind: "transcript", text: "late" });
  });
  await waitFor(() => {
    expect(view.result.current.state.entry).toBeNull();
  });
  expect(discard).toHaveBeenCalledWith(source);
  expect(draft).toBe("");
  view.unmount();
});

it("retains the same original after a failed Retry so the operator can try again", async () => {
  const services = createTestServices([]);
  vi.spyOn(services.api, "speechAvailability").mockResolvedValue({
    uploadUrl: "/speech",
    maxBytes: 10000,
    maxDurationSeconds: 600,
    encodings: [{ mimeType: "audio/webm", extension: "webm" }],
  });
  const source: SpeechSource = { kind: "browser", blob: new Blob(["audio"]), durationMs: 2000 };
  const failed = new Error("provider rejected");
  vi.spyOn(services.speech, "retry").mockResolvedValue({
    result: async () => {
      throw failed;
    },
    cancel: async () => undefined,
  });
  const discard = vi.spyOn(services.speech, "discard").mockResolvedValue();
  const target = createSpeechTarget({ read: () => "", write: vi.fn(), editor: () => null });
  const model = createSpeechRecovery({ services, t: appI18n.t, push: vi.fn() });
  const view = renderHook(
    () => ({
      state: useAtomValue(model.state),
      actions: useRecoveryActions(model),
    }),
    { wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider> },
  );
  act(() => {
    view.result.current.actions.offer({ source, target, error: new Error("first failure") });
  });
  act(() => {
    view.result.current.actions.retry();
  });
  await waitFor(() => {
    expect(view.result.current.state.entry?.error.cause).toBe(failed);
  });
  expect(view.result.current.state.entry?.source).toBe(source);
  expect(discard).not.toHaveBeenCalled();
  view.unmount();
});
