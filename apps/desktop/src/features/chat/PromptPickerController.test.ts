import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import { ChatRuntimeOwner } from "@/app-facade";
import { hydration, runtimeApi, runtimeHost, target, deferred } from "@/test-support/chat-runtime";
import {
  question,
  approval,
  connectedConnection,
  failedBatchWithFreeform,
} from "@/test-support/chat-prompts";
import type { PromptAnswerBatchResponse } from "@/api";
import { PromptPickerController } from "./PromptPickerController";

describe("mounted Chat prompt submission", () => {
  it("discards drafts on disconnect and disposal without stopping work or replaying an in-flight answer", async () => {
    const fixture = runtimeApi();
    const owner = new ChatRuntimeOwner(fixture.api, target, new QueryClient(), runtimeHost());
    owner.start();
    const prompt = question();
    fixture.handlers[0]?.onEvent({
      sequence: 1,
      kind: "hydration",
      payload: { ...hydration(), PendingPrompts: [{ state: "pending", prompt }] },
    });
    const connection = connectedConnection();
    const response = deferred<PromptAnswerBatchResponse>();
    const send = vi.fn(async () => response.promise);
    const controller = new PromptPickerController(
      owner,
      { answerPromptBatch: send, listPendingPrompts: vi.fn() },
      target,
      { onError: vi.fn(), connection },
    );
    controller.mount();
    controller.dispatch({ kind: "commentary", text: "Transient" });
    controller.dispatch({ kind: "activate", selection: { kind: "suggested", number: 1 } });
    expect(send).toHaveBeenCalledOnce();
    connection.set("disconnected");
    expect(controller.snapshot.state.drafts.size).toBe(0);
    connection.set("connected");
    response.resolve({ results: [{ toolCallID: prompt.toolCallID, outcome: "resolved" }] });
    await response.promise;
    expect(owner.snapshot.pendingPrompts).toEqual([prompt]);
    expect(controller.snapshot.state.drafts.get(prompt.toolCallID)).toMatchObject({
      status: "tentative",
      commentary: "",
    });
    expect(send).toHaveBeenCalledOnce();
    controller.dispatch({ kind: "commentary", text: "Discard on departure" });
    controller.dispose();
    expect(controller.snapshot.state.drafts.size).toBe(0);
    expect(owner.snapshot.pendingPrompts).toEqual([prompt]);
    await owner.dispose();
  });
  it("retains declines and the last pending facts after refresh failure, then explicitly resends all-declined entries", async () => {
    const fixture = runtimeApi();
    const owner = new ChatRuntimeOwner(fixture.api, target, new QueryClient(), runtimeHost());
    owner.start();
    const prompt = question();
    fixture.handlers[0]?.onEvent({
      sequence: 1,
      kind: "hydration",
      payload: { ...hydration(), PendingPrompts: [{ state: "pending", prompt }] },
    });
    const send = vi
      .fn<() => Promise<PromptAnswerBatchResponse>>()
      .mockRejectedValueOnce(new Error("Failed"))
      .mockResolvedValueOnce({ results: [{ toolCallID: prompt.toolCallID, outcome: "skipped" }] });
    const refresh = vi.fn().mockRejectedValue(new Error("Read failed"));
    const error = vi.fn();
    const controller = new PromptPickerController(
      owner,
      { answerPromptBatch: send, listPendingPrompts: refresh },
      target,
      { onError: error, connection: connectedConnection() },
    );
    controller.mount();
    controller.dispatch({ kind: "decline" });
    await vi.waitFor(() => {
      expect(controller.snapshot.isPending).toBe(false);
    });
    expect(error).toHaveBeenCalledTimes(2);
    expect(refresh).toHaveBeenCalledOnce();
    expect(owner.snapshot.pendingPrompts).toEqual([prompt]);
    expect(controller.snapshot.state.drafts.get(prompt.toolCallID)?.status).toBe("declined");
    expect(send).toHaveBeenCalledOnce();
    controller.dispatch({ kind: "confirm" });
    await vi.waitFor(() => {
      expect(owner.snapshot.pendingPrompts).toEqual([]);
    });
    expect(send).toHaveBeenCalledTimes(2);
    controller.dispose();
    await owner.dispose();
  });

  it("removes externally resolved drafts and ignores an earlier successful response when the next Step is visible", async () => {
    const fixture = runtimeApi();
    const owner = new ChatRuntimeOwner(fixture.api, target, new QueryClient(), runtimeHost());
    owner.start();
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
    const controller = new PromptPickerController(
      owner,
      { answerPromptBatch: send, listPendingPrompts: vi.fn() },
      target,
      { onError: vi.fn(), connection: connectedConnection() },
    );
    controller.mount();
    controller.dispatch({ kind: "activate", selection: { kind: "suggested", number: 1 } });
    fixture.handlers[0]?.onEvent({
      sequence: 2,
      kind: "prompt",
      payload: { state: "resolved", toolCallID: "other" },
    });
    expect(controller.snapshot.state.current).toBe(first.toolCallID);
    expect(controller.snapshot.state.drafts.has("other")).toBe(false);
    expect(send).not.toHaveBeenCalled();
    controller.dispatch({ kind: "activate", selection: { kind: "suggested", number: 1 } });
    expect(send).toHaveBeenCalledOnce();
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
    await vi.waitFor(() => {
      expect(controller.snapshot.isPending).toBe(false);
    });
    expect(owner.snapshot.pendingPrompts).toEqual([later]);
    expect(controller.snapshot.state.current).toBe("later");
    controller.dispose();
    await owner.dispose();
  });
  it("does not replace a later Step with an earlier failed request's delayed refresh", async () => {
    const fixture = runtimeApi();
    const owner = new ChatRuntimeOwner(fixture.api, target, new QueryClient(), runtimeHost());
    owner.start();
    const first = question();
    const later = question("later", { stepID: "423e4567-e89b-42d3-a456-426614174000" });
    fixture.handlers[0]?.onEvent({
      sequence: 1,
      kind: "hydration",
      payload: { ...hydration(), PendingPrompts: [{ state: "pending", prompt: first }] },
    });
    const read = deferred<readonly (typeof first)[]>();
    const refresh = vi.fn(async () => read.promise);
    const controller = new PromptPickerController(
      owner,
      {
        answerPromptBatch: vi.fn().mockRejectedValue(new Error("Failed")),
        listPendingPrompts: refresh,
      },
      target,
      { onError: vi.fn(), connection: connectedConnection() },
    );
    controller.mount();
    controller.dispatch({ kind: "decline" });
    await vi.waitFor(() => {
      expect(refresh).toHaveBeenCalledOnce();
    });
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
    read.resolve([first]);
    await vi.waitFor(() => {
      expect(controller.snapshot.isPending).toBe(false);
    });
    expect(owner.snapshot.pendingPrompts).toEqual([later]);
    expect(controller.snapshot.state.current).toBe("later");
    controller.dispose();
    await owner.dispose();
  });
  it("refreshes partial acceptance through generated freeform reads without replay and preserves editable drafts", async () => {
    const fixture = runtimeApi();
    const owner = new ChatRuntimeOwner(fixture.api, target, new QueryClient(), runtimeHost());
    owner.start();
    const freeform = question("freeform", { suggestions: [] });
    const prompts = [question(), freeform];
    fixture.handlers[0]?.onEvent({
      sequence: 1,
      kind: "hydration",
      payload: { ...hydration(), PendingPrompts: prompts.map((prompt) => ({ state: "pending", prompt })) },
    });
    const api = failedBatchWithFreeform(freeform);
    const send = vi.spyOn(api, "answerPromptBatch");
    const refresh = vi.spyOn(api, "listPendingPrompts");
    const error = vi.fn();
    const controller = new PromptPickerController(owner, api, target, {
      onError: error,
      connection: connectedConnection(),
    });
    controller.mount();
    controller.dispatch({ kind: "activate", selection: { kind: "suggested", number: 1 } });
    controller.dispatch({ kind: "commentary", text: "My explanation" });
    controller.dispatch({ kind: "confirm" });
    await vi.waitFor(() => {
      expect(controller.snapshot.isPending).toBe(false);
    });
    expect(error).toHaveBeenCalledOnce();
    expect(refresh).toHaveBeenCalledOnce();
    expect(owner.snapshot.pendingPrompts.map((prompt) => prompt.toolCallID)).toEqual(["freeform"]);
    expect(controller.snapshot.state.drafts.get("freeform")).toMatchObject({
      commentary: "My explanation",
      status: "answered",
    });
    expect(send).toHaveBeenCalledOnce();
    controller.dispatch({ kind: "commentary", text: "Changed explanation" });
    expect(controller.snapshot.state.drafts.get("freeform")?.status).toBe("tentative");
    expect(send).toHaveBeenCalledOnce();
    controller.dispatch({ kind: "confirm" });
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
    controller.dispose();
    await owner.dispose();
  });
  it("sends one immutable batch only after every pending member is confirmed and removes shuffled outcomes", async () => {
    const fixture = runtimeApi();
    const owner = new ChatRuntimeOwner(fixture.api, target, new QueryClient(), runtimeHost());
    owner.start();
    const prompts = [question(), approval()];
    fixture.handlers[0]?.onEvent({
      sequence: 1,
      kind: "hydration",
      payload: { ...hydration(), PendingPrompts: prompts.map((prompt) => ({ state: "pending", prompt })) },
    });
    const response = deferred<PromptAnswerBatchResponse>();
    const answerPromptBatch = vi.fn(async () => response.promise);
    const listPendingPrompts = vi.fn(async () => []);
    const controller = new PromptPickerController(
      owner,
      {
        answerPromptBatch,
        listPendingPrompts,
      },
      target,
      { onError: vi.fn(), connection: connectedConnection() },
    );
    controller.mount();
    controller.dispatch({ kind: "activate", selection: { kind: "suggested", number: 2 } });
    expect(answerPromptBatch).not.toHaveBeenCalled();
    controller.dispatch({ kind: "activate", selection: { kind: "approval", decision: "deny" } });
    expect(answerPromptBatch).toHaveBeenCalledExactlyOnceWith({
      sessionID: target.sessionID,
      stepID: prompts[0]?.stepID,
      entries: [
        { kind: "question", toolCallID: "question-1", selectedOptionNumber: 2, freeform: null },
        { kind: "approval", toolCallID: "approval-1", decision: "deny", commentary: null },
      ],
    });
    expect(controller.snapshot.isPending).toBe(true);
    controller.dispatch({ kind: "navigate", direction: -1 });
    expect(controller.snapshot.state.current).toBe("question-1");
    controller.dispatch({ kind: "commentary", text: "Cannot edit during submission" });
    controller.dispatch({ kind: "decline" });
    controller.dispatch({ kind: "confirm" });
    expect(controller.snapshot.state.drafts.get("question-1")?.commentary).toBe("");
    expect(answerPromptBatch).toHaveBeenCalledOnce();
    response.resolve({
      results: [
        { toolCallID: "approval-1", outcome: "skipped" },
        { toolCallID: "question-1", outcome: "resolved" },
      ],
    });
    await vi.waitFor(() => {
      expect(owner.snapshot.pendingPrompts).toEqual([]);
    });
    expect(controller.snapshot.isPending).toBe(false);
    expect(controller.snapshot.state.current).toBeNull();
    expect(listPendingPrompts).not.toHaveBeenCalled();
    controller.dispose();
    await owner.dispose();
  });
});
