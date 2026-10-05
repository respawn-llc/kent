import { QueryClient } from "@tanstack/react-query";
import { waitFor } from "@testing-library/react";
import { ChatRuntimeOwner } from "@/app-facade";
import { createTestServices } from "@/test-support/app-services";
import { hydration, runtimeApi, runtimeHost, target } from "@/test-support/chat-runtime";
import { chatCompactionFeedback } from "./chatCompactionFeedback";
import * as ui from "@/ui";

it.each([false, true])(
  "notifies after replacement idle hydration only when unfocused (focused=%s)",
  async (focused) => {
    const services = createTestServices([]);
    vi.spyOn(services.nativeBridge.window, "isFocused").mockResolvedValue(focused);
    vi.spyOn(services.nativeBridge.notifications, "permissionState").mockResolvedValue("granted");
    const notify = vi.spyOn(services.nativeBridge.notifications, "notify").mockResolvedValue();
    const fixture = runtimeApi();
    const host = chatCompactionFeedback(services, target);
    const owner = new ChatRuntimeOwner(fixture.api, target, new QueryClient(), runtimeHost(host));
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
            ActiveStep: { RunID: "run", StepID: "step", ActiveKind: "compaction" },
          },
        },
      },
    });
    fixture.handlers[0]?.onEvent({
      sequence: 2,
      kind: "compaction_status",
      payload: {
        StepID: "step",
        RequestID: "request",
        Mode: "manual",
        Count: 3,
        State: "completed",
      },
    });
    expect(notify).not.toHaveBeenCalled();
    fixture.handlers[1]?.onOpen?.();
    fixture.handlers[1]?.onEvent({
      sequence: 1,
      kind: "hydration",
      payload: {
        ...initial,
        SessionStatus: { ...initial.SessionStatus, CompactionCount: 3 },
        RuntimeReadModelUpdate: {
          Version: { Epoch: "epoch-1", Generation: 1, Sequence: 3 },
          Activity: {
            ...initial.RuntimeReadModelUpdate.Activity,
            State: "registered_idle",
            ActiveStep: null,
          },
        },
      },
    });
    await waitFor(() => {
      expect(services.nativeBridge.window.isFocused).toHaveBeenCalledOnce();
    });
    if (focused) expect(notify).not.toHaveBeenCalled();
    else
      await waitFor(() => {
        expect(notify).toHaveBeenCalledExactlyOnceWith(
          expect.objectContaining({
            target: { kind: "session_prompt", projectID: target.projectID, sessionID: target.sessionID },
          }),
        );
      });
    await owner.dispose();
  },
);

it("surfaces terminal manual failure through the ordinary status facility", () => {
  const services = createTestServices([]);
  const notice = vi.spyOn(ui, "showStatusToast").mockImplementation(() => undefined);
  const diagnostic = { Code: "internal_failure", Detail: "provider unavailable" };
  chatCompactionFeedback(services, target).onManualCompactionFailed?.(diagnostic);
  expect(notice).toHaveBeenCalledWith(
    expect.objectContaining({
      tone: "danger",
      body: diagnostic.Detail,
    }),
  );
});
