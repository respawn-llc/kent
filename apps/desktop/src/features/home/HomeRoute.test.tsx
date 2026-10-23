import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, mock, vi } from "bun:test";
import { SidebarRootContext } from "@/app-facade";
import { appI18n } from "@/i18n";
import { TestAppProviders, createTestServices } from "@/test-support/app-services";
import { deferred } from "@/test-support/chat-runtime";
import { createTestSidebarController } from "@/test-support/sidebar";

const push = vi.fn();
const openProject = vi.fn(async () => undefined);
const actualFacade = await import("@/app-facade");
await mock.module("@/app-facade", () => ({
  ...actualFacade,
  useAppNavigation: () => ({ openProject }),
  useStatusController: () => ({ push, dismiss: vi.fn() }),
}));
const { HomeRoute } = await import("./HomeRoute");

it.each(["created", "attached"] as const)(
  "keeps failed drafts for retry and starts fresh after the %s outcome",
  async (outcome) => {
    push.mockClear();
    openProject.mockClear();
    const services = createTestServices([]);
    vi.spyOn(services.api, "listProjects").mockResolvedValue({
      projects: [],
      nextPageToken: null,
      generatedAt: 1,
    });
    vi.spyOn(services.api, "listAttention").mockResolvedValue({
      items: [],
      nextPageToken: null,
      generatedAt: 1,
    });
    const picker = vi
      .spyOn(services.nativeBridge.directories, "selectDirectory")
      .mockResolvedValue({ path: "/Kent" });
    const plan = vi.spyOn(services.api, "planWorkspace").mockResolvedValue({
      kind: "local_unbound",
      canonicalRoot: "/Kent",
      binding: null,
    });
    const pending = deferred<Awaited<ReturnType<typeof services.api.createProject>>>();
    const create = vi.spyOn(services.api, "createProject").mockImplementation(async () => {
      expect(screen.queryByRole("dialog") === null).toBe(true);
      return pending.promise;
    });
    render(
      <TestAppProviders services={services} skipAnimations>
        <SidebarRootContext.Provider value={createTestSidebarController()}>
          <HomeRoute selectedProjectID={null} />
        </SidebarRootContext.Provider>
      </TestAppProviders>,
    );
    const user = userEvent.setup();
    const newProject = screen.getByRole("button", { name: appI18n.t("home.newProject") });
    await user.click(newProject);
    const dialog = await screen.findByRole("dialog");
    const name = within(dialog).getByRole("textbox", { name: appI18n.t("home.projectName") });
    const key = within(dialog).getByRole("textbox", { name: appI18n.t("home.projectKey") });
    await user.clear(name);
    await user.type(name, "Authored Project");
    await user.clear(key);
    await user.type(key, "AUTH");
    await user.click(within(dialog).getByRole("button", { name: appI18n.t("home.createProject") }));
    await waitFor(() => {
      expect(create).toHaveBeenCalledTimes(1);
    });
    expect(screen.queryByRole("dialog") === null).toBe(true);
    expect(newProject).toBeDisabled();
    await act(async () => {
      pending.reject(new Error("Unavailable"));
    });
    await waitFor(() => expect(newProject).not.toBeDisabled());
    expect(push).toHaveBeenCalledWith(expect.objectContaining({ tone: "danger" }));
    expect(screen.queryByRole("dialog") === null).toBe(true);
    expect(create).toHaveBeenCalledTimes(1);
    await user.click(newProject);
    const retry = await screen.findByRole("dialog");
    expect(within(retry).getByRole("textbox", { name: appI18n.t("home.projectName") })).toHaveValue(
      "Authored Project",
    );
    expect(within(retry).getByRole("textbox", { name: appI18n.t("home.projectKey") })).toHaveValue("AUTH");
    expect(picker).toHaveBeenCalledTimes(1);
    expect(create).toHaveBeenCalledTimes(1);
    const retried = deferred<Awaited<ReturnType<typeof services.api.createProject>>>();
    const binding = {
      canonicalRoot: "/Kent",
      projectID: "project-created",
      workspaceID: "workspace-created",
      projectName: "Authored Project",
      projectKey: "AUTH",
      workspaceName: "Kent",
      workspaceStatus: "available",
    };
    if (outcome === "attached") {
      plan.mockResolvedValue({ kind: "bound", canonicalRoot: "/Kent", binding });
    }
    create.mockImplementation(async () => {
      expect(screen.queryByRole("dialog") === null).toBe(true);
      return retried.promise;
    });
    await user.click(within(retry).getByRole("button", { name: appI18n.t("home.createProject") }));
    if (outcome === "created") {
      await waitFor(() => {
        expect(create).toHaveBeenCalledTimes(2);
      });
      expect(create).toHaveBeenLastCalledWith("Authored Project", "AUTH", "/Kent");
      await act(async () => {
        retried.resolve(binding);
      });
    }
    await waitFor(() => {
      expect(openProject).toHaveBeenCalledWith("project-created");
    });
    expect(create).toHaveBeenCalledTimes(outcome === "created" ? 2 : 1);
    picker.mockResolvedValue({ path: "/Next" });
    plan.mockResolvedValue({ kind: "local_unbound", canonicalRoot: "/Next", binding: null });
    await waitFor(() => expect(newProject).not.toBeDisabled());
    await user.click(newProject);
    const fresh = await screen.findByRole("dialog");
    expect(picker).toHaveBeenCalledTimes(2);
    expect(within(fresh).getByRole("textbox", { name: appI18n.t("home.projectName") })).toHaveValue("Next");
    expect(within(fresh).getByRole("textbox", { name: appI18n.t("home.projectKey") })).toHaveValue("NEXT");
    expect(within(fresh).getByRole("textbox", { name: appI18n.t("home.workspaceRoot") })).toHaveValue(
      "/Next",
    );
  },
);
