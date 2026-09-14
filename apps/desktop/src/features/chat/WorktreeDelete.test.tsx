import { RegistryProvider, useAtomValue } from "@effect/atom-react";
import { QueryClient } from "@tanstack/react-query";
import { act, render, renderHook, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { DirtyStateKind } from "@/api";

import { appI18n } from "@/i18n";
import {
  worktreeQueryFixtureRoutes,
  worktreeDeleteSuccessFixture,
  worktreeErrorFixture,
} from "@/test-support/api";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { createWorktreeDelete, useWorktreeDelete } from "./WorktreeDelete";
import { WorktreeDeleteButton } from "./WorktreeDeleteButton";
import { deferred } from "@/test-support/chat-runtime";

it("confirms the decoded fresh preview and refreshes after accepted deletion", async () => {
  const services = createTestServices(worktreeQueryFixtureRoutes());
  const send = vi.spyOn(services.api, "deleteWorktree");
  const close = vi.fn();
  const refresh = vi.fn();
  const model = createWorktreeDelete({
    client: new QueryClient(),
    api: services.api,
    sessionID: "session-1",
    selector: "feature",
    close,
    refreshOpenWorktreeList: refresh,
    push: vi.fn(),
    t: appI18n.t,
  });
  const view = renderHook(
    () => ({
      preview: useAtomValue(model.preview),
      deletion: useAtomValue(model.deletion),
      ...useWorktreeDelete(model),
    }),
    { wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider> },
  );
  await waitFor(() => {
    expect(view.result.current.preview.isSuccess).toBe(true);
  });
  const preview = view.result.current.preview.data;
  await act(async () => {
    view.result.current.confirm("confirm");
  });
  await waitFor(() => {
    expect(view.result.current.deletion.isSuccess).toBe(true);
  });
  expect(send).toHaveBeenCalledWith("session-1", preview, "confirm");
  expect(close).toHaveBeenCalledTimes(1);
  expect(refresh).toHaveBeenCalledWith("session-1");
});

it.each([
  { cleanliness: { kind: DirtyStateKind.DIRTY_STATE_CLEAN }, detached: false },
  {
    cleanliness: { kind: DirtyStateKind.DIRTY_STATE_DIRTY, dirtyFileCount: 3 },
    detached: false,
  },
  {
    cleanliness: { kind: DirtyStateKind.DIRTY_STATE_UNKNOWN, unknownCause: "Unavailable" },
    detached: true,
  },
])(
  "uses informed confirmation for cleanliness $cleanliness.kind with detached=$detached",
  async ({ cleanliness, detached }) => {
    const services = createTestServices(worktreeQueryFixtureRoutes({ cleanliness, detached }));
    const send = vi.spyOn(services.api, "deleteWorktree");
    render(
      <TestAppProviders services={services}>
        <RegistryProvider>
          <WorktreeDeleteButton sessionID="session-1" selector="feature" refreshOpenWorktreeList={vi.fn()} />
        </RegistryProvider>
      </TestAppProviders>,
    );
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: appI18n.t("chat.worktree.delete") }));
    const confirm = await screen.findByRole("button", { name: appI18n.t("chat.worktree.confirm") });
    expect(screen.queryAllByRole("button", { name: appI18n.t("chat.worktree.confirmBranch") })).toHaveLength(
      detached ? 0 : 1,
    );
    await user.click(confirm);
    await waitFor(() => {
      expect(send).toHaveBeenCalledTimes(1);
      expect(send.mock.calls[0]?.[1].cleanliness?.kind).toBe(cleanliness.kind);
    });
  },
);

