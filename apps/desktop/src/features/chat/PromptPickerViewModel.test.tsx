import { RegistryProvider, useAtomSet, useAtomValue } from "@effect/atom-react";
import { QueryClient } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { expect, it, vi } from "vitest";
import { ChatRuntimeOwner } from "@/app-facade";
import { runtimeApi, runtimeHost, target, deferred, hydration } from "@/test-support/chat-runtime";
import { question, approval, failedBatchWithFreeform } from "@/test-support/chat-prompts";
import type { PromptAnswerBatchInput, PromptAnswerBatchResponse } from "@/api";
import { createPromptPickerViewModel } from "./PromptPickerViewModel";

function mountPicker(options: Parameters<typeof createPromptPickerViewModel>[0]) {
  const model = createPromptPickerViewModel(options);
  return renderHook(
    () => ({
      state: useAtomValue(model.state),
      request: useAtomValue(model.request),
      dispatch: useAtomSet(model.dispatch),
    }),
    {
      wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider>,
    },
  );
}

function setupOwner() {
  const fixture = runtimeApi();
  const client = new QueryClient();
  const owner = new ChatRuntimeOwner(fixture.api, target, client, runtimeHost());
  owner.start();
  return { fixture, client, owner };
}

it("retains drafts on observation loss and discards them on departure without replaying an answer", async () => {
  const { owner, client, fixture } = setupOwner();
  const prompt = question();
  owner.replacePendingPrompts([prompt]);
  const response = deferred<PromptAnswerBatchResponse>();
  const send = vi.fn(async () => response.promise);
  const options = {
    owner,
    client,
    onError: vi.fn(),
    api: { answerPromptBatch: send },
  };
  const view = mountPicker(options);
  await act(async () => {
    view.result.current.dispatch({ action: { kind: "commentary", text: "Transient" } });
    view.result.current.dispatch({
      action: { kind: "activate", selection: { kind: "suggested", number: 1 } },
    });
  });
  expect(send).toHaveBeenCalledOnce();
  act(() => {
    fixture.handlers[0]?.onTransportLoss?.();
  });
  expect(view.result.current.state.drafts.size).toBe(1);
  expect(view.result.current.request.isPending).toBe(true);
  expect(view.result.current.state.drafts.get(prompt.toolCallID)).toMatchObject({
    status: "answered",
    commentary: "Transient",
  });
  await act(async () => {
    view.result.current.dispatch({ action: { kind: "confirm" } });
    response.resolve({ results: [{ toolCallID: prompt.toolCallID, outcome: "resolved" }] });
    await response.promise;
  });
  await vi.waitFor(() => {
    expect(owner.snapshot.pendingPrompts).toEqual([]);
  });
  expect(send).toHaveBeenCalledOnce();
  act(() => {
    owner.replacePendingPrompts([question("next")]);
  });
  act(() => {
    view.result.current.dispatch({ action: { kind: "commentary", text: "Discard on departure" } });
  });
  view.unmount();
  const utils = mountPicker(options);
  expect(utils.result.current.state.drafts.get("next")?.commentary).toBe("");
  expect(owner.snapshot.pendingPrompts).toEqual([question("next")]);
  expect(send).toHaveBeenCalledOnce();
  utils.unmount();
  await owner.dispose();
});

it("retains declines and pending facts after delivery failure, then explicitly resends all-declined entries", async () => {
  const { owner, client } = setupOwner();
  const prompt = question();
  owner.replacePendingPrompts([prompt]);
  const send = vi
    .fn<() => Promise<PromptAnswerBatchResponse>>()
    .mockRejectedValueOnce(new Error("Failed"))
    .mockResolvedValueOnce({ results: [{ toolCallID: prompt.toolCallID, outcome: "skipped" }] });
  const onError = vi.fn();
  const view = mountPicker({
    owner,
    client,
    onError,
    api: { answerPromptBatch: send },
  });
  await act(async () => {
    view.result.current.dispatch({ action: { kind: "decline" } });
  });
  await vi.waitFor(() => {
    expect(view.result.current.request.isError).toBe(true);
  });
  expect(onError).toHaveBeenCalledTimes(1);
  expect(owner.snapshot.pendingPrompts).toEqual([prompt]);
  expect(view.result.current.state.drafts.get(prompt.toolCallID)?.status).toBe("declined");
  expect(send).toHaveBeenCalledOnce();
  await act(async () => {
    view.result.current.dispatch({ action: { kind: "confirm" } });
  });
  await vi.waitFor(() => {
    expect(owner.snapshot.pendingPrompts).toEqual([]);
  });
  expect(send).toHaveBeenCalledTimes(2);
  view.unmount();
  await owner.dispose();
});

