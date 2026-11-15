import { beforeEach, expect, it, mock, vi } from "bun:test";
import { BranchCleanupOutcomeKind } from "@/api";
import type { toast } from "sonner";
import { act, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import type { TaskDeleteResponse } from "@/api";

const warning = vi.fn<typeof toast.warning>();
await mock.module("sonner", () => ({
  toast: Object.assign(vi.fn(), {
    dismiss: vi.fn(),
    error: vi.fn(),
    info: vi.fn(),
    success: vi.fn(),
    warning,
  }),
  Toaster: () => null,
}));
const { TestAppProviders, createTestServices } = await import("@/test-support/app-services");
const { useBoardTaskDeletion, useBoardTaskDeletions } = await import("./BoardTaskDeletion");
const { deferred } = await import("@/test-support/chat-runtime");
const { branchCleanupOutcomeFixture } = await import("@/test-support/api");

beforeEach(() => {
  warning.mockClear();
});

it("warns about a retained branch after navigation without failing Task deletion", async () => {
  const services = createTestServices([]);
  const branchName = "kent/retained-task";
  const diagnostic = "branch is checked out elsewhere";
  const cleanup = branchCleanupOutcomeFixture(
    BranchCleanupOutcomeKind.WORKTREE_BRANCH_CLEANUP_OUTCOME_RETAINED,
    branchName,
    diagnostic,
  );
  const response = deferred<{ branchCleanup: typeof cleanup }>();
  const remove = vi.spyOn(services.api, "deleteTask").mockReturnValue(response.promise);
  const onDeleted = vi.fn();
  const onError = vi.fn();
  const { result, unmount } = renderHook(() => useBoardTaskDeletions(), {
    wrapper: ({ children }: { children: ReactNode }) => (
      <TestAppProviders services={services}>{children}</TestAppProviders>
    ),
  });
  act(() => {
    result.current.submit({ taskID: "a", onDeleted, onError });
  });
  await waitFor(() => {
    expect(remove).toHaveBeenCalledTimes(1);
  });
  unmount();
  await act(async () => {
    response.resolve({ branchCleanup: cleanup });
  });
  await waitFor(() => {
    expect(onDeleted).toHaveBeenCalledTimes(1);
    expect(warning).toHaveBeenCalledTimes(1);
  });
  expect(warning.mock.calls[0]?.[1]?.description).toContain(branchName);
  expect(warning.mock.calls[0]?.[1]?.description).toContain(diagnostic);
  expect(onError).not.toHaveBeenCalled();
});

it.each([
  undefined,
  BranchCleanupOutcomeKind.WORKTREE_BRANCH_CLEANUP_OUTCOME_NOT_REQUESTED,
  BranchCleanupOutcomeKind.WORKTREE_BRANCH_CLEANUP_OUTCOME_NOT_APPLICABLE,
  BranchCleanupOutcomeKind.WORKTREE_BRANCH_CLEANUP_OUTCOME_DELETED,
])("completes Task deletion without a warning for cleanup outcome %s", async (kind) => {
  const services = createTestServices([]);
  const response: TaskDeleteResponse = {
    branchCleanup: kind === undefined ? undefined : branchCleanupOutcomeFixture(kind),
  };
  vi.spyOn(services.api, "deleteTask").mockResolvedValue(response);
  const onDeleted = vi.fn();
  const onError = vi.fn();
  const { result } = renderHook(() => useBoardTaskDeletions(), {
    wrapper: ({ children }: { children: ReactNode }) => (
      <TestAppProviders services={services}>{children}</TestAppProviders>
    ),
  });
  act(() => {
    result.current.submit({ taskID: "a", onDeleted, onError });
  });
  await waitFor(() => {
    expect(onDeleted).toHaveBeenCalledTimes(1);
  });
  expect(warning).not.toHaveBeenCalled();
  expect(onError).not.toHaveBeenCalled();
});

it("rejects same-turn duplicate deletion without blocking another Task and completes after navigation", async () => {
  const services = createTestServices([]);
  const a = deferred<TaskDeleteResponse>();
  const b = deferred<TaskDeleteResponse>();
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
    b.resolve({});
  });
  await waitFor(() => {
    expect(result.current.b.isPending).toBe(false);
  });
  expect(result.current.a.isPending).toBe(true);
  expect(onDeletedB).toHaveBeenCalledTimes(1);
  unmount();
  await act(async () => {
    a.resolve({});
  });
  await waitFor(() => {
    expect(onDeletedA).toHaveBeenCalledTimes(1);
  });
  expect(onError).not.toHaveBeenCalled();
});

it("keeps deletion admission when its confirmation is closed and reopened", async () => {
  const services = createTestServices([]);
  const pending = deferred<TaskDeleteResponse>();
  const remove = vi.spyOn(services.api, "deleteTask").mockReturnValue(pending.promise);
  const completion = { onDeleted: vi.fn(), onError: vi.fn() };
  function Confirmation({ owner }: { owner: ReturnType<typeof useBoardTaskDeletions> }) {
    const deletion = useBoardTaskDeletion(owner, "a");
    return (
      <button
        onClick={() => {
          deletion.submit(completion);
        }}
      >
        confirm
      </button>
    );
  }
  function Board({ confirmation }: { confirmation: boolean }) {
    const owner = useBoardTaskDeletions();
    return (
      <>
        <button
          onClick={() => {
            owner.submit({ taskID: "b", ...completion });
          }}
        >
          delete B
        </button>
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
  await waitFor(() => {
    expect(remove).toHaveBeenCalledTimes(1);
  });
  view.rerender(
    <TestAppProviders services={services}>
      <Board confirmation={false} />
    </TestAppProviders>,
  );
  fireEvent.click(screen.getByRole("button", { name: "delete B" }));
  await waitFor(() => {
    expect(remove).toHaveBeenCalledTimes(2);
  });
  view.rerender(
    <TestAppProviders services={services}>
      <Board confirmation />
    </TestAppProviders>,
  );
  fireEvent.click(screen.getByRole("button", { name: "confirm" }));
  await act(async () => {
    pending.resolve({});
  });
  expect(remove.mock.calls).toEqual([["a"], ["b"]]);
  expect(completion.onDeleted).toHaveBeenCalledTimes(2);
});
