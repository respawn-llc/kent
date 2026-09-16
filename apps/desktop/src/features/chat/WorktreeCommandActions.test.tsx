import { act, renderHook, waitFor } from "@testing-library/react";
import { RegistryProvider, useAtomValue } from "@effect/atom-react";
import { QueryClient } from "@tanstack/react-query";
import { createTestServices } from "@/test-support/app-services";
import { worktreeCommandFixture as fixture, worktreeCommandFixtureRoutes } from "@/test-support/api";
import { appI18n } from "@/i18n";
import type { WorktreeDeletePreview } from "@/api";
import { useWorktreeActions } from "./WorktreeActions";
import { createWorktreeCommandActions } from "./WorktreeCommandActions";
import { createWorktreeCommandDelete, useWorktreeCommandDelete } from "./WorktreeCommandDelete";
import { deferred } from "@/test-support/chat-runtime";

it.each([false, true])("resolves a command selector before Switch, current=%s", async (isCurrent) => {
  const services = createTestServices(worktreeCommandFixtureRoutes(isCurrent));
  const push = vi.fn();
  const open = vi.fn();
  const model = createWorktreeCommandActions({
    client: new QueryClient(),
    api: services.api,
    sessionID: "session",
    roots: { open },
    focusComposer: vi.fn(),
    deleteTarget: vi.fn(),
    refreshOpenWorktreeList: vi.fn(),
    push,
    t: appI18n.t,
  });
  renderHook(() => useWorktreeActions(model.transitions), { wrapper: RegistryProvider });
  vi.spyOn(crypto, "randomUUID").mockReturnValue(fixture.operationID);
  try {
    await act(async () => {
      await model.execute({ kind: "switch", selector: "topic" });
    });
    expect(open).not.toHaveBeenCalled();
    expect(push).not.toHaveBeenCalled();
    expect(services.transport.descriptorCalls).toMatchObject([
      { descriptor: fixture.methods.resolve, request: { sessionId: "session", selector: "topic" } },
      {
        descriptor: fixture.methods.enter,
        request: { sessionId: "session", selector: fixture.selector, operationId: fixture.operationID },
      },
    ]);
  } finally {
    vi.restoreAllMocks();
  }
});

it("dispatches Leave without resolving or listing targets", async () => {
  const services = createTestServices(worktreeCommandFixtureRoutes());
  const push = vi.fn();
  const open = vi.fn();
  const model = createWorktreeCommandActions({
    client: new QueryClient(),
    api: services.api,
    sessionID: "session",
    roots: { open },
    focusComposer: vi.fn(),
    deleteTarget: vi.fn(),
    refreshOpenWorktreeList: vi.fn(),
    push,
    t: appI18n.t,
  });
  renderHook(() => useWorktreeActions(model.transitions), { wrapper: RegistryProvider });
  vi.spyOn(crypto, "randomUUID").mockReturnValue(fixture.operationID);
  try {
    await act(async () => {
      await model.execute({ kind: "leave" });
    });
    expect(open).not.toHaveBeenCalled();
    expect(push).not.toHaveBeenCalled();
    expect(services.transport.descriptorCalls).toHaveLength(1);
    expect(services.transport.descriptorCalls[0]).toMatchObject({
      descriptor: fixture.methods.leave,
      request: { sessionId: "session", operationId: fixture.operationID },
    });
  } finally {
    vi.restoreAllMocks();
  }
});

