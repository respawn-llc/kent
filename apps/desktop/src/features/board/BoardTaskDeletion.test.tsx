import { act, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { TestAppProviders, createTestServices } from "@/test-support/app-services";
import { deferred } from "@/test-support/chat-runtime";
import { useBoardTaskDeletion, useBoardTaskDeletions } from "./BoardTaskDeletion";

it("rejects same-turn duplicate deletion without blocking another Task and completes after navigation", async () => {
  const services = createTestServices([]);
  const a = deferred<undefined>();
  const b = deferred<undefined>();
  const remove = vi
    .spyOn(services.api, "deleteTask")
    .mockImplementation(async (id) => (id === "a" ? a.promise : b.promise));
  const onDeletedA = vi.fn(async () => undefined);
  const onDeletedB = vi.fn(async () => undefined);
  const onError = vi.fn();
  const { result, unmount } = renderHook(
    () => {
      const owner = useBoardTaskDeletions();
      return { a: useBoardTaskDeletion(owner, "a"), b: useBoardTaskDeletion(owner, "b") };
    },
    {
      wrapper: ({ children }: { children: ReactNode }) => (
        <TestAppProviders services={services}>{children}</TestAppProviders>
      ),
    },
  );
  act(() => {
    result.current.a.submit({ onDeleted: onDeletedA, onError });
    result.current.a.submit({ onDeleted: onDeletedA, onError });
    result.current.b.submit({ onDeleted: onDeletedB, onError });
  });
  await waitFor(() => {
    expect(remove.mock.calls).toEqual([["a"], ["b"]]);
  });
  expect(result.current.a.isPending).toBe(true);
  expect(result.current.b.isPending).toBe(true);
  await act(async () => {
    b.resolve(undefined);
  });
  await waitFor(() => {
    expect(result.current.b.isPending).toBe(false);
  });
  expect(result.current.a.isPending).toBe(true);
  expect(onDeletedB).toHaveBeenCalledOnce();
  unmount();
  await act(async () => {
    a.resolve(undefined);
  });
  await waitFor(() => {
    expect(onDeletedA).toHaveBeenCalledOnce();
  });
  expect(onError).not.toHaveBeenCalled();
});

it("keeps deletion admission when its confirmation is closed and reopened", async () => {
  const services = createTestServices([]);
  const pending = deferred<undefined>();
  const remove = vi.spyOn(services.api, "deleteTask").mockReturnValue(pending.promise);
  const completion = { onDeleted: vi.fn(), onError: vi.fn() };
  function Confirmation({ owner }: { owner: ReturnType<typeof useBoardTaskDeletions> }) {
    const deletion = useBoardTaskDeletion(owner, "a");
    return <button onClick={() => { deletion.submit(completion); }}>confirm</button>;
  }
  function Board({ confirmation }: { confirmation: boolean }) {
    const owner = useBoardTaskDeletions();
    return (
      <>
        <button onClick={() => { owner.submit({ taskID: "b", ...completion }); }}>delete B</button>
        {confirmation ? <Confirmation owner={owner} /> : null}
      </>
    );
  }
  const view = render(
    <TestAppProviders services={services}>
      <Board confirmation />
    </TestAppProviders>,
  );
  fireEvent.click(screen.getByRole("button", { name: "confirm" }));
  await waitFor(() => { expect(remove).toHaveBeenCalledOnce(); });
  view.rerender(
    <TestAppProviders services={services}>
      <Board confirmation={false} />
    </TestAppProviders>,
  );
  fireEvent.click(screen.getByRole("button", { name: "delete B" }));
  await waitFor(() => { expect(remove).toHaveBeenCalledTimes(2); });
  view.rerender(
    <TestAppProviders services={services}>
      <Board confirmation />
    </TestAppProviders>,
  );
  fireEvent.click(screen.getByRole("button", { name: "confirm" }));
  await act(async () => { pending.resolve(undefined); });
  expect(remove.mock.calls).toEqual([["a"], ["b"]]);
  expect(completion.onDeleted).toHaveBeenCalledTimes(2);
});
