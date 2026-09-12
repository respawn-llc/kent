import { RegistryProvider, useAtomValue } from "@effect/atom-react";
import { QueryClient } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";

import { ChatOperationError, RpcError } from "@/api";
import { appI18n } from "@/i18n";
import { createTestServices } from "@/test-support/app-services";
import { deferred, target } from "@/test-support/chat-runtime";
import { row } from "@/test-support/transcript-window";
import { settingsOperationFailureMessage } from "../chatSettingsPresentation";
import { createChatMessageEditViewModel, useChatMessageEditActions } from "./ChatMessageEditViewModel";
import type { ChatUserMessageItem } from "./ChatUserMessage";

function userItem(rollbackTargetID: string | null = "rollback-token"): ChatUserMessageItem {
  const committed = row(1);
  const value = { Text: "**Original**\n", RollbackTargetID: rollbackTargetID };
  return { kind: "user", state: "committed", key: "1:1", row: { ...committed, User: value }, value };
}

function setup() {
  const services = createTestServices([]);
  const response = deferred<string>();
  const fork = vi.spyOn(services.api.chat, "forkEdit").mockReturnValue(response.promise);
  const client = new QueryClient();
  const push = vi.fn();
  const onSuccess = vi.fn();
  const model = createChatMessageEditViewModel({
    api: services.api.chat,
    client,
    target,
    t: appI18n.t,
    push,
  });
  const view = renderHook(
    () => ({
      request: useAtomValue(model.request),
      ...useChatMessageEditActions(model, "available"),
    }),
    { wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider> },
  );
  return { ...view, model, fork, response, push, onSuccess };
}

it("does not activate Edit without a server rollback target", async () => {
  const view = setup();
  await act(async () => {
    view.result.current.activate({ item: userItem(null), draft: "", onSuccess: view.onSuccess });
  });
  expect(view.fork).not.toHaveBeenCalled();
  expect(view.onSuccess).not.toHaveBeenCalled();
});

it.each(["", " \n ", "Keep **this** draft\n"])(
  "forks with original text and verbatim draft %j",
  async (draft) => {
    const view = setup();
    const item = userItem();
    await act(async () => {
      view.result.current.activate({ item, draft, onSuccess: view.onSuccess });
    });
    expect(view.fork).toHaveBeenCalledWith(target, {
      rollbackTargetID: item.value.RollbackTargetID,
      initialInput: draft === "" ? item.value.Text : `${item.value.Text}\n\n${draft}`,
    });
    await act(async () => {
      view.response.resolve("child-session");
    });
  },
);

it("hands the returned child and captured draft to the activation callback", async () => {
  const view = setup();
  const input = { item: userItem(), draft: "parent draft", onSuccess: view.onSuccess };
  await act(async () => {
    view.result.current.activate(input);
  });
  input.draft = "later draft";
  input.onSuccess = vi.fn();
  await act(async () => {
    view.response.resolve("authoritative-child");
  });
  expect(view.onSuccess).toHaveBeenCalledExactlyOnceWith({
    sessionID: "authoritative-child",
    draft: "**Original**\n\n\nparent draft",
  });
  expect(input.onSuccess).not.toHaveBeenCalled();
});

it("admits one pending Edit even when activated twice in the same turn", async () => {
  const view = setup();
  const input = { item: userItem(), draft: "existing", onSuccess: view.onSuccess };
  await act(async () => {
    view.result.current.activate(input);
    view.result.current.activate(input);
  });
  expect(view.fork).toHaveBeenCalledTimes(1);
  expect(view.result.current.request.isPending).toBe(true);
  await act(async () => {
    view.response.resolve("child");
  });
  expect(view.result.current.request.isSuccess).toBe(true);
});

it("reports failure without a child handoff and allows another explicit Edit", async () => {
  const view = setup();
  const failure = new ChatOperationError(
    new RpcError({
      code: -32603,
      message: "Store unavailable",
      method: "session.resolve_transition",
    }),
    { kind: "internal_failure", operation: "fork", cause: "Store unavailable" },
  );
  const input = { item: userItem(), draft: "unchanged parent", onSuccess: view.onSuccess };
  await act(async () => {
    view.result.current.activate(input);
  });
  await act(async () => {
    view.response.reject(failure);
    await view.response.promise.catch(() => undefined);
  });
  expect(view.result.current.request.error).toBe(failure);
  expect(view.onSuccess).not.toHaveBeenCalled();
  expect(view.push).toHaveBeenCalledWith(
    expect.objectContaining({
      tone: "danger",
      body: settingsOperationFailureMessage(appI18n.t, failure),
    }),
  );
  expect(input.draft).toBe("unchanged parent");
  view.fork.mockResolvedValue("retried-child");
  await act(async () => {
    view.result.current.activate(input);
  });
  expect(view.fork).toHaveBeenCalledTimes(2);
  expect(view.onSuccess).toHaveBeenCalledExactlyOnceWith({
    sessionID: "retried-child",
    draft: "**Original**\n\n\nunchanged parent",
  });
});