it.each([false, true])(
  "keeps immediate failure inline only while observed (dismissed=%s)",
  async (dismissed) => {
    const services = createTestServices(worktreeQueryFixtureRoutes());
    const response = deferred<Awaited<ReturnType<typeof services.api.deleteWorktree>>>();
    vi.spyOn(services.api, "deleteWorktree").mockReturnValue(response.promise);
    const push = vi.fn();
    const model = createWorktreeDelete({
      client: new QueryClient(),
      api: services.api,
      sessionID: "session-1",
      selector: "feature",
      close: vi.fn(),
      refreshOpenWorktreeList: vi.fn(),
      push,
      t: appI18n.t,
    });
    const view = renderHook(
      () => ({
        preview: useAtomValue(model.preview),
        deletion: useAtomValue(model.deletion),
        ...useWorktreeDelete(model),
      }),
      { wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider> },
    );
    await waitFor(() => {
      expect(view.result.current.preview.isSuccess).toBe(true);
    });
    await act(async () => {
      view.result.current.confirm("confirm");
    });
    if (dismissed) {
      vi.useFakeTimers();
      view.unmount();
      await act(async () => vi.advanceTimersByTimeAsync(500));
      vi.useRealTimers();
    }
    await act(async () => {
      response.reject(new Error("Delete rejected"));
      await response.promise.catch(() => undefined);
    });
    expect(push).toHaveBeenCalledTimes(dismissed ? 1 : 0);
    if (!dismissed)
      await waitFor(() => {
        expect(view.result.current.deletion.isError).toBe(true);
      });
  },
);

it("reports retained branch and root cleanup together once", async () => {
  const services = createTestServices(worktreeQueryFixtureRoutes());
  vi.spyOn(services.api, "deleteWorktree").mockResolvedValue(
    worktreeDeleteSuccessFixture({
      retainedBranch: "feature",
      diagnostic: "In use",
      leftoverRoot: "/repo/feature",
    }),
  );
  const push = vi.fn();
  const model = createWorktreeDelete({
    client: new QueryClient(),
    api: services.api,
    sessionID: "session-1",
    selector: "feature",
    close: vi.fn(),
    refreshOpenWorktreeList: vi.fn(),
    push,
    t: appI18n.t,
  });
  const view = renderHook(() => ({ preview: useAtomValue(model.preview), ...useWorktreeDelete(model) }), {
    wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider>,
  });
  await waitFor(() => {
    expect(view.result.current.preview.isSuccess).toBe(true);
  });
  await act(async () => {
    view.result.current.confirm("confirm_and_branch");
  });
  await waitFor(() => {
    expect(push).toHaveBeenCalledTimes(1);
  });
  expect(push).toHaveBeenCalledWith(expect.objectContaining({ tone: "warning" }));
});

it("leaves only Close after preview failure and never authorizes deletion", async () => {
  const services = createTestServices(worktreeQueryFixtureRoutes());
  vi.spyOn(services.api, "previewWorktreeDelete").mockRejectedValue(new Error("Preview failed"));
  const send = vi.spyOn(services.api, "deleteWorktree");
  render(
    <TestAppProviders services={services}>
      <RegistryProvider>
        <WorktreeDeleteButton sessionID="session-1" selector="feature" refreshOpenWorktreeList={vi.fn()} />
      </RegistryProvider>
    </TestAppProviders>,
  );
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: appI18n.t("chat.worktree.delete") }));
  await screen.findByText("Preview failed");
  expect(screen.queryByRole("button", { name: appI18n.t("chat.worktree.confirm") })).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: appI18n.t("app.close") }));
  expect(send).not.toHaveBeenCalled();
});

it("refreshes a rejected Clean preview and requires a new confirmation", async () => {
  const services = createTestServices(worktreeQueryFixtureRoutes());
  const failure = worktreeErrorFixture("delete_precondition");
  const send = vi.spyOn(services.api, "deleteWorktree").mockRejectedValueOnce(failure);
  const push = vi.fn();
  const model = createWorktreeDelete({
    client: new QueryClient(),
    api: services.api,
    sessionID: "session-1",
    selector: "feature",
    close: vi.fn(),
    refreshOpenWorktreeList: vi.fn(),
    push,
    t: appI18n.t,
  });
  const view = renderHook(() => ({ preview: useAtomValue(model.preview), ...useWorktreeDelete(model) }), {
    wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider>,
  });
  await waitFor(() => {
    expect(view.result.current.preview.isSuccess).toBe(true);
  });
  await act(async () => {
    view.result.current.confirm("confirm");
  });
  await waitFor(() => {
    expect(view.result.current.preview.data?.cleanliness?.kind).toBe(DirtyStateKind.DIRTY_STATE_DIRTY);
  });
  expect(send).toHaveBeenCalledTimes(1);
  expect(push).not.toHaveBeenCalled();
  await act(async () => {
    view.result.current.confirm("confirm");
  });
  await waitFor(() => {
    expect(send).toHaveBeenCalledTimes(2);
  });
});
