import { afterEach, beforeEach, expect, it, onTestFinished } from "bun:test";
import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { appI18n } from "@/i18n";
import { approvalDecisionLabel } from "@/shared/prompt-presentation";
import { installChatStorageFixture } from "./chatStorageFixture";
import { approval, question } from "@/test-support/chat-prompts";
import { hydration } from "@/test-support/chat-runtime";
import { sessionWithPrompts } from "@/test-support/chat-destination";

let restoreStorage: (() => void) | undefined;
beforeEach(() => {
  restoreStorage = installChatStorageFixture();
});
afterEach(() => {
  restoreStorage?.();
  restoreStorage = undefined;
});

it("does not apply a trailing native double-click to a later batch question", async () => {
  const view = sessionWithPrompts();
  onTestFinished(() => {
    view.unmount();
  });
  const toolCallIDs = ["first", "second", "third"];
  const batch = { toolCallIDs, unmaterializedCount: 0 };
  const prompts = toolCallIDs.map((id) => question(id, { batch }));
  await waitFor(() => {
    expect(view.handlers).toHaveLength(1);
  });
  await act(async () => {
    view.handlers[0]?.onEvent({
      sequence: 1,
      kind: "hydration",
      payload: { ...hydration(), PendingPrompts: prompts.map((prompt) => ({ state: "pending", prompt })) },
    });
  });
  const user = userEvent.setup({ delay: null });
  const next = screen.getByRole("button", { name: appI18n.t("chat.picker.next") });
  await user.click(next);
  await user.click(next);
  fireEvent.change(await screen.findByRole("textbox", { name: appI18n.t("task.commentary") }), {
    target: { value: "third draft" },
  });
  const previous = screen.getByRole("button", { name: appI18n.t("chat.picker.previous") });
  await user.click(previous);
  await user.click(previous);

  const firstOption = screen.getByRole("radio", { name: "one" });
  fireEvent.click(firstOption, { detail: 1 });
  await waitFor(() => {
    expect(screen.getByText(appI18n.t("chat.picker.position", { current: 2, count: 3 }))).toBeInTheDocument();
  });
  const secondOption = screen.getByRole("radio", { name: "one" });
  fireEvent.click(secondOption, { detail: 2 });
  await waitFor(() => {
    expect(screen.getByText(appI18n.t("chat.picker.position", { current: 3, count: 3 }))).toBeInTheDocument();
  });
  fireEvent.doubleClick(secondOption, { detail: 2 });

  expect(await screen.findByDisplayValue("third draft")).toBeInTheDocument();
  expect(view.answer).not.toHaveBeenCalled();
});

it("requires native double-click to answer a multi-prompt choice with commentary", async () => {
  const view = sessionWithPrompts();
  onTestFinished(() => {
    view.unmount();
  });
  const toolCallIDs = ["first", "second"];
  const batch = { toolCallIDs, unmaterializedCount: 0 };
  const prompts = toolCallIDs.map((id) => question(id, { batch }));
  await waitFor(() => {
    expect(view.handlers).toHaveLength(1);
  });
  await act(async () => {
    view.handlers[0]?.onEvent({
      sequence: 1,
      kind: "hydration",
      payload: { ...hydration(), PendingPrompts: prompts.map((prompt) => ({ state: "pending", prompt })) },
    });
  });
  const user = userEvent.setup({ delay: null });
  await user.type(
    await screen.findByRole("textbox", { name: appI18n.t("task.commentary") }),
    "keep this note",
  );
  const option = screen.getByRole("radio", { name: "one" });
  await user.click(option);
  expect(view.answer).not.toHaveBeenCalled();
  expect(screen.getByText(appI18n.t("chat.picker.position", { current: 1, count: 2 }))).toBeInTheDocument();
  await user.dblClick(option);
  expect(screen.getByText(appI18n.t("chat.picker.position", { current: 2, count: 2 }))).toBeInTheDocument();
  expect(view.answer).not.toHaveBeenCalled();
});

