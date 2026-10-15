import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ChatInputMutationResult } from "@/api";
import { appI18n } from "@/i18n";
import { hydration, target } from "@/test-support/chat-runtime";
import { sessionWithPrompts } from "@/test-support/chat-destination";
import { installAnimatedHeightGeometry } from "@/test-support/resize-observer";
import { row } from "@/test-support/transcript-window";

it("releases a busy submission after two seconds and keeps subsequent typing through late acceptance", async () => {
  let respond!: (response: ChatInputMutationResult) => void;
  const response = new Promise<ChatInputMutationResult>((resolve) => {
    respond = resolve;
  });
  const view = sessionWithPrompts((services) => {
    vi.spyOn(services.api.chat, "steer").mockReturnValue(response);
  });
  try {
    const editor = await screen.findByRole("textbox");
    await waitFor(() => {
      expect(view.handlers).toHaveLength(1);
    });
    await act(async () => {
      view.handlers[0]?.onEvent({ sequence: 1, kind: "hydration", payload: hydration() });
    });
    const user = userEvent.setup();
    await user.type(editor, "Busy submission");
    await user.click(screen.getByRole("button", { name: appI18n.t("chatComposer.send") }));
    expect(editor).toHaveValue("Busy submission");
    expect(editor).toHaveAttribute("readonly");
    await waitFor(
      () => {
        expect(editor).toHaveValue("");
        expect(editor).not.toHaveAttribute("readonly");
      },
      { timeout: 3000 },
    );
    expect(await screen.findByText("Busy submission")).toBeInTheDocument();
    await user.type(editor, "next draft");
    const inputID = vi.mocked(view.services.api.chat.steer).mock.calls[0]?.[2];
    if (inputID === undefined) throw new Error("Missing submitted identity");
    await act(async () => {
      respond({
        sessionID: target.sessionID,
        outcome: { kind: "accepted", queueItemID: inputID, diagnostic: null, disposition: "pending_steer" },
      });
    });
    expect(editor).toHaveValue("next draft");
    expect(screen.getAllByText("Busy submission")).toHaveLength(1);
  } finally {
    view.unmount();
  }
}, 15000);

it("shows an idle submission before its response and adopts commitment once without losing new typing", async () => {
  const geometry = installAnimatedHeightGeometry(80);
  let respond!: (response: ChatInputMutationResult) => void;
  const response = new Promise<ChatInputMutationResult>((resolve) => {
    respond = resolve;
  });
  const view = sessionWithPrompts((services) => {
    vi.spyOn(services.api.chat, "steer").mockReturnValue(response);
  });
  try {
    const editor = await screen.findByRole("textbox");
    await waitFor(() => {
      expect(view.handlers).toHaveLength(1);
    });
    await act(async () => {
      const initial = hydration();
      view.handlers[0]?.onEvent({
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
    });
    fireEvent.change(editor, { target: { value: "Optimistic message" } });
    fireEvent.click(screen.getByRole("button", { name: appI18n.t("chatComposer.send") }));
    act(() => {
      geometry.resize(80);
    });
    const list = await screen.findByRole("list");
    expect(await within(list).findByText("Optimistic message")).toBeInTheDocument();
    expect(editor).toHaveValue("");
    expect(editor).not.toHaveAttribute("readonly");
    fireEvent.change(editor, { target: { value: "next draft" } });
    const inputID = vi.mocked(view.services.api.chat.steer).mock.calls[0]?.[2];
    if (inputID === undefined) throw new Error("Missing submitted identity");
    await act(async () => {
      view.handlers[0]?.onEvent({
        sequence: 2,
        kind: "committed_row",
        payload: { ...row(1), User: { Text: "Optimistic message", QueueItemIDs: [inputID.toJSONValue()] } },
      });
      respond({
        sessionID: target.sessionID,
        outcome: { kind: "accepted", queueItemID: inputID, diagnostic: null, disposition: "pending_steer" },
      });
    });
    await waitFor(() => {
      expect(within(list).getAllByText("Optimistic message")).toHaveLength(1);
    });
    expect(editor).toHaveValue("next draft");
  } finally {
    view.unmount();
    geometry.restore();
  }
});
