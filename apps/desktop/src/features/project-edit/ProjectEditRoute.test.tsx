import { RegistryProvider } from "@effect/atom-react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import type { MockInstance } from "vitest";

import {
  RpcError,
  rpcErrorCodes,
  type ProjectEdit,
  type WorkspaceCatalogPage,
  type WorkspaceCatalogRow,
  type ApiService,
} from "@/api";
import {
  AppServicesProvider,
  queryKeys,
  SidebarHeaderActionProvider,
  SidebarHeaderActionSlot,
} from "@/app-facade";
import { appI18n } from "@/i18n";
import { createTestServices } from "@/test-support/app-services";
import { deferred } from "@/test-support/chat-runtime";
import { createTestSidebarNavigator } from "@/test-support/sidebar";
import { ProjectEditRoute } from "./ProjectEditRoute";

const project = { displayName: "Kent", projectID: "project-1", projectKey: "KNT" };
const workspace = { id: "workspace-1", isDefault: false, name: "One", rootPath: "/one" };
let services: ReturnType<typeof createTestServices>;
let client: QueryClient;
let getMetadata: MockInstance<ApiService["getProjectEdit"]>;
let listWorkspaces: MockInstance<ApiService["listWorkspaces"]>;
const statusPush = vi.hoisted(() => vi.fn());
const sidebarBackWhen = vi.hoisted(() => vi.fn());
const navigation = vi.hoisted(() => ({ openHome: vi.fn(async () => undefined) }));

vi.mock("@/app-facade", async (importOriginal) => ({
  ...(await importOriginal()),
  useAppServices: () => services,
  useAppNavigation: () => navigation,
  useSidebarBackWhen: sidebarBackWhen,
  useSidebarHeaderOffset: () => 0,
  useStatusController: () => ({ push: statusPush }),
  useNativeDialogFallback: () => ({ fallback: null, open: vi.fn(async () => undefined) }),
  useWindowChromeTitle: () => undefined,
}));

vi.mock("@/ui", async (importOriginal) => ({
  ...(await importOriginal()),
  VirtualizedInfiniteList: (
    props: Readonly<{
      empty?: ReactNode;
      header: ReactNode;
      items: readonly { occurrenceKey: string; workspace: WorkspaceCatalogRow }[];
      nextBoundary?: Readonly<{ state: string; onRetry?: () => void }>;
      previousBoundary?: Readonly<{ state: string; onRetry?: () => void }>;
      hasPreviousPage: boolean;
      hasNextPage: boolean;
      onLoadPrevious(): void;
      onLoadMore(): void;
      renderItem(item: { occurrenceKey: string; workspace: WorkspaceCatalogRow }): ReactNode;
    }>,
  ) => (
    <>
      {props.header}
      {props.items.map((item) => (
        <span key={item.occurrenceKey}>{props.renderItem(item)}</span>
      ))}
      {props.items.length === 0 ? props.empty : null}
      {props.hasNextPage ? <button onClick={props.onLoadMore}>next-edge</button> : null}
      {props.hasPreviousPage ? <button onClick={props.onLoadPrevious}>previous-edge</button> : null}
      {props.nextBoundary?.state === "error" ? (
        <button onClick={props.nextBoundary.onRetry}>retry-edge</button>
      ) : null}
      {props.previousBoundary?.state === "error" ? (
        <button onClick={props.previousBoundary.onRetry}>retry-previous</button>
      ) : null}
    </>
  ),
}));

function mount(navigator?: ReturnType<typeof createTestSidebarNavigator>) {
  return render(
    <ProjectEditRoute projectId={project.projectID} {...(navigator === undefined ? {} : { navigator })} />,
    {
      wrapper: ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={client}>
          <RegistryProvider>
            <AppServicesProvider services={services}>
              <SidebarHeaderActionProvider>
                <SidebarHeaderActionSlot />
                {children}
              </SidebarHeaderActionProvider>
            </AppServicesProvider>
          </RegistryProvider>
        </QueryClientProvider>
      ),
    },
  );
}

