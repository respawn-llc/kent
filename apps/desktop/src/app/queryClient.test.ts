import { afterEach, expect, it, vi } from "vitest";
import { focusManager, MutationObserver, onlineManager, QueryObserver } from "@tanstack/react-query";

import { createAppQueryClient } from "./queryClient";

afterEach(() => {
  vi.useRealTimers();
  focusManager.setFocused(undefined);
  onlineManager.setOnline(true);
});

it("does not refresh a stale observed read on focus or online changes", async () => {
  const client = createAppQueryClient();
  client.mount();
  const read = vi.fn(async () => "retained");
  const observer = new QueryObserver(client, {
    queryKey: ["observed-policy"],
    queryFn: read,
    staleTime: 0,
  });
  const unsubscribe = observer.subscribe(() => undefined);
  await observer.refetch();
  expect(read).toHaveBeenCalledTimes(1);

  focusManager.setFocused(false);
  onlineManager.setOnline(false);
  focusManager.setFocused(true);
  onlineManager.setOnline(true);
  await new Promise((resolve) => setTimeout(resolve, 0));

  expect(read).toHaveBeenCalledTimes(1);
  unsubscribe();
  client.unmount();
  client.clear();
});

it("settles a failed read after one attempt and permits an explicit subsequent read", async () => {
  vi.useFakeTimers();
  const client = createAppQueryClient();
  const failure = new Error("unavailable");
  const read = vi.fn().mockRejectedValueOnce(failure).mockResolvedValue("fresh");
  const options = { queryKey: ["policy"], queryFn: read };
  const result = client.fetchQuery(options).catch((error: unknown) => error);

  await vi.runAllTimersAsync();

  expect(await result).toBe(failure);
  expect(read).toHaveBeenCalledTimes(1);
  expect(await client.fetchQuery(options)).toBe("fresh");
  expect(read).toHaveBeenCalledTimes(2);
  client.clear();
});

it("attempts explicit reads and actions while offline without replay on online change", async () => {
  const client = createAppQueryClient();
  client.mount();
  onlineManager.setOnline(false);
  const read = vi.fn(async () => "read");
  const action = vi.fn(async () => "action");
  const mutation = new MutationObserver(client, { mutationFn: action });
  const readResult = client.fetchQuery({ queryKey: ["offline-policy"], queryFn: read });
  const actionResult = mutation.mutate();
  await new Promise((resolve) => setTimeout(resolve, 0));

  expect(read).toHaveBeenCalledTimes(1);
  expect(action).toHaveBeenCalledTimes(1);
  expect(await readResult).toBe("read");
  expect(await actionResult).toBe("action");
  onlineManager.setOnline(true);
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(read).toHaveBeenCalledTimes(1);
  expect(action).toHaveBeenCalledTimes(1);
  client.unmount();
  client.clear();
});
