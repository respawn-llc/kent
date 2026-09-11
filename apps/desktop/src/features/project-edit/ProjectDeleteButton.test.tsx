import { RegistryProvider } from "@effect/atom-react";
import { QueryClient } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AppServicesProvider, StatusProvider } from "@/app-facade";
import { appI18n } from "@/i18n";
import { createTestServices } from "@/test-support/app-services";
import { createTestSidebarNavigator } from "@/test-support/sidebar";
import { createProjectEditViewModel } from "./ProjectEditViewModel";
import { ProjectDeleteButton } from "./ProjectDeleteButton";

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
          <ProjectDeleteButton model={model} projectID="project-1" openHome={openHome} />
        </StatusProvider>
      </AppServicesProvider>
    </RegistryProvider>,
  );
  const selectedProjectOpenHome = vi.fn(async () => undefined);
  view.rerender(
    <RegistryProvider>
      <AppServicesProvider services={services}>
        <StatusProvider>
          <ProjectDeleteButton model={model} projectID="project-1" openHome={selectedProjectOpenHome} />
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
  expect(navigator.close).toHaveBeenCalledOnce();
  expect(selectedProjectOpenHome).toHaveBeenCalledTimes(homeCalls);
  expect(openHome).not.toHaveBeenCalled();
  expect(remove).toHaveBeenCalledOnce();
});
