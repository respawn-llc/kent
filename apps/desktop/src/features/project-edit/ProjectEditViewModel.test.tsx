import { RegistryProvider, useAtomValue } from "@effect/atom-react";
import { QueryClient } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";

import { queryKeys } from "@/app-facade";
import { appI18n } from "@/i18n";
import { createTestServices } from "@/test-support/app-services";
import { deferred } from "@/test-support/chat-runtime";
import { createTestSidebarNavigator } from "@/test-support/sidebar";
import { createProjectEditViewModel, useProjectEditActions } from "./ProjectEditViewModel";

const project = { projectID: "project-1", displayName: "Kent", projectKey: "KNT" };
const savedProject = {
  id: project.projectID,
  name: "New name",
  key: project.projectKey,
  primaryWorkspace: {
    id: "workspace-1",
    name: "Kent",
    rootPath: "/kent",
    availability: "available" as const,
    isPrimary: true,
    updatedAt: 1,
  },
  defaultWorkflowID: null,
  defaultWorkflowName: null,
  defaultWorkflowValid: false,
  updatedAt: 1,
  taskCount: 0,
  attentionCount: 0,
  workflowCount: 0,
};

function fixture(metadata = project) {
  const services = createTestServices([]);
  vi.spyOn(services.api, "getProjectEdit").mockResolvedValue(metadata);
  const response = deferred<Awaited<ReturnType<typeof services.api.updateProject>>>();
  const update = vi.spyOn(services.api, "updateProject").mockReturnValue(response.promise);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  client.setQueryData(queryKeys.projectEdit(project.projectID), metadata);
  const push = vi.fn();
  const navigator = createTestSidebarNavigator();
  const openHome = vi.fn(async () => undefined);
  const model = createProjectEditViewModel({
    services,
    client,
    projectID: project.projectID,
    t: appI18n.t,
    push,
    completion: { navigator, openHome },
  });
  return { response, update, client, push, services, navigator, openHome, model };
}

function setup(metadata = project) {
  const context = fixture(metadata);
  const view = renderHook(
    () => ({
      state: useAtomValue(context.model.state),
      ...useProjectEditActions(context.model),
    }),
    { wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider> },
  );
  return { ...view, ...context };
}

it("admits only one Save when the public action binding is invoked twice in one turn", async () => {
  const view = setup();
  act(() => {
    view.result.current.editName("New name");
  });
  await act(async () => {
    view.result.current.save(undefined);
    view.result.current.save(undefined);
  });
  expect(view.update).toHaveBeenCalledTimes(1);
  await act(async () => {
    view.response.resolve({ project: savedProject });
    await view.response.promise;
  });
});

it.each(["", "  name", "same"])("does not submit invalid or unchanged name input: %s", async (name) => {
  const view = setup();
  act(() => {
    view.result.current.editName(name === "same" ? project.displayName : name);
    view.result.current.save(undefined);
  });
  await act(async () => undefined);
  expect(view.update).not.toHaveBeenCalled();
});

it("allows a name-only Save with an unchanged legacy key", async () => {
  const view = setup({ ...project, projectKey: "legacy-key" });
  await act(async () => {
    view.result.current.editName("New name");
    view.result.current.save(undefined);
  });
  expect(view.update).toHaveBeenCalledWith(project.projectID, "New name", undefined);
  await act(async () => {
    view.response.resolve({ project: savedProject });
    await view.response.promise;
  });
});

it("reports a request failure through the ordinary status notification", async () => {
  const view = setup();
  act(() => {
    view.result.current.editName("New name");
    view.result.current.save(undefined);
  });
  await act(async () => {
    view.response.reject(new Error("Request failed"));
    await view.response.promise.catch(() => undefined);
  });
  expect(view.push).toHaveBeenCalledWith(expect.objectContaining({ tone: "danger" }));
  expect(view.update).toHaveBeenCalledTimes(1);
});

it("does not submit an invalid changed Project key", async () => {
  const view = setup();
  act(() => {
    view.result.current.editKey("invalid-key");
    view.result.current.save(undefined);
  });
  await act(async () => undefined);
  expect(view.update).not.toHaveBeenCalled();
});

