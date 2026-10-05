import { RegistryProvider, useAtomValue } from "@effect/atom-react";
import { QueryClient } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { appI18n } from "@/i18n";
import { createTestServices } from "@/test-support/app-services";
import { deferred } from "@/test-support/chat-runtime";
import { createProjectCreationModel, useProjectCreationActions } from "./ProjectCreationModel";

function setup() {
  const services = createTestServices([]);
  const select = vi.spyOn(services.nativeBridge.directories, "selectDirectory").mockResolvedValue(null);
  const plan = vi.spyOn(services.api, "planWorkspace");
  const create = vi.spyOn(services.api, "createProject");
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const push = vi.fn();
  const model = createProjectCreationModel({ services, client, push, t: appI18n.t });
  const context = { openProject: vi.fn(async () => undefined), openDraft: vi.fn(async () => undefined) };
  const view = renderHook(
    () => ({
      ...useProjectCreationActions(model),
      state: useAtomValue(model.state),
    }),
    {
      wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider>,
    },
  );
  return { ...view, services, select, plan, create, client, push, context };
}

it("stops workspace selection on directory cancellation", async () => {
  const view = setup();
  await act(async () => {
    view.result.current.chooseWorkspace(view.context);
  });
  expect(view.select).toHaveBeenCalledOnce();
  expect(view.plan).not.toHaveBeenCalled();
  expect(view.create).not.toHaveBeenCalled();
  expect(view.context.openDraft).not.toHaveBeenCalled();
});

const binding = {
  canonicalRoot: "/Kent",
  projectID: "project-1",
  workspaceID: "workspace-1",
  projectName: "Kent",
  projectKey: "KNT",
  workspaceName: "Kent",
  workspaceStatus: "available",
};

it("opens the attached Project without creating another Project", async () => {
  const view = setup();
  view.select.mockResolvedValue({ path: "/kent" });
  view.plan.mockResolvedValue({ kind: "bound", canonicalRoot: "/kent", binding });
  await act(async () => {
    view.result.current.chooseWorkspace(view.context);
  });
  expect(view.context.openProject).toHaveBeenCalledWith(binding.projectID);
  expect(view.create).not.toHaveBeenCalled();
});

it("opens creation with the planned canonical workspace", async () => {
  const view = setup();
  view.select.mockResolvedValue({ path: "/alias" });
  view.plan.mockResolvedValue({ kind: "local_unbound", canonicalRoot: "/Kent", binding: null });
  await act(async () => {
    view.result.current.chooseWorkspace(view.context);
  });
  expect(view.context.openDraft).toHaveBeenCalledWith({ name: "Kent", key: "KENT", workspaceRoot: "/Kent" });
  expect(view.create).not.toHaveBeenCalled();
});

const draft = { name: "Kent", key: "KENT", workspaceRoot: "/Kent" };

it("revalidates and creates using authored values, then completes and refreshes", async () => {
  const view = setup();
  view.plan.mockResolvedValue({ kind: "local_unbound", canonicalRoot: "/Kent", binding: null });
  view.create.mockResolvedValue(binding);
  const complete = vi.fn(async () => undefined);
  const invalidate = vi.spyOn(view.client, "invalidateQueries");
  await act(async () => {
    view.result.current.submit({ draft, complete, selectionRequired: vi.fn() });
  });
  expect(view.plan).toHaveBeenCalledWith(draft.workspaceRoot);
  expect(view.create).toHaveBeenCalledWith(draft.name, draft.key, draft.workspaceRoot);
  expect(complete).toHaveBeenCalledWith(binding.projectID, "created");
  expect(invalidate).toHaveBeenCalled();
});

it.each(["picker", "plan", "create"] as const)(
  "reports %s failure through ordinary status feedback",
  async (stage) => {
    const view = setup();
    const error = new Error("Unavailable");
    view.select.mockResolvedValue({ path: "/Kent" });
    view.plan.mockResolvedValue({ kind: "local_unbound", canonicalRoot: "/Kent", binding: null });
    if (stage === "picker") view.select.mockRejectedValue(error);
    if (stage === "plan") view.plan.mockRejectedValue(error);
    if (stage === "create") view.create.mockRejectedValue(error);
    await act(async () => {
      if (stage === "create")
        view.result.current.submit({ draft, complete: vi.fn(), selectionRequired: vi.fn() });
      else view.result.current.chooseWorkspace(view.context);
    });
    expect(view.push).toHaveBeenCalledWith(expect.objectContaining({ tone: "danger" }));
    if (stage === "create") expect(view.result.current.state.error).toBe(error);
  },
);