function seedCatalog(pages: readonly WorkspaceCatalogPage[]) {
  client.setQueryData(queryKeys.projectWorkspaceCatalog(project.projectID), {
    pages,
    pageParams: pages.map((page) => page.offset),
  });
}

beforeEach(() => {
  services = createTestServices([]);
  getMetadata = vi.spyOn(services.api, "getProjectEdit").mockResolvedValue(project);
  listWorkspaces = vi.spyOn(services.api, "listWorkspaces").mockResolvedValue({
    projectID: project.projectID,
    offset: 0,
    workspaces: [],
    nextOffset: null,
  });
  client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  client.setQueryData(queryKeys.projectEdit(project.projectID), project);
  statusPush.mockClear();
  sidebarBackWhen.mockClear();
  navigation.openHome = vi.fn(async () => undefined);
});

it("confirms workspace detach above the destination and refreshes the sidebar list", async () => {
  const workspace = { id: "workspace-extra", name: "Extra", rootPath: "/extra", isDefault: false };
  listWorkspaces.mockResolvedValue({
    projectID: project.projectID,
    offset: 0,
    workspaces: [workspace],
    nextOffset: null,
  });
  const response = deferred<Awaited<ReturnType<typeof services.api.unlinkWorkspace>>>();
  const unlink = vi.spyOn(services.api, "unlinkWorkspace").mockReturnValue(response.promise);
  const nativeWindow = vi.spyOn(services.nativeBridge.dialogs, "openWindow");
  const view = mount();
  const button = await screen.findByRole("button", {
    name: appI18n.t("projectEdit.unlinkWorkspace", { path: "/extra" }),
  });
  fireEvent.click(button);
  const dialog = screen.getByRole("dialog", { name: appI18n.t("projectEdit.unlinkTitle") });
  expect(view.container).not.toContainElement(dialog);
  expect(document.body).toContainElement(dialog);
  expect(nativeWindow).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: appI18n.t("projectEdit.unlinkConfirm") }));
  await waitFor(() => {
    expect(unlink).toHaveBeenCalledOnce();
  });
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  listWorkspaces.mockResolvedValue({
    projectID: project.projectID,
    offset: 0,
    workspaces: [],
    nextOffset: null,
  });
  await act(async () => {
    response.resolve({
      projectID: project.projectID,
      workspaceID: workspace.id,
      project: null,
      blockers: [],
    });
    await response.promise;
  });
  await waitFor(() => expect(button).not.toBeInTheDocument());
});

it("reports a detach failure in the destination after closing confirmation", async () => {
  listWorkspaces.mockResolvedValue({
    projectID: project.projectID,
    offset: 0,
    workspaces: [workspace],
    nextOffset: null,
  });
  vi.spyOn(services.api, "unlinkWorkspace").mockRejectedValue(new Error("request failed"));
  mount();
  fireEvent.click(
    await screen.findByRole("button", {
      name: appI18n.t("projectEdit.unlinkWorkspace", { path: workspace.rootPath }),
    }),
  );
  fireEvent.click(screen.getByRole("button", { name: appI18n.t("projectEdit.unlinkConfirm") }));
  await waitFor(() => {
    expect(statusPush).toHaveBeenCalledWith(expect.objectContaining({ tone: "danger" }));
  });
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(
    screen.getByRole("button", {
      name: appI18n.t("projectEdit.unlinkWorkspace", { path: workspace.rootPath }),
    }),
  ).toBeEnabled();
});