it("does not request Make Default for an already default Workspace", async () => {
  const view = setup();
  const saveDefault = vi
    .spyOn(view.services.api, "setDefaultWorkspace")
    .mockResolvedValue({ project: savedProject });
  await act(async () => {
    view.result.current.makeDefault({ id: "workspace-1", name: "One", rootPath: "/one", isDefault: true });
  });
  expect(saveDefault).not.toHaveBeenCalled();
});

it("leaves catalog pages unchanged when making a Workspace default", async () => {
  const view = setup();
  const saveDefault = vi
    .spyOn(view.services.api, "setDefaultWorkspace")
    .mockResolvedValue({ project: savedProject });
  const invalidate = vi.spyOn(view.client, "invalidateQueries");
  await act(async () => {
    view.result.current.makeDefault({ id: "workspace-1", name: "One", rootPath: "/one", isDefault: false });
  });
  expect(saveDefault).toHaveBeenCalledWith(project.projectID, "workspace-1");
  expect(invalidate).toHaveBeenCalledWith({ queryKey: queryKeys.projectEdit(project.projectID) });
  expect(invalidate).not.toHaveBeenCalledWith(
    expect.objectContaining({ queryKey: queryKeys.projectWorkspaceCatalog(project.projectID) }),
  );
});

it("admits only one directory selection while the picker is pending", async () => {
  const view = setup();
  const selection = deferred<{ path: string } | null>();
  const select = vi
    .spyOn(view.services.nativeBridge.directories, "selectDirectory")
    .mockReturnValue(selection.promise);
  const attach = vi.spyOn(view.services.api, "attachWorkspace");
  await act(async () => {
    view.result.current.chooseWorkspace(undefined);
    view.result.current.chooseWorkspace(undefined);
  });
  expect(select).toHaveBeenCalledTimes(1);
  await act(async () => {
    selection.resolve(null);
    await selection.promise;
  });
  expect(attach).not.toHaveBeenCalled();
});

it("sends an already attached directory explicitly and keeps the catalog untouched", async () => {
  const view = setup();
  vi.spyOn(view.services.nativeBridge.directories, "selectDirectory").mockResolvedValue({ path: "/buried" });
  const attach = vi.spyOn(view.services.api, "attachWorkspace").mockResolvedValue({
    binding: {
      projectID: project.projectID,
      workspaceID: "buried",
      projectKey: project.projectKey,
      projectName: project.displayName,
      workspaceName: "Buried",
      workspaceStatus: "available",
    },
    outcome: "already_attached",
  });
  const invalidate = vi.spyOn(view.client, "invalidateQueries");
  await act(async () => {
    view.result.current.chooseWorkspace(undefined);
  });
  expect(attach).toHaveBeenCalledWith(project.projectID, "/buried");
  expect(view.push).toHaveBeenCalledWith(expect.objectContaining({ tone: "success" }));
  expect(invalidate).not.toHaveBeenCalledWith(
    expect.objectContaining({ queryKey: queryKeys.projectWorkspaceCatalog(project.projectID) }),
  );
});

it("admits only one inline Unlink while its request is pending", async () => {
  const view = setup();
  const response = deferred<Awaited<ReturnType<typeof view.services.api.unlinkWorkspace>>>();
  const unlink = vi.spyOn(view.services.api, "unlinkWorkspace").mockReturnValue(response.promise);
  const close = vi.fn();
  await act(async () => {
    view.result.current.unlink({ workspaceID: "workspace-1", close });
    view.result.current.unlink({ workspaceID: "workspace-1", close });
  });
  expect(unlink).toHaveBeenCalledTimes(1);
  await act(async () => {
    response.resolve({
      blockers: [],
      project: null,
      projectID: project.projectID,
      workspaceID: "workspace-1",
    });
    await response.promise;
  });
  expect(close).toHaveBeenCalledTimes(1);
});