it("quick-answers a multi-prompt batch with each empty-commentary option click", async () => {
  const view = sessionWithPrompts();
  onTestFinished(() => {
    view.unmount();
  });
  const toolCallIDs = ["first", "second", "third"];
  const batch = { toolCallIDs, unmaterializedCount: 0 };
  const prompts = toolCallIDs.map((id) => question(id, { batch }));
  await waitFor(() => {
    expect(view.handlers).toHaveLength(1);
  });
  await act(async () => {
    view.handlers[0]?.onEvent({
      sequence: 1,
      kind: "hydration",
      payload: { ...hydration(), PendingPrompts: prompts.map((prompt) => ({ state: "pending", prompt })) },
    });
  });

  const user = userEvent.setup({ delay: null });
  await user.click(await screen.findByText("two"));
  expect(screen.getByText(appI18n.t("chat.picker.position", { current: 2, count: 3 }))).toBeInTheDocument();
  expect(view.answer).not.toHaveBeenCalled();

  await user.click(screen.getByRole("radio", { name: "two" }));
  expect(screen.getByText(appI18n.t("chat.picker.position", { current: 3, count: 3 }))).toBeInTheDocument();
  expect(view.answer).not.toHaveBeenCalled();

  await user.click(screen.getByText("two"));
  await waitFor(() => {
    expect(view.answer).toHaveBeenCalledTimes(1);
  });
  expect(view.answer.mock.calls[0]?.[0].entries).toEqual([
    { toolCallID: "first", kind: "question", selectedOptionNumber: 2, freeform: null },
    { toolCallID: "second", kind: "question", selectedOptionNumber: 2, freeform: null },
    { toolCallID: "third", kind: "question", selectedOptionNumber: 2, freeform: null },
  ]);
});

it("keeps keyboard navigation in the picker when prompt navigation removes the focused option", async () => {
  const view = sessionWithPrompts();
  onTestFinished(() => {
    view.unmount();
  });
  const toolCallIDs = ["first", "second", "third"];
  const batch = { toolCallIDs, unmaterializedCount: 0 };
  const prompts = toolCallIDs.map((id) => question(id, { batch }));
  await waitFor(() => {
    expect(view.handlers).toHaveLength(1);
  });
  await act(async () => {
    view.handlers[0]?.onEvent({
      sequence: 1,
      kind: "hydration",
      payload: { ...hydration(), PendingPrompts: prompts.map((prompt) => ({ state: "pending", prompt })) },
    });
  });

  const user = userEvent.setup({ delay: null });
  await user.type(
    await screen.findByRole("textbox", { name: appI18n.t("task.commentary") }),
    "keep this note",
  );
  const option = screen.getByRole("radio", { name: "one" });
  await user.click(option);
  expect(screen.getByRole("radio", { name: "one" })).toBeChecked();
  expect(view.answer).not.toHaveBeenCalled();

  await user.keyboard("{ArrowRight}");
  expect(screen.getByText(appI18n.t("chat.picker.position", { current: 2, count: 3 }))).toBeInTheDocument();
  await user.keyboard("{ArrowRight}");
  expect(screen.getByText(appI18n.t("chat.picker.position", { current: 3, count: 3 }))).toBeInTheDocument();
  const nextButton = screen.getByRole("button", { name: appI18n.t("chat.picker.next") });
  await user.click(nextButton);
  expect(screen.getByText(appI18n.t("chat.picker.position", { current: 1, count: 3 }))).toBeInTheDocument();
  expect(nextButton).toHaveFocus();
  expect(view.answer).not.toHaveBeenCalled();
});

