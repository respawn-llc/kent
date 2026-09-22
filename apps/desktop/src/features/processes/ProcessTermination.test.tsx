import { RegistryProvider, useAtomMount, useAtomValue, useAtomSet } from "@effect/atom-react";
import { QueryClient } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";

import { createTestServices } from "@/test-support/app-services";
import { deferred } from "@/test-support/chat-runtime";
import { createProcessTermination } from "./ProcessesViewModel";

it("ends accepted request loading at settlement without a repair read and admits different processes independently", async () => {
  const { api } = createTestServices([]);
  const client = new QueryClient();
  const response = deferred<undefined>();
  const kill = vi.spyOn(api, "killProcess").mockReturnValue(response.promise);
  const read = vi.spyOn(api, "listProcesses");
  const first = createProcessTermination({ api, client, processID: "a", onError: vi.fn() });
  const second = createProcessTermination({ api, client, processID: "b", onError: vi.fn() });
  const view = renderHook(
    () => {
      useAtomMount(first.request);
      useAtomMount(second.request);
      return {
        pending: useAtomValue(first.pending),
        terminate: useAtomSet(first.terminate),
        other: useAtomSet(second.terminate),
      };
    },
    { wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider> },
  );
  await act(async () => {
    view.result.current.terminate(undefined);
    view.result.current.terminate(undefined);
    view.result.current.other(undefined);
  });
  expect(kill).toHaveBeenCalledTimes(2);
  expect(kill).toHaveBeenCalledWith("a");
  expect(kill).toHaveBeenCalledWith("b");
  expect(view.result.current.pending).toBe(true);
  await act(async () => {
    response.resolve(undefined);
  });
  await waitFor(() => {
    expect(view.result.current.pending).toBe(false);
  });
  expect(read).not.toHaveBeenCalled();
});

it("reports a rejected request after panel close and shares its pending state with a remounted row", async () => {
  const { api } = createTestServices([]);
  const client = new QueryClient();
  const response = deferred<undefined>();
  const kill = vi
    .spyOn(api, "killProcess")
    .mockReturnValueOnce(response.promise)
    .mockResolvedValue(undefined);
  const read = vi.spyOn(api, "listProcesses");
  const onError = vi.fn();
  const model = () => createProcessTermination({ api, client, processID: "a", onError });
  const mount = () => {
    const current = model();
    return renderHook(
      () => {
        useAtomMount(current.request);
        return { pending: useAtomValue(current.pending), terminate: useAtomSet(current.terminate) };
      },
      { wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider> },
    );
  };
  const view = mount();
  await act(async () => {
    view.result.current.terminate(undefined);
  });
  view.unmount();
  const { result } = mount();
  expect(result.current.pending).toBe(true);
  await act(async () => {
    result.current.terminate(undefined);
  });
  expect(kill).toHaveBeenCalledTimes(1);
  const failure = new Error("rejected");
  await act(async () => {
    response.reject(failure);
  });
  await waitFor(() => {
    expect(onError.mock.calls[0]?.[0]).toBe(failure);
  });
  await waitFor(() => {
    expect(result.current.pending).toBe(false);
  });
  await act(async () => {
    result.current.terminate(undefined);
  });
  expect(kill).toHaveBeenCalledTimes(2);
  expect(read).not.toHaveBeenCalled();
});