it("keeps inline confirmation open for blockers and permits an explicit retry", async () => {
  const view = setup();
  const unlink = vi.spyOn(view.services.api, "unlinkWorkspace").mockResolvedValue({
    projectID: project.projectID,
    workspaceID: "workspace-1",
    project: null,
    blockers: [{ code: "in-use", message: "Workspace is in use" }],
  });
  const close = vi.fn();
  const invalidate = vi.spyOn(view.client, "invalidateQueries");
  await act(async () => {
    view.result.current.unlink({ workspaceID: "workspace-1", close });
  });
  expect(close).not.toHaveBeenCalled();
  expect(view.push).toHaveBeenCalledWith(expect.objectContaining({ tone: "danger" }));
  await act(async () => {
    view.result.current.unlink({ workspaceID: "workspace-1", close });
  });
  expect(unlink).toHaveBeenCalledTimes(2);
  expect(invalidate).not.toHaveBeenCalledWith(
    expect.objectContaining({ queryKey: queryKeys.projectWorkspaceCatalog(project.projectID) }),
  );
});

it("finishes Delete after navigation without taking over the replacement destination", async () => {
  const view = setup();
  const response = deferred<Awaited<ReturnType<typeof view.services.api.deleteProject>>>();
  const remove = vi.spyOn(view.services.api, "deleteProject").mockReturnValue(response.promise);
  const close = vi.fn();
  const invalidate = vi.spyOn(view.client, "invalidateQueries");
  await act(async () => {
    view.result.current.deleteProject({ close });
  });
  expect(remove).toHaveBeenCalledTimes(1);
  vi.mocked(view.navigator.close).mockReturnValue("stale");
  view.unmount();
  await act(async () => {
    response.resolve({ projectID: project.projectID, deleted: true, blockers: [] });
    await response.promise;
  });
  expect(view.openHome).not.toHaveBeenCalled();
  expect(invalidate).toHaveBeenCalledWith(expect.objectContaining({ queryKey: queryKeys.projects }));
  expect(view.push).toHaveBeenCalledWith(expect.objectContaining({ tone: "success" }));
});

it("keeps admission attached when visual readers leave during a pending Save", async () => {
  const context = fixture();
  let observe = true;
  function Visual() {
    useAtomValue(context.model.state);
    return null;
  }
  const view = renderHook(() => useProjectEditActions(context.model), {
    wrapper: ({ children }: { children: ReactNode }) => (
      <RegistryProvider>
        {children}
        {observe ? <Visual /> : null}
      </RegistryProvider>
    ),
  });
  await act(async () => {
    view.result.current.editName("New name");
    view.result.current.save(undefined);
  });
  observe = false;
  view.rerender();
  await act(async () => {
    view.result.current.save(undefined);
  });
  expect(context.update).toHaveBeenCalledTimes(1);
  await act(async () => {
    context.response.resolve({ project: savedProject });
    await context.response.promise;
  });
  expect(context.push).toHaveBeenCalledWith(expect.objectContaining({ tone: "success" }));
});

it("keeps Query failure feedback after the actual Atom owner is disposed", async () => {
  vi.useFakeTimers();
  try {
    const view = setup();
    await act(async () => {
      view.result.current.editName("New name");
      view.result.current.save(undefined);
    });
    view.unmount();
    await act(async () => vi.advanceTimersByTimeAsync(500));
    await act(async () => {
      view.response.reject(new Error("Save failed"));
      await view.response.promise.catch(() => undefined);
    });
    expect(view.push).toHaveBeenCalledWith(expect.objectContaining({ tone: "danger" }));
    expect(view.update).toHaveBeenCalledTimes(1);
  } finally {
    vi.useRealTimers();
  }
});

it("does not continue local picker work after its actual owner is disposed", async () => {
  vi.useFakeTimers();
  try {
    const view = setup();
    const selection = deferred<{ path: string } | null>();
    vi.spyOn(view.services.nativeBridge.directories, "selectDirectory").mockReturnValue(selection.promise);
    const attach = vi.spyOn(view.services.api, "attachWorkspace");
    await act(async () => {
      view.result.current.chooseWorkspace(undefined);
    });
    view.unmount();
    await act(async () => vi.advanceTimersByTimeAsync(500));
    await act(async () => {
      selection.resolve({ path: "/one" });
      await selection.promise;
    });
    expect(attach).not.toHaveBeenCalled();
  } finally {
    vi.useRealTimers();
  }
});