it.each(["Ctrl+D decline", "external resolution"] as const)(
  "keeps keyboard interaction in the picker after %s removes the focused prompt",
  async (resolution) => {
    const view = sessionWithPrompts();
    onTestFinished(() => {
      view.unmount();
    });
    const toolCallIDs = ["first", "second", "third"];
    const batch = { toolCallIDs, unmaterializedCount: 0 };
    const prompts = toolCallIDs.map((id) => question(id, { batch }));
    const firstPrompt = prompts[0];
    if (firstPrompt === undefined) throw new Error("Question batch must contain a prompt.");
    await waitFor(() => {
      expect(view.handlers).toHaveLength(1);
    });
    await act(async () => {
      view.handlers[0]?.onEvent({
        sequence: 1,
        kind: "hydration",
        payload: { ...hydration(), PendingPrompts: prompts.map((prompt) => ({ state: "pending", prompt })) },
      });
    });

    const user = userEvent.setup({ delay: null });
    await user.type(
      await screen.findByRole("textbox", { name: appI18n.t("task.commentary") }),
      "keep this note",
    );
    await user.click(screen.getByRole("radio", { name: "one" }));
    expect(view.answer).not.toHaveBeenCalled();

    if (resolution === "Ctrl+D decline") {
      await user.keyboard("{Control>}d{/Control}");
    } else {
      await act(async () => {
        view.handlers[0]?.onEvent({
          sequence: 2,
          kind: "prompt",
          payload: { state: "resolved", toolCallID: firstPrompt.toolCallID },
        });
      });
    }
    expect(
      await screen.findByText(appI18n.t("chat.picker.position", { current: 2, count: 3 })),
    ).toBeInTheDocument();

    await user.keyboard("{ArrowDown}");
    expect(screen.getByRole("radio", { name: "one" })).toBeChecked();
    await user.keyboard("{Enter}");
    expect(screen.getByText(appI18n.t("chat.picker.position", { current: 3, count: 3 }))).toBeInTheDocument();
    expect(view.answer).not.toHaveBeenCalled();
  },
);

it("starts a single-prompt recommendation unselected and treats clicks as selection only", async () => {
  const view = sessionWithPrompts();
  onTestFinished(() => {
    view.unmount();
  });
  const prompt = question("quick-answer", { recommendedOptionIndex: 1 });
  await waitFor(() => {
    expect(view.handlers).toHaveLength(1);
  });
  await act(async () => {
    view.handlers[0]?.onEvent({
      sequence: 1,
      kind: "hydration",
      payload: { ...hydration(), PendingPrompts: [{ state: "pending", prompt }] },
    });
  });
  const user = userEvent.setup({ delay: null });
  const first = await screen.findByRole("radio", { name: "one" });
  expect(first.getAttribute("aria-checked")).toBe("false");
  await user.keyboard("{Enter}");
  expect(view.answer).not.toHaveBeenCalled();
  await user.click(screen.getByText("one"));
  await waitFor(() => {
    expect(screen.getByRole("radio", { name: "one" }).getAttribute("aria-checked")).toBe("true");
  });
  expect(view.answer).not.toHaveBeenCalled();
  const second = screen.getByRole("radio", { name: "two" });
  await user.click(second);
  await waitFor(() => {
    expect(screen.getByRole("radio", { name: "two" }).getAttribute("aria-checked")).toBe("true");
  });
  await user.click(second);
  expect(view.answer).not.toHaveBeenCalled();
});

it("confirms a single-prompt suggested answer with a native double-click", async () => {
  const view = sessionWithPrompts();
  onTestFinished(() => {
    view.unmount();
  });
  const prompt = question("double-click", { recommendedOptionIndex: 1 });
  await waitFor(() => {
    expect(view.handlers).toHaveLength(1);
  });
  await act(async () => {
    view.handlers[0]?.onEvent({
      sequence: 1,
      kind: "hydration",
      payload: { ...hydration(), PendingPrompts: [{ state: "pending", prompt }] },
    });
  });
  await userEvent.setup({ delay: null }).dblClick(await screen.findByRole("radio", { name: "one" }));
  await waitFor(() => {
    expect(view.answer).toHaveBeenCalledTimes(1);
  });
  expect(view.answer.mock.calls[0]?.[0].entries).toMatchObject([{ selectedOptionNumber: 1, freeform: null }]);
});