it("removes externally resolved drafts and ignores an earlier successful response when the next Step is visible", async () => {
  const { owner, client, fixture } = setupOwner();
  const first = question();
  const other = question("other");
  const later = question("later", { stepID: "423e4567-e89b-42d3-a456-426614174000" });
  fixture.handlers[0]?.onEvent({
    sequence: 1,
    kind: "hydration",
    payload: {
      ...hydration(),
      PendingPrompts: [first, other].map((prompt) => ({ state: "pending", prompt })),
    },
  });
  const response = deferred<PromptAnswerBatchResponse>();
  const send = vi.fn(async () => response.promise);
  const view = mountPicker({
    owner,
    client,
    onError: vi.fn(),
    api: { answerPromptBatch: send },
  });
  await act(async () => {
    view.result.current.dispatch({
      action: { kind: "activate", selection: { kind: "suggested", number: 1 } },
    });
    fixture.handlers[0]?.onEvent({
      sequence: 2,
      kind: "prompt",
      payload: { state: "resolved", toolCallID: other.toolCallID },
    });
  });
  expect(view.result.current.state.current).toBe(first.toolCallID);
  expect(view.result.current.state.drafts.has(other.toolCallID)).toBe(false);
  expect(send).not.toHaveBeenCalled();
  await act(async () => {
    view.result.current.dispatch({
      action: { kind: "activate", selection: { kind: "suggested", number: 1 } },
    });
  });
  expect(send).toHaveBeenCalledOnce();
  await act(async () => {
    fixture.handlers[0]?.onEvent({
      sequence: 3,
      kind: "prompt",
      payload: { state: "resolved", toolCallID: first.toolCallID },
    });
    fixture.handlers[0]?.onEvent({
      sequence: 4,
      kind: "prompt",
      payload: { state: "pending", prompt: later },
    });
    response.resolve({ results: [{ toolCallID: first.toolCallID, outcome: "skipped" }] });
    await response.promise;
  });
  await vi.waitFor(() => {
    expect(view.result.current.request.isPending).toBe(false);
  });
  expect(owner.snapshot.pendingPrompts).toEqual([later]);
  expect(view.result.current.state.current).toBe(later.toolCallID);
  view.unmount();
  await owner.dispose();
});

it("keeps later Step authority when an earlier request fails", async () => {
  const { owner, client, fixture } = setupOwner();
  const first = question();
  const later = question("later", { stepID: "423e4567-e89b-42d3-a456-426614174000" });
  fixture.handlers[0]?.onEvent({
    sequence: 1,
    kind: "hydration",
    payload: { ...hydration(), PendingPrompts: [{ state: "pending", prompt: first }] },
  });
  const response = deferred<PromptAnswerBatchResponse>();
  const send = vi.fn(async () => response.promise);
  const view = mountPicker({
    owner,
    client,
    onError: vi.fn(),
    api: { answerPromptBatch: send },
  });
  await act(async () => {
    view.result.current.dispatch({ action: { kind: "decline" } });
  });
  await vi.waitFor(() => {
    expect(send).toHaveBeenCalledOnce();
  });
  await act(async () => {
    fixture.handlers[0]?.onEvent({
      sequence: 2,
      kind: "prompt",
      payload: { state: "resolved", toolCallID: first.toolCallID },
    });
    fixture.handlers[0]?.onEvent({
      sequence: 3,
      kind: "prompt",
      payload: { state: "pending", prompt: later },
    });
    response.reject(new Error("delivery failed"));
  });
  await vi.waitFor(() => {
    expect(view.result.current.request.isError).toBe(true);
  });
  expect(owner.snapshot.pendingPrompts).toEqual([later]);
  expect(view.result.current.state.current).toBe(later.toolCallID);
  expect(send).toHaveBeenCalledOnce();
  view.unmount();
  await owner.dispose();
});

