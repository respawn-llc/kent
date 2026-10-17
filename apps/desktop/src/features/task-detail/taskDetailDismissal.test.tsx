import { act, renderHook } from "@testing-library/react";
import { expect, it, vi } from "bun:test";

import { useExactTaskDetailDeleteDismissal } from "./taskDetailDismissal";

it("dismisses the current standalone Task host and propagates navigation failure", async () => {
  const dismiss = vi.fn(async () => undefined);
  const { result } = renderHook(() => useExactTaskDetailDeleteDismissal("task-1", dismiss));

  expect(result.current()).resolves.toEqual({ kind: "accepted" });
  expect(dismiss).toHaveBeenCalledTimes(1);

  dismiss.mockRejectedValueOnce(new Error("navigation failed"));
  const failed = await result.current();
  expect(failed.kind).toBe("failed");
  if (failed.kind !== "failed") {
    throw new Error("Expected failed dismissal.");
  }
  expect(failed.error).toBeInstanceOf(Error);
  if (!(failed.error instanceof Error)) {
    throw new Error("Expected Error dismissal cause.");
  }
  expect(failed.error.message).toBe("navigation failed");
});

it("returns stale after the native Task host is replaced or released", async () => {
  const dismiss = vi.fn(async () => undefined);
  const { result, rerender, unmount } = renderHook(
    ({ taskID }) => useExactTaskDetailDeleteDismissal(taskID, dismiss),
    { initialProps: { taskID: "task-1" } },
  );
  const replacedHostDismissal = result.current;

  rerender({ taskID: "task-2" });
  expect(replacedHostDismissal()).resolves.toEqual({ kind: "stale" });
  expect(dismiss).not.toHaveBeenCalled();

  const releasedHostDismissal = result.current;
  act(() => {
    unmount();
  });
  expect(releasedHostDismissal()).resolves.toEqual({ kind: "stale" });
  expect(dismiss).not.toHaveBeenCalled();
});

it("returns stale when the exact host changes while dismissal is pending", async () => {
  let resolveDismiss: (() => void) | undefined;
  let rejectDismiss: ((error: Error) => void) | undefined;
  const dismiss = vi
    .fn<() => Promise<void>>()
    .mockImplementationOnce(
      async () =>
        new Promise<void>((resolve) => {
          resolveDismiss = resolve;
        }),
    )
    .mockImplementationOnce(
      async () =>
        new Promise<void>((_, reject) => {
          rejectDismiss = reject;
        }),
    );
  const { result, rerender, unmount } = renderHook(
    ({ taskID }) => useExactTaskDetailDeleteDismissal(taskID, dismiss),
    { initialProps: { taskID: "task-1" } },
  );

  const replacedResult = result.current();
  rerender({ taskID: "task-2" });
  resolveDismiss?.();
  expect(replacedResult).resolves.toEqual({ kind: "stale" });

  const releasedResult = result.current();
  unmount();
  rejectDismiss?.(new Error("navigation failed"));
  expect(releasedResult).resolves.toEqual({ kind: "stale" });
});
