import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import type { ChatMainView, ChatTranscriptPayloadByKind } from "@/api";
import { hydration, mainViewRead, runtimeApi, runtimeHost, target } from "@/test-support/chat-runtime";
import { ChatRuntimeOwner } from "./chatRuntime";
import { queryKeys } from "./queryKeys";

function activeHydration(count = 2): ChatTranscriptPayloadByKind["hydration"] {
  const initial = hydration();
  return {
    ...initial,
    SessionStatus: { ...initial.SessionStatus, CompactionCount: count },
    RuntimeReadModelUpdate: {
      ...initial.RuntimeReadModelUpdate,
      Activity: {
        ...initial.RuntimeReadModelUpdate.Activity,
        ActiveStep: { RunID: "run", StepID: "compact-step", ActiveKind: "compaction" },
      },
    },
  };
}

describe("admitted compaction facts", () => {
  it("projects the supplied completed count without incrementing a client counter", async () => {
    const fixture = runtimeApi();
    const client = new QueryClient();
    client.setQueryData(queryKeys.chatMainView(target.sessionID), mainViewRead().mainView);
    const owner = new ChatRuntimeOwner(fixture.api, target, client, runtimeHost());
    owner.start();
    fixture.handlers[0]?.onEvent({ sequence: 1, kind: "hydration", payload: activeHydration() });
    fixture.handlers[0]?.onEvent({
      sequence: 2,
      kind: "compaction_status",
      payload: {
        StepID: "compact-step",
        Mode: "manual",
        RequestID: "request",
        State: "completed",
        Count: 3,
      },
    });
    expect(
      client.getQueryData<ChatMainView>(queryKeys.chatMainView(target.sessionID))?.status.compactionCount,
    ).toBe(3);
    await owner.dispose();
  });
  it("delivers one pending completion on ordinary replacement idle hydration", async () => {
    const fixture = runtimeApi();
    const completed = vi.fn();
    const owner = new ChatRuntimeOwner(
      fixture.api,
      target,
      new QueryClient(),
      runtimeHost({
        onManualCompactionCompleted: completed,
      }),
    );
    owner.start();
    fixture.handlers[0]?.onEvent({ sequence: 1, kind: "hydration", payload: activeHydration() });
    fixture.handlers[0]?.onEvent({
      sequence: 2,
      kind: "compaction_status",
      payload: {
        StepID: "compact-step",
        Mode: "manual",
        RequestID: "request",
        State: "completed",
        Count: 3,
      },
    });
    expect(completed).not.toHaveBeenCalled();
    expect(fixture.handlers).toHaveLength(2);
    const replacement = activeHydration(3);
    fixture.handlers[1]?.onOpen?.();
    fixture.handlers[1]?.onEvent({
      sequence: 1,
      kind: "hydration",
      payload: {
        ...replacement,
        RuntimeReadModelUpdate: {
          Version: { Epoch: "epoch-1", Generation: 1, Sequence: 3 },
          Activity: {
            ...replacement.RuntimeReadModelUpdate.Activity,
            State: "registered_idle",
            ActiveStep: null,
          },
        },
      },
    });
    expect(completed).toHaveBeenCalledOnce();
    await owner.dispose();
  });
  it("coalesces successes through busy replacement hydration until a later idle event", async () => {
    const fixture = runtimeApi();
    const completed = vi.fn();
    const owner = new ChatRuntimeOwner(
      fixture.api,
      target,
      new QueryClient(),
      runtimeHost({
        onManualCompactionCompleted: completed,
      }),
    );
    owner.start();
    fixture.handlers[0]?.onEvent({ sequence: 1, kind: "hydration", payload: activeHydration() });
    for (let attempt = 0; attempt < 2; attempt++) {
      const handler = fixture.handlers[attempt];
      handler?.onEvent({
        sequence: 2,
        kind: "compaction_status",
        payload: {
          StepID: "compact-step",
          Mode: "manual",
          RequestID: `request-${attempt.toString()}`,
          State: "completed",
          Count: 3 + attempt,
        },
      });
      const replacement = fixture.handlers[attempt + 1];
      replacement?.onOpen?.();
      replacement?.onEvent({ sequence: 1, kind: "hydration", payload: activeHydration(3 + attempt) });
      expect(completed).not.toHaveBeenCalled();
    }
    const initial = hydration();
    fixture.handlers[2]?.onEvent({
      sequence: 2,
      kind: "runtime_read_model_update",
      payload: {
        Version: { Epoch: "epoch-1", Generation: 1, Sequence: 3 },
        Activity: { ...initial.RuntimeReadModelUpdate.Activity, State: "registered_idle", ActiveStep: null },
      },
    });
    expect(completed).toHaveBeenCalledOnce();
    fixture.handlers[2]?.onEvent({
      sequence: 3,
      kind: "runtime_read_model_update",
      payload: {
        Version: { Epoch: "epoch-1", Generation: 1, Sequence: 4 },
        Activity: { ...initial.RuntimeReadModelUpdate.Activity, State: "registered_idle", ActiveStep: null },
      },
    });
    expect(completed).toHaveBeenCalledOnce();
    await owner.dispose();
  });
  it.each([
    { mode: "auto", requestID: null },
    { mode: "handoff", requestID: null },
    { mode: "workflow_post_completion", requestID: null },
    { mode: "manual", requestID: null },
  ] as const)("does not notify for $mode completion with request $requestID", async ({ mode, requestID }) => {
    const fixture = runtimeApi();
    const completed = vi.fn();
    const owner = new ChatRuntimeOwner(
      fixture.api,
      target,
      new QueryClient(),
      runtimeHost({
        onManualCompactionCompleted: completed,
      }),
    );
    owner.start();
    fixture.handlers[0]?.onEvent({ sequence: 1, kind: "hydration", payload: activeHydration() });
    fixture.handlers[0]?.onEvent({
      sequence: 2,
      kind: "compaction_status",
      payload: {
        StepID: "compact-step",
        Mode: mode,
        RequestID: requestID,
        State: "completed",
        Count: 3,
      },
    });
    const replacement = activeHydration(3);
    fixture.handlers[1]?.onOpen?.();
    fixture.handlers[1]?.onEvent({
      sequence: 1,
      kind: "hydration",
      payload: {
        ...replacement,
        RuntimeReadModelUpdate: {
          Version: { Epoch: "epoch-1", Generation: 1, Sequence: 3 },
          Activity: {
            ...replacement.RuntimeReadModelUpdate.Activity,
            State: "registered_idle",
            ActiveStep: null,
          },
        },
      },
    });
    expect(owner.snapshot.observation.kind).toBe("observing");
    expect(completed).not.toHaveBeenCalled();
    await owner.dispose();
  });
  it("does not manufacture completion from an idle hydration's historical count", async () => {
    const fixture = runtimeApi();
    const completed = vi.fn();
    const owner = new ChatRuntimeOwner(
      fixture.api,
      target,
      new QueryClient(),
      runtimeHost({
        onManualCompactionCompleted: completed,
      }),
    );
    owner.start();
    const initial = hydration();
    fixture.handlers[0]?.onEvent({
      sequence: 1,
      kind: "hydration",
      payload: {
        ...initial,
        RuntimeReadModelUpdate: {
          ...initial.RuntimeReadModelUpdate,
          Activity: {
            ...initial.RuntimeReadModelUpdate.Activity,
            State: "registered_idle",
            ActiveStep: null,
          },
        },
      },
    });
    expect(owner.snapshot.observation.kind).toBe("observing");
    expect(completed).not.toHaveBeenCalled();
    await owner.dispose();
  });
  it("delivers a manual request failure with its typed diagnostic and no completion", async () => {
    const fixture = runtimeApi();
    const failed = vi.fn();
    const completed = vi.fn();
    const owner = new ChatRuntimeOwner(
      fixture.api,
      target,
      new QueryClient(),
      runtimeHost({
        onManualCompactionCompleted: completed,
        onManualCompactionFailed: failed,
      }),
    );
    owner.start();
    fixture.handlers[0]?.onEvent({ sequence: 1, kind: "hydration", payload: activeHydration() });
    const diagnostic = { Code: "internal_failure", Detail: "provider rejected compaction" };
    fixture.handlers[0]?.onEvent({
      sequence: 2,
      kind: "compaction_status",
      payload: {
        StepID: "compact-step",
        Mode: "manual",
        RequestID: "request",
        State: "failed",
        Count: 0,
        Diagnostic: diagnostic,
      },
    });
    expect(failed).toHaveBeenCalledExactlyOnceWith(diagnostic);
    expect(completed).not.toHaveBeenCalled();
    await owner.dispose();
  });
});