it.each(["picker", "plan", "create"] as const)(
  "rejects repeated actions synchronously during %s",
  async (stage) => {
    const view = setup();
    const selection = deferred<{ path: string } | null>();
    const planning = deferred<Awaited<ReturnType<typeof view.services.api.planWorkspace>>>();
    const creation = deferred<typeof binding>();
    view.select.mockReturnValue(stage === "picker" ? selection.promise : Promise.resolve({ path: "/Kent" }));
    view.plan.mockReturnValue(
      stage === "plan"
        ? planning.promise
        : Promise.resolve({ kind: "local_unbound", canonicalRoot: "/Kent", binding: null }),
    );
    view.create.mockReturnValue(creation.promise);
    const input = { draft, complete: vi.fn(async () => undefined), selectionRequired: vi.fn() };
    await act(async () => {
      if (stage === "picker") view.result.current.chooseWorkspace(view.context);
      else view.result.current.submit(input);
      if (stage === "picker") view.result.current.chooseWorkspace(view.context);
      else view.result.current.submit(input);
    });
    await act(async () => {
      view.result.current.chooseWorkspace(view.context);
      view.result.current.submit(input);
    });
    expect(view.select).toHaveBeenCalledTimes(stage === "picker" ? 1 : 0);
    expect(view.plan).toHaveBeenCalledTimes(stage === "picker" ? 0 : 1);
    expect(view.create).toHaveBeenCalledTimes(stage === "create" ? 1 : 0);
    await act(async () => {
      selection.resolve(null);
      planning.resolve({ kind: "local_unbound", canonicalRoot: "/Kent", binding: null });
      creation.resolve(binding);
    });
  },
);

it("does not create when revalidation requires workspace selection", async () => {
  const view = setup();
  view.plan.mockResolvedValue({ kind: "server_workspace_selection", canonicalRoot: "/Kent", binding: null });
  const selectionRequired = vi.fn();
  await act(async () => {
    view.result.current.submit({ draft, complete: vi.fn(), selectionRequired });
  });
  expect(view.create).not.toHaveBeenCalled();
  expect(selectionRequired).toHaveBeenCalledOnce();
  expect(view.push).toHaveBeenCalledWith(expect.objectContaining({ tone: "info" }));
});

it("does not open inline creation after native launch fails following disposal", async () => {
  vi.useFakeTimers();
  try {
    const view = setup();
    vi.spyOn(view.services.nativeBridge.capabilities, "projectCreationWindow", "get").mockReturnValue(true);
    const launch = deferred<undefined>();
    const openWindow = vi
      .spyOn(view.services.nativeBridge.projectCreation, "openWindow")
      .mockReturnValue(launch.promise);
    view.select.mockResolvedValue({ path: "/Kent" });
    view.plan.mockResolvedValue({ kind: "local_unbound", canonicalRoot: "/Kent", binding: null });
    await act(async () => {
      view.result.current.chooseWorkspace(view.context);
    });
    expect(openWindow).toHaveBeenCalledOnce();
    view.unmount();
    await act(async () => vi.advanceTimersByTimeAsync(500));
    await act(async () => {
      launch.reject(new Error("Launch failed"));
    });
    expect(view.context.openDraft).not.toHaveBeenCalled();
    expect(view.push).not.toHaveBeenCalled();
  } finally {
    vi.useRealTimers();
  }
});

it.each(["plan", "create"] as const)(
  "disposes local work but preserves Query completion during %s",
  async (stage) => {
    vi.useFakeTimers();
    try {
      const view = setup();
      const planning = deferred<Awaited<ReturnType<typeof view.services.api.planWorkspace>>>();
      const creation = deferred<typeof binding>();
      view.plan.mockReturnValue(
        stage === "plan"
          ? planning.promise
          : Promise.resolve({ kind: "local_unbound", canonicalRoot: "/Kent", binding: null }),
      );
      view.create.mockReturnValue(creation.promise);
      const complete = vi.fn(async () => undefined);
      await act(async () => {
        view.result.current.submit({ draft, complete, selectionRequired: vi.fn() });
      });
      view.unmount();
      await act(async () => vi.advanceTimersByTimeAsync(500));
      await act(async () => {
        planning.resolve({ kind: "local_unbound", canonicalRoot: "/Kent", binding: null });
        creation.resolve(binding);
      });
      expect(view.create).toHaveBeenCalledTimes(stage === "create" ? 1 : 0);
      expect(complete).toHaveBeenCalledTimes(stage === "create" ? 1 : 0);
    } finally {
      vi.useRealTimers();
    }
  },
);