it.each(["enter", "send"] as const)(
  "confirms the selected single-prompt answer with %s and retains commentary",
  async (method) => {
    const view = sessionWithPrompts();
    onTestFinished(() => {
      view.unmount();
    });
    const prompt = question(`confirm-${method}`, { recommendedOptionIndex: 1 });
    await waitFor(() => {
      expect(view.handlers).toHaveLength(1);
    });
    await act(async () => {
      view.handlers[0]?.onEvent({
        sequence: 1,
        kind: "hydration",
        payload: { ...hydration(), PendingPrompts: [{ state: "pending", prompt }] },
      });
    });
    const user = userEvent.setup({ delay: null });
    await user.type(
      await screen.findByRole("textbox", { name: appI18n.t("task.commentary") }),
      "keep this note",
    );
    const first = screen.getByRole("radio", { name: "one" });
    await user.click(first);
    expect(view.answer).not.toHaveBeenCalled();
    const second = screen.getByRole("radio", { name: "two" });
    await user.click(second);
    expect(view.answer).not.toHaveBeenCalled();
    await user.click(second);
    expect(view.answer).not.toHaveBeenCalled();
    expect(second.getAttribute("aria-checked")).toBe("true");
    if (method === "enter") await user.keyboard("{Enter}");
    else await user.click(screen.getByRole("button", { name: appI18n.t("chatComposer.send") }));
    await waitFor(() => {
      expect(view.answer).toHaveBeenCalledTimes(1);
    });
    expect(view.answer.mock.calls[0]?.[0].entries).toMatchObject([
      { selectedOptionNumber: 2, freeform: "keep this note" },
    ]);
  },
);

it.each(["enter", "send"] as const)(
  "confirms a selected single-prompt Approval decision with %s",
  async (method) => {
    const view = sessionWithPrompts();
    onTestFinished(() => {
      view.unmount();
    });
    const prompt = approval();
    await waitFor(() => {
      expect(view.handlers).toHaveLength(1);
    });
    await act(async () => {
      view.handlers[0]?.onEvent({
        sequence: 1,
        kind: "hydration",
        payload: { ...hydration(), PendingPrompts: [{ state: "pending", prompt }] },
      });
    });
    const user = userEvent.setup({ delay: null });
    await user.type(
      await screen.findByRole("textbox", { name: appI18n.t("task.commentary") }),
      "explain the choice",
    );
    const decision = screen.getByRole("radio", {
      name: approvalDecisionLabel("allow_once", appI18n.t),
    });
    expect(decision.getAttribute("aria-checked")).toBe("false");
    await user.click(decision);
    expect(view.answer).not.toHaveBeenCalled();
    if (method === "enter") await user.keyboard("{Enter}");
    else await user.click(screen.getByRole("button", { name: appI18n.t("chatComposer.send") }));
    await waitFor(() => {
      expect(view.answer).toHaveBeenCalledTimes(1);
    });
    expect(view.answer.mock.calls[0]?.[0].entries).toEqual([
      {
        kind: "approval",
        toolCallID: prompt.toolCallID,
        decision: "allow_once",
        commentary: "explain the choice",
      },
    ]);
  },
);

it("requires a native double-click to confirm an answer with commentary", async () => {
  const view = sessionWithPrompts();
  onTestFinished(() => {
    view.unmount();
  });
  const prompt = question("recommended-test", { recommendedOptionIndex: 1 });
  await waitFor(() => {
    expect(view.handlers).toHaveLength(1);
  });
  await act(async () => {
    view.handlers[0]?.onEvent({
      sequence: 1,
      kind: "hydration",
      payload: { ...hydration(), PendingPrompts: [{ state: "pending", prompt }] },
    });
  });
  const user = userEvent.setup({ delay: null });
  const field = await screen.findByRole("textbox", { name: appI18n.t("task.commentary") });
  const option = screen.getByRole("radio", { name: "one" });
  await user.type(field, "still writing");
  await user.click(option);
  expect(view.answer).not.toHaveBeenCalled();
  await user.type(field, " commentary");
  await user.click(option);
  expect(view.answer).not.toHaveBeenCalled();
  await user.click(option);
  expect(view.answer).not.toHaveBeenCalled();
  await user.dblClick(option);
  await waitFor(() => {
    expect(view.answer).toHaveBeenCalledTimes(1);
  });
  expect(view.answer.mock.calls[0]?.[0].entries).toMatchObject([
    { selectedOptionNumber: 1, freeform: "still writing commentary" },
  ]);
});