it("keeps drafts when unrelated Home selection changes the navigation callback", () => {
  const view = mount();
  fireEvent.change(screen.getByRole("textbox", { name: appI18n.t("projectEdit.name") }), {
    target: { value: "Draft name" },
  });
  fireEvent.change(screen.getByRole("textbox", { name: appI18n.t("projectEdit.taskKey") }), {
    target: { value: "DRAFT" },
  });
  navigation.openHome = vi.fn(async () => undefined);
  view.rerender(<ProjectEditRoute projectId={project.projectID} />);
  expect(screen.getByRole("textbox", { name: appI18n.t("projectEdit.name") })).toHaveValue("Draft name");
  expect(screen.getByRole("textbox", { name: appI18n.t("projectEdit.taskKey") })).toHaveValue("DRAFT");
});

it("keeps the pending Save observer when unrelated Home selection changes the navigation callback", async () => {
  const response = deferred<Awaited<ReturnType<ApiService["updateProject"]>>>();
  const update = vi.spyOn(services.api, "updateProject").mockReturnValue(response.promise);
  const view = mount();
  fireEvent.change(screen.getByRole("textbox", { name: appI18n.t("projectEdit.name") }), {
    target: { value: "Draft name" },
  });
  fireEvent.click(screen.getByRole("button", { name: appI18n.t("projectEdit.saveName") }));
  await waitFor(() => {
    expect(update).toHaveBeenCalledTimes(1);
  });
  navigation.openHome = vi.fn(async () => undefined);
  view.rerender(<ProjectEditRoute projectId={project.projectID} />);
  const save = screen.getByRole("button", { name: appI18n.t("projectEdit.saveName") });
  expect(save).toBeDisabled();
  expect(screen.getByRole("textbox", { name: appI18n.t("projectEdit.name") })).toHaveValue("Draft name");
  fireEvent.click(save);
  expect(update).toHaveBeenCalledTimes(1);
  await act(async () => {
    response.reject(new Error("Save failed"));
    await response.promise.catch(() => undefined);
  });
  expect(statusPush).toHaveBeenCalledWith(expect.objectContaining({ tone: "danger" }));
});

it("keeps metadata editable and Attach available while the first catalog page owns Retry", async () => {
  listWorkspaces.mockRejectedValue(new Error("Catalog failed"));
  mount();
  expect(screen.getByRole("textbox", { name: appI18n.t("projectEdit.name") })).toHaveValue("Kent");
  expect(screen.getByRole("button", { name: appI18n.t("projectEdit.attachWorkspace") })).toBeEnabled();
  fireEvent.click(await screen.findByRole("button", { name: appI18n.t("app.retry") }));
  await waitFor(() => {
    expect(listWorkspaces).toHaveBeenCalledTimes(2);
  });
  expect(getMetadata).not.toHaveBeenCalled();
});

it("keeps loaded Workspace actions while metadata owns Retry", async () => {
  client.removeQueries({ queryKey: queryKeys.projectEdit(project.projectID) });
  getMetadata.mockRejectedValue(new Error("Metadata failed"));
  seedCatalog([{ projectID: project.projectID, offset: 0, workspaces: [workspace], nextOffset: null }]);
  mount();
  fireEvent.click(await screen.findByRole("button", { name: appI18n.t("app.retry") }));
  expect(screen.getByRole("button", { name: appI18n.t("projectEdit.attachWorkspace") })).toBeEnabled();
  expect(
    screen.getByRole("button", { name: appI18n.t("projectEdit.makeDefaultWorkspace", { path: "/one" }) }),
  ).toBeEnabled();
  await waitFor(() => {
    expect(getMetadata).toHaveBeenCalledTimes(2);
  });
  expect(listWorkspaces).not.toHaveBeenCalled();
});

it("hydrates metadata drafts when the pending read completes", async () => {
  client.removeQueries({ queryKey: queryKeys.projectEdit(project.projectID) });
  const pending = deferred<ProjectEdit>();
  getMetadata.mockReturnValue(pending.promise);
  mount();
  await act(async () => {
    pending.resolve(project);
    await pending.promise;
  });
  expect(await screen.findByRole("textbox", { name: appI18n.t("projectEdit.name") })).toHaveValue("Kent");
});

