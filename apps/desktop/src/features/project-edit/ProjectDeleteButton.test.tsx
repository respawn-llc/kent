import { RegistryProvider } from "@effect/atom-react";
import { QueryClient } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "bun:test";
import { AppServicesProvider, StatusProvider } from "@/app-facade";
import { appI18n } from "@/i18n";
import { createTestServices } from "@/test-support/app-services";
import { createTestSidebarNavigator } from "@/test-support/sidebar";
import { createProjectEditViewModel } from "./ProjectEditViewModel";
import { ProjectDeleteButton } from "./ProjectDeleteButton";

it("dismisses confirmation before submitting and leaves retry with Project Edit", async () => {
  const services = createTestServices([]);
  Object.defineProperty(services.nativeBridge.capabilities, "dialogWindows", { value: true });
  const openWindow = vi.spyOn(services.nativeBridge.dialogs, "openWindow");
  const remove = vi.spyOn(services.api, "deleteProject").mockImplementation(async () => {
    expect(screen.queryByRole("dialog") === null).toBe(true);
    return { projectID: "project-1", deleted: false, blockers: [] };
  });
  const model = createProjectEditViewModel({
    services,
    client: new QueryClient(),
    projectID: "project-1",
    t: appI18n.t,
    push: vi.fn(),
    navigator: createTestSidebarNavigator(),
  });
  const view = render(
    <RegistryProvider>
      <AppServicesProvider services={services}>
        <StatusProvider>
          <ProjectDeleteButton model={model} openHome={async () => undefined} />
        </StatusProvider>
      </AppServicesProvider>
    </RegistryProvider>,
  );
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: appI18n.t("projectEdit.deleteProject") }));
  await user.click(screen.getByRole("button", { name: appI18n.t("app.cancel") }));
  expect(remove).not.toHaveBeenCalled();
  for (let attempt = 1; attempt <= 2; attempt += 1) {
    await user.click(screen.getByRole("button", { name: appI18n.t("projectEdit.deleteProject") }));
    await user.click(
      within(await screen.findByRole("dialog")).getByRole("button", {
        name: appI18n.t("projectEdit.deleteConfirm"),
      }),
    );
    await waitFor(() => {
      expect(remove).toHaveBeenCalledTimes(attempt);
    });
    expect(screen.queryByRole("dialog") === null).toBe(true);
  }
  await user.click(screen.getByRole("button", { name: appI18n.t("projectEdit.deleteProject") }));
  view.unmount();
  expect(screen.queryByRole("dialog") === null).toBe(true);
  expect(remove).toHaveBeenCalledTimes(2);
  expect(openWindow).not.toHaveBeenCalled();
});

it.each([
  ["accepted", 1],
  ["stale", 0],
] as const)("navigates Home only when the original sidebar close is %s", async (outcome, homeCalls) => {
  const services = createTestServices([]);
  const remove = vi
    .spyOn(services.api, "deleteProject")
    .mockResolvedValue({ projectID: "project-1", deleted: true, blockers: [] });
  const navigator = createTestSidebarNavigator({ close: vi.fn(() => outcome) });
  const openHome = vi.fn(async () => undefined);
  const client = new QueryClient();
  const push = vi.fn();
  const model = createProjectEditViewModel({
    services,
    client,
    projectID: "project-1",
    t: appI18n.t,
    push,
    navigator,
  });
  const user = userEvent.setup();
  const view = render(
    <RegistryProvider>
      <AppServicesProvider services={services}>
        <StatusProvider>
          <ProjectDeleteButton model={model} openHome={openHome} />
        </StatusProvider>
      </AppServicesProvider>
    </RegistryProvider>,
  );
  const selectedProjectOpenHome = vi.fn(async () => undefined);
  view.rerender(
    <RegistryProvider>
      <AppServicesProvider services={services}>
        <StatusProvider>
          <ProjectDeleteButton model={model} openHome={selectedProjectOpenHome} />
        </StatusProvider>
      </AppServicesProvider>
    </RegistryProvider>,
  );
  await user.click(screen.getByRole("button", { name: appI18n.t("projectEdit.deleteProject") }));
  expect(remove).not.toHaveBeenCalled();
  await user.click(
    within(await screen.findByRole("dialog")).getByRole("button", {
      name: appI18n.t("projectEdit.deleteConfirm"),
    }),
  );
  await waitFor(() => {
    expect(push).toHaveBeenCalledWith(expect.objectContaining({ tone: "success" }));
  });
  expect(navigator.close).toHaveBeenCalledTimes(1);
  expect(selectedProjectOpenHome).toHaveBeenCalledTimes(homeCalls);
  expect(openHome).not.toHaveBeenCalled();
  expect(remove).toHaveBeenCalledTimes(1);
});