it("uses received partial acceptance after failed delivery without a repair read and preserves drafts", async () => {
  const { owner, client } = setupOwner();
  const freeform = question("freeform", { suggestions: [] });
  owner.replacePendingPrompts([question(), freeform]);
  const api = failedBatchWithFreeform(freeform);
  const send = vi.spyOn(api, "answerPromptBatch");
  const refresh = vi.spyOn(api, "listPendingPrompts");
  const onError = vi.fn();
  const view = mountPicker({
    owner,
    client,
    api,
    onError,
  });
  await act(async () => {
    view.result.current.dispatch({
      action: { kind: "activate", selection: { kind: "suggested", number: 1 } },
    });
    view.result.current.dispatch({ action: { kind: "commentary", text: "My explanation" } });
    view.result.current.dispatch({ action: { kind: "confirm" } });
  });
  await vi.waitFor(() => {
    expect(view.result.current.request.isError).toBe(true);
  });
  expect(onError).toHaveBeenCalledOnce();
  expect(refresh).not.toHaveBeenCalled();
  expect(owner.snapshot.pendingPrompts).toHaveLength(2);
  act(() => {
    owner.resolvePendingPrompts(new Set(["question-1"]));
  });
  expect(owner.snapshot.pendingPrompts.map((prompt) => prompt.toolCallID)).toEqual(["freeform"]);
  expect(view.result.current.state.drafts.get("freeform")).toMatchObject({
    commentary: "My explanation",
    status: "answered",
  });
  expect(send).toHaveBeenCalledOnce();
  act(() => {
    view.result.current.dispatch({ action: { kind: "commentary", text: "Changed explanation" } });
  });
  expect(view.result.current.state.drafts.get("freeform")?.status).toBe("tentative");
  expect(send).toHaveBeenCalledOnce();
  await act(async () => {
    view.result.current.dispatch({ action: { kind: "confirm" } });
  });
  await vi.waitFor(() => {
    expect(send).toHaveBeenCalledTimes(2);
  });
  expect(send.mock.calls[1]?.[0].entries).toEqual([
    {
      kind: "question",
      toolCallID: "freeform",
      selectedOptionNumber: null,
      freeform: "Changed explanation",
    },
  ]);
  view.unmount();
  await owner.dispose();
});

it("sends one immutable batch only after every pending member is confirmed and removes shuffled outcomes", async () => {
  const { owner, client } = setupOwner();
  const prompts = [question(), approval()];
  owner.replacePendingPrompts(prompts);
  const response = deferred<PromptAnswerBatchResponse>();
  const answerPromptBatch = vi.fn<(input: PromptAnswerBatchInput) => Promise<PromptAnswerBatchResponse>>(
    async () => response.promise,
  );
  const view = mountPicker({
    owner,
    client,
    onError: vi.fn(),
    api: { answerPromptBatch },
  });
  act(() => {
    view.result.current.dispatch({
      action: { kind: "activate", selection: { kind: "suggested", number: 2 } },
    });
  });
  expect(answerPromptBatch).not.toHaveBeenCalled();
  await act(async () => {
    view.result.current.dispatch({
      action: { kind: "activate", selection: { kind: "approval", decision: "deny" } },
    });
  });
  expect(answerPromptBatch).toHaveBeenCalledExactlyOnceWith({
    sessionID: target.sessionID,
    stepID: prompts[0]?.stepID,
    entries: [
      { kind: "question", toolCallID: "question-1", selectedOptionNumber: 2, freeform: null },
      { kind: "approval", toolCallID: "approval-1", decision: "deny", commentary: null },
    ],
  });
  expect(view.result.current.request.isPending).toBe(true);
  act(() => {
    view.result.current.dispatch({ action: { kind: "navigate", direction: -1 } });
  });
  expect(view.result.current.state.current).toBe("question-1");
  await act(async () => {
    view.result.current.dispatch({ action: { kind: "commentary", text: "Cannot edit during submission" } });
    view.result.current.dispatch({ action: { kind: "decline" } });
    view.result.current.dispatch({ action: { kind: "confirm" } });
  });
  expect(view.result.current.state.drafts.get("question-1")?.commentary).toBe("");
  expect(answerPromptBatch).toHaveBeenCalledOnce();
  expect(answerPromptBatch.mock.calls[0]?.[0].entries).toEqual([
    { kind: "question", toolCallID: "question-1", selectedOptionNumber: 2, freeform: null },
    { kind: "approval", toolCallID: "approval-1", decision: "deny", commentary: null },
  ]);
  await act(async () => {
    response.resolve({
      results: [
        { toolCallID: "approval-1", outcome: "skipped" },
        { toolCallID: "question-1", outcome: "resolved" },
      ],
    });
    await response.promise;
  });
  await vi.waitFor(() => {
    expect(owner.snapshot.pendingPrompts).toEqual([]);
  });
  expect(view.result.current.request.isPending).toBe(false);
  expect(view.result.current.state.current).toBeNull();
  view.unmount();
  await owner.dispose();
});

it("reports a failed answer after navigation without restoring a disposed owner's prompts or replaying", async () => {
  const { owner, client } = setupOwner();
  owner.replacePendingPrompts([question()]);
  const response = deferred<PromptAnswerBatchResponse>();
  const send = vi.fn(async () => response.promise);
  const onError = vi.fn();
  const view = mountPicker({
    owner,
    client,
    api: { answerPromptBatch: send },
    onError,
  });
  await act(async () => {
    view.result.current.dispatch({ action: { kind: "decline" } });
  });
  expect(send).toHaveBeenCalledOnce();
  view.unmount();
  await owner.dispose();
  response.reject(new Error("Send failed"));
  await vi.waitFor(() => {
    expect(onError).toHaveBeenCalledOnce();
  });
  expect(send).toHaveBeenCalledOnce();
  expect(owner.snapshot.disposed).toBe(true);
});