it.each(["resolveWorktreeSelector", "switchWorktree"] as const)(
  "admits Leave independently while Switch is pending at %s",
  async (method) => {
    const services = createTestServices(worktreeCommandFixtureRoutes());
    const gate = deferred<undefined>();
    const resolveSelector = services.api.resolveWorktreeSelector.bind(services.api);
    const transition = services.api.switchWorktree.bind(services.api);
    const held =
      method === "resolveWorktreeSelector"
        ? vi.spyOn(services.api, method).mockImplementationOnce(async (...args) => {
            const result = await resolveSelector(...args);
            await gate.promise;
            return result;
          })
        : vi.spyOn(services.api, method).mockImplementationOnce(async (...args) => {
            const result = await transition(...args);
            await gate.promise;
            return result;
          });
    const push = vi.fn();
    const model = createWorktreeCommandActions({
      client: new QueryClient(),
      api: services.api,
      sessionID: "session",
      roots: { open: vi.fn() },
      focusComposer: vi.fn(),
      deleteTarget: vi.fn(),
      refreshOpenWorktreeList: vi.fn(),
      push,
      t: appI18n.t,
    });
    const { result: pending } = renderHook(
      () => {
        useWorktreeActions(model.transitions);
        return useAtomValue(model.transitions.requestPending);
      },
      { wrapper: RegistryProvider },
    );
    vi.spyOn(crypto, "randomUUID").mockReturnValue(fixture.operationID);
    let first!: Promise<void>;
    try {
      act(() => {
        first = model.execute({ kind: "switch", selector: "topic" });
      });
      await waitFor(() => {
        expect(held).toHaveBeenCalledOnce();
      });
      await act(async () => {
        await model.execute({ kind: "leave" });
      });
      const leave = services.transport.descriptorCalls.find(
        ({ descriptor }) => descriptor === fixture.methods.leave,
      );
      expect(leave?.request).toMatchObject({ sessionId: "session" });
      expect(pending.current).toBe(true);
      expect(push).not.toHaveBeenCalled();
    } finally {
      await act(async () => {
        gate.resolve(undefined);
        await first;
      });
      vi.restoreAllMocks();
    }
    expect(pending.current).toBe(false);
  },
);

it.each(["list", "create"] as const)("opens the ordinary %s destination", async (kind) => {
  const services = createTestServices([]);
  const open = vi.fn().mockReturnValue({ lifecycle: Promise.resolve("closed"), release: vi.fn() });
  const model = createWorktreeCommandActions({
    client: new QueryClient(),
    api: services.api,
    sessionID: "session",
    roots: { open },
    focusComposer: vi.fn(),
    deleteTarget: vi.fn(),
    refreshOpenWorktreeList: vi.fn(),
    push: vi.fn(),
    t: appI18n.t,
  });
  await model.execute({ kind });
  expect(open).toHaveBeenCalledWith({ kind: "worktree", sessionID: "session", page: kind });
  expect(services.transport.descriptorCalls).toHaveLength(0);
});

it.each([null, "topic"])(
  "prepares Delete %j independently while the first preview is loading",
  async (nextTarget) => {
    const services = createTestServices(worktreeCommandFixtureRoutes());
    const gate = deferred<undefined>();
    const previewDelete = services.api.previewWorktreeDelete.bind(services.api);
    const held = vi.spyOn(services.api, "previewWorktreeDelete").mockImplementationOnce(async (...args) => {
      const result = await previewDelete(...args);
      await gate.promise;
      return result;
    });
    const present = vi.fn<(preview: WorktreeDeletePreview) => void>();
    const push = vi.fn();
    const model = createWorktreeCommandDelete({
      client: new QueryClient(),
      api: services.api,
      sessionID: "session",
      present,
      refreshOpenWorktreeList: vi.fn(),
      push,
      t: appI18n.t,
    });
    const { result: pending } = renderHook(
      () => {
        useWorktreeCommandDelete(model);
        return useAtomValue(model.requestPending);
      },
      { wrapper: RegistryProvider },
    );
    let first!: Promise<void>;
    try {
      act(() => {
        first = model.prepare("topic");
      });
      await waitFor(() => {
        expect(held).toHaveBeenCalledOnce();
      });
      await act(async () => {
        await model.prepare(nextTarget);
      });
      expect(held).toHaveBeenCalledTimes(2);
      expect(present).toHaveBeenCalledOnce();
      expect(pending.current).toBe(true);
      expect(push).not.toHaveBeenCalled();
    } finally {
      await act(async () => {
        gate.resolve(undefined);
        await first;
      });
    }
    expect(present).toHaveBeenCalledTimes(2);
    expect(pending.current).toBe(false);
  },
);