it("preserves edited drafts across a recoverable metadata failure and explicit Retry", async () => {
  mount();
  fireEvent.change(screen.getByRole("textbox", { name: appI18n.t("projectEdit.name") }), {
    target: { value: "Draft" },
  });
  getMetadata.mockRejectedValueOnce(new Error("Read failed"));
  await act(async () => {
    await client.invalidateQueries({ queryKey: queryKeys.projectEdit(project.projectID) });
  });
  fireEvent.click(await screen.findByRole("button", { name: appI18n.t("app.retry") }));
  expect(await screen.findByRole("textbox", { name: appI18n.t("projectEdit.name") })).toHaveValue("Draft");
});

it("treats a missing Project from the catalog as Back", async () => {
  listWorkspaces.mockRejectedValue(
    new RpcError({
      code: rpcErrorCodes.projectNotFound,
      message: "gone",
      method: "project.workspace.list",
    }),
  );
  const navigator = createTestSidebarNavigator();
  mount(navigator);
  await waitFor(() => {
    expect(sidebarBackWhen).toHaveBeenCalledWith(true, navigator);
  });
});

it("retains overlapping rows through a failed next edge and retries only that edge", async () => {
  seedCatalog([
    { projectID: project.projectID, offset: 0, workspaces: [workspace], nextOffset: 100 },
    { projectID: project.projectID, offset: 100, workspaces: [workspace], nextOffset: 200 },
  ]);
  listWorkspaces.mockRejectedValue(new Error("Edge failed"));
  mount();
  fireEvent.click(screen.getByRole("button", { name: "next-edge" }));
  fireEvent.click(await screen.findByRole("button", { name: "retry-edge" }));
  await waitFor(() => {
    expect(listWorkspaces).toHaveBeenCalledTimes(2);
  });
  expect(listWorkspaces).toHaveBeenLastCalledWith(project.projectID, 200);
  expect(
    screen.getAllByRole("button", { name: appI18n.t("projectEdit.makeDefaultWorkspace", { path: "/one" }) }),
  ).toHaveLength(2);
});

it("traverses at most four retained pages and exposes the previous edge after eviction", async () => {
  listWorkspaces.mockImplementation(async (_id, offset = 0) => ({
    projectID: project.projectID,
    offset,
    nextOffset: offset + 100,
    workspaces: [{ ...workspace, id: `workspace-${offset.toString()}` }],
  }));
  mount();
  for (let page = 0; page < 5; page++) {
    fireEvent.click(await screen.findByRole("button", { name: "next-edge" }));
    await waitFor(() => {
      expect(listWorkspaces).toHaveBeenCalledTimes(page + 2);
    });
  }
  const retained = client.getQueryData<{ pages: WorkspaceCatalogPage[] }>(
    queryKeys.projectWorkspaceCatalog(project.projectID),
  );
  expect(retained?.pages).toHaveLength(4);
  expect(retained?.pages[0]?.offset).toBe(200);
  fireEvent.click(screen.getByRole("button", { name: "previous-edge" }));
  await waitFor(() => {
    expect(listWorkspaces).toHaveBeenLastCalledWith(project.projectID, 100);
  });
});

it("renders cached content immediately on return but discards unsaved drafts", async () => {
  seedCatalog([{ projectID: project.projectID, offset: 0, workspaces: [workspace], nextOffset: null }]);
  const view = mount();
  fireEvent.change(screen.getByRole("textbox", { name: appI18n.t("projectEdit.name") }), {
    target: { value: "Draft" },
  });
  view.unmount();
  mount();
  expect(screen.getByRole("textbox", { name: appI18n.t("projectEdit.name") })).toHaveValue(
    project.displayName,
  );
  expect(
    screen.getByRole("button", { name: appI18n.t("projectEdit.makeDefaultWorkspace", { path: "/one" }) }),
  ).toBeEnabled();
  expect(listWorkspaces).not.toHaveBeenCalled();
});
