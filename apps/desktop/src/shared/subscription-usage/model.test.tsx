import { expect, test, vi } from "bun:test";
import { RegistryProvider, useAtomValue, useAtomSet } from "@effect/atom-react";
import { QueryClient } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { createTestServices } from "@/test-support/app-services";
import { deferred } from "@/test-support/chat-runtime";
import type { SubscriptionUsage, SubscriptionConnection } from "@/api";
import { createSubscriptionUsageModel } from "./model";

const usage: SubscriptionUsage = { plan: "Pro", source: "chatgpt", windows: [] };
function setup(connections: readonly SubscriptionConnection[] | Error = [{ id: "one", supported: true }]) {
  const { api } = createTestServices([]);
  const client = new QueryClient();
  const read = vi.spyOn(api, "getSubscriptionUsage");
  const binding = vi
    .spyOn(api, "getSubscriptionBinding")
    .mockResolvedValue({ connectionID: "one", supported: true });
  const catalog = vi.spyOn(api, "getSubscriptionConnections");
  if (connections instanceof Error) catalog.mockRejectedValue(connections);
  else catalog.mockResolvedValue(connections);
  const chrome = createSubscriptionUsageModel({ api, client, sessionID: null });
  const chat = createSubscriptionUsageModel({ api, client, sessionID: "session" });
  const view = renderHook(
    () => ({
      chrome: useAtomValue(chrome.state),
      chat: useAtomValue(chat.state),
      openChrome: useAtomSet(chrome.open),
      openChat: useAtomSet(chat.open),
      retryChrome: useAtomSet(chrome.retry),
      retryChat: useAtomSet(chat.retry),
    }),
    { wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider> },
  );
  return { ...view, api, client, read, catalog, binding };
}

test("unavailable Session bindings are hidden without quota reads", async () => {
  const view = setup();
  view.binding.mockResolvedValue(null);
  act(() => {
    view.result.current.openChat();
  });
  await waitFor(() => {
    expect(view.binding).toHaveBeenCalledTimes(1);
    expect(view.result.current.chat.hidden).toBe(true);
  });
  expect(view.result.current.chat.error).toBeNull();
  expect(view.read).not.toHaveBeenCalled();
});

test("two consumers share an in-flight connection read and its cached result", async () => {
  const view = setup();
  const response = deferred<SubscriptionUsage>();
  view.read.mockReturnValue(response.promise);
  act(() => {
    view.result.current.openChrome();
    view.result.current.openChat();
  });
  await waitFor(() => {
    expect(view.read).toHaveBeenCalledTimes(1);
  });
  await act(async () => {
    response.resolve(usage);
  });
  await waitFor(() => {
    expect(view.result.current.chrome.rows[0]?.data).toEqual(usage);
    expect(view.result.current.chat.rows[0]?.data).toEqual(usage);
  });
});

test("reopening explicitly refreshes without hiding cached content", async () => {
  const view = setup();
  view.read.mockResolvedValueOnce(usage);
  act(() => {
    view.result.current.openChrome();
  });
  await waitFor(() => {
    expect(view.result.current.chrome.rows[0]?.data).toEqual(usage);
  });
  const refresh = deferred<SubscriptionUsage>();
  view.read.mockReturnValueOnce(refresh.promise);
  act(() => {
    view.result.current.openChrome();
  });
  await waitFor(() => {
    expect(view.read).toHaveBeenCalledTimes(2);
  });
  expect(view.result.current.chrome.rows[0]?.data).toEqual(usage);
  await act(async () => {
    refresh.resolve({ ...usage, plan: "Plus" });
  });
  await waitFor(() => {
    expect(view.result.current.chrome.rows[0]?.data?.plan).toBe("Plus");
  });
});

test("successes appear progressively and Retry refreshes failures only", async () => {
  const view = setup([
    { id: "one", supported: true },
    { id: "two", supported: true },
    { id: "api", supported: false },
  ]);
  const slow = deferred<SubscriptionUsage>();
  view.read.mockImplementation(async (id) => {
    if (id === "one") return usage;
    return slow.promise;
  });
  act(() => {
    view.result.current.openChrome();
  });
  await waitFor(() => {
    expect(view.result.current.chrome.rows[0]?.data).toEqual(usage);
  });
  await act(async () => {
    slow.reject(new Error("Quota unavailable"));
  });
  await waitFor(() => {
    expect(view.result.current.chrome.failed).toBe(true);
  });
  view.read.mockResolvedValue(usage);
  act(() => {
    view.result.current.retryChrome(undefined);
  });
  await waitFor(() => {
    expect(view.result.current.chrome.failed).toBe(false);
  });
  expect(view.read.mock.calls.map(([id]) => id)).toEqual(["one", "two", "two"]);
});

test("Retry resolves failed prerequisites before loading their connections", async () => {
  const view = setup(new Error("Catalog unavailable"));
  view.read.mockResolvedValue(usage);
  act(() => {
    view.result.current.openChrome();
  });
  await waitFor(() => {
    expect(view.result.current.chrome.error).toBeInstanceOf(Error);
  });
  expect(view.read).not.toHaveBeenCalled();
  view.catalog.mockResolvedValue([{ id: "one", supported: true }]);
  act(() => {
    view.result.current.retryChrome(undefined);
  });
  await waitFor(() => {
    expect(view.result.current.chrome.rows[0]?.data).toEqual(usage);
  });
  view.binding.mockRejectedValueOnce(new Error("Binding unavailable"));
  act(() => {
    view.result.current.openChat();
  });
  await waitFor(() => {
    expect(view.result.current.chat.error).toBeInstanceOf(Error);
  });
  view.binding.mockResolvedValue({ connectionID: "two", supported: true });
  act(() => {
    view.result.current.retryChat(undefined);
  });
  await waitFor(() => {
    expect(view.result.current.chat.rows[0]?.id).toBe("two");
  });
  await waitFor(() => {
    expect(view.result.current.chat.rows[0]?.data).toEqual(usage);
  });
});

test("a failed refresh retains cached usage with observable diagnostics", async () => {
  const view = setup();
  view.read.mockResolvedValueOnce(usage);
  act(() => {
    view.result.current.openChrome();
  });
  await waitFor(() => {
    expect(view.result.current.chrome.rows[0]?.data).toEqual(usage);
  });
  view.read.mockRejectedValueOnce(new Error("Quota unavailable"));
  act(() => {
    view.result.current.openChrome();
  });
  await waitFor(() => {
    expect(view.result.current.chrome.failed).toBe(true);
  });
  expect(view.result.current.chrome.rows[0]?.data).toEqual(usage);
  expect(view.result.current.chrome.rows[0]?.error).toBeInstanceOf(Error);
});