it.each([
  { selector: "topic", choice: "confirm" as const },
  { selector: null, choice: "confirm_and_branch" as const },
])("deletes only from authoritative preview: %j", async ({ selector, choice }) => {
  const services = createTestServices(worktreeCommandFixtureRoutes());
  const present = vi.fn<(preview: WorktreeDeletePreview) => void>();
  const push = vi.fn();
  const model = createWorktreeCommandDelete({
    client: new QueryClient(),
    api: services.api,
    sessionID: "session",
    present,
    refreshOpenWorktreeList: vi.fn(),
    push,
    t: appI18n.t,
  });
  renderHook(() => useWorktreeCommandDelete(model), { wrapper: RegistryProvider });
  await act(async () => {
    await model.prepare(selector);
  });
  expect(push).not.toHaveBeenCalled();
  expect(present).toHaveBeenCalledOnce();
  const preview = present.mock.calls[0]?.[0];
  if (preview === undefined) throw new Error("Expected preview");
  await act(async () => {
    await model.deletion.submit({ preview, choice });
  });
  expect(push).not.toHaveBeenCalled();
  expect(services.transport.descriptorCalls.map(({ descriptor }) => descriptor)).toEqual([
    selector === null ? fixture.methods.status : fixture.methods.resolve,
    fixture.methods.preview,
    fixture.methods.delete,
  ]);
  expect(services.transport.descriptorCalls[1]?.request).toMatchObject({
    selector: selector === null ? fixture.currentRoot : fixture.selector,
  });
  expect(services.transport.descriptorCalls[2]?.request).toMatchObject({
    scope: { scope: { case: "sessionId", value: "session" } },
    selector: fixture.deletionSelector,
    forceFolderRemoval: true,
    branchCleanupPolicy: fixture.branchCleanup[choice],
  });
});

it("dispatches a confirmed Delete independently of an earlier pending deletion", async () => {
  const services = createTestServices(worktreeCommandFixtureRoutes());
  const gate = deferred<undefined>();
  const deleteWorktree = services.api.deleteWorktree.bind(services.api);
  const held = vi.spyOn(services.api, "deleteWorktree").mockImplementationOnce(async (...args) => {
    const result = await deleteWorktree(...args);
    await gate.promise;
    return result;
  });
  const present = vi.fn<(preview: WorktreeDeletePreview) => void>();
  const push = vi.fn();
  const model = createWorktreeCommandDelete({
    client: new QueryClient(),
    api: services.api,
    sessionID: "session",
    present,
    refreshOpenWorktreeList: vi.fn(),
    push,
    t: appI18n.t,
  });
  const { result: pending } = renderHook(
    () => {
      useWorktreeCommandDelete(model);
      return useAtomValue(model.deletion.requestPending);
    },
    { wrapper: RegistryProvider },
  );
  await act(async () => {
    await model.prepare("topic");
  });
  const preview = present.mock.calls[0]?.[0];
  if (preview === undefined) throw new Error("Expected preview");
  let first!: Promise<void>;
  try {
    act(() => {
      first = model.deletion.submit({ preview, choice: "confirm" });
    });
    await waitFor(() => {
      expect(held).toHaveBeenCalledOnce();
    });
    await act(async () => {
      await model.prepare(null);
      const nextPreview = present.mock.calls[1]?.[0];
      if (nextPreview === undefined) throw new Error("Expected second preview");
      await model.deletion.submit({ preview: nextPreview, choice: "confirm_and_branch" });
    });
    expect(held).toHaveBeenCalledTimes(2);
    expect(pending.current).toBe(true);
    expect(push).not.toHaveBeenCalled();
  } finally {
    await act(async () => {
      gate.resolve(undefined);
      await first;
    });
  }
  expect(pending.current).toBe(false);
});
