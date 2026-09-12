import { RegistryProvider, useAtomMount } from "@effect/atom-react";
import { QueryClient } from "@tanstack/react-query";
import { act, render, waitFor } from "@testing-library/react";
import { appI18n } from "@/i18n";
import { queryKeys } from "@/app-facade";
import { createTestServices } from "@/test-support/app-services";
import { deferred } from "@/test-support/chat-runtime";
import { createProjectEditViewModel, type ProjectEditViewModel } from "./ProjectEditViewModel";

function Reader({ model }: { model: ProjectEditViewModel }) {
  useAtomMount(model.workspaceChanges);
  return null;
}

function setup() {
  const services = createTestServices([]);
  const client = new QueryClient();
  const push = vi.fn();
  const register = vi.spyOn(services.nativeBridge.projectWorkspace, "onChanged");
  const release = vi.fn();
  register.mockResolvedValue(release);
  const model = createProjectEditViewModel({ services, client, projectID: "project-1", t: appI18n.t, push });
  return { model, client, push, register, release };
}

it("reports failed native registration through an ordinary notification", async () => {
  const fixture = setup();
  fixture.register.mockRejectedValue(new Error("Registration failed"));
  render(
    <RegistryProvider>
      <Reader model={fixture.model} />
    </RegistryProvider>,
  );
  await waitFor(() => {
    expect(fixture.push).toHaveBeenCalledWith(expect.objectContaining({ tone: "danger" }));
  });
  expect(fixture.register).toHaveBeenCalledTimes(1);
});

it("shares one registration across readers and releases after the final reader leaves", async () => {
  const fixture = setup();
  const view = render(
    <RegistryProvider>
      <Reader model={fixture.model} />
      <Reader model={fixture.model} />
    </RegistryProvider>,
  );
  await waitFor(() => {
    expect(fixture.register).toHaveBeenCalledTimes(1);
  });
  view.rerender(
    <RegistryProvider>
      <Reader model={fixture.model} />
    </RegistryProvider>,
  );
  expect(fixture.release).not.toHaveBeenCalled();
  const invalidate = vi.spyOn(fixture.client, "invalidateQueries");
  const handler = fixture.register.mock.calls[0]?.[0];
  await act(async () => handler?.({ projectID: "project-1" }));
  expect(invalidate).toHaveBeenCalledWith({ queryKey: queryKeys.projectEdit("project-1") });
  expect(invalidate).not.toHaveBeenCalledWith(
    expect.objectContaining({ queryKey: queryKeys.projectWorkspaceCatalog("project-1") }),
  );
  view.rerender(<RegistryProvider />);
  await waitFor(() => {
    expect(fixture.release).toHaveBeenCalledTimes(1);
  });
});

it("releases registration that resolves after its final reader detached", async () => {
  const fixture = setup();
  const registration = deferred<() => void>();
  fixture.register.mockReturnValue(registration.promise);
  const view = render(
    <RegistryProvider>
      <Reader model={fixture.model} />
    </RegistryProvider>,
  );
  await waitFor(() => {
    expect(fixture.register).toHaveBeenCalledTimes(1);
  });
  view.rerender(<RegistryProvider />);
  await act(async () => {
    registration.resolve(fixture.release);
    await registration.promise;
  });
  await waitFor(() => {
    expect(fixture.release).toHaveBeenCalledTimes(1);
  });
});

it("keeps one selected-Project refresh waiting behind an in-flight refresh", async () => {
  const fixture = setup();
  const pending = deferred<undefined>();
  const invalidate = vi
    .spyOn(fixture.client, "invalidateQueries")
    .mockImplementationOnce(async () => pending.promise);
  render(
    <RegistryProvider>
      <Reader model={fixture.model} />
    </RegistryProvider>,
  );
  await waitFor(() => {
    expect(fixture.register).toHaveBeenCalledTimes(1);
  });
  const handler = fixture.register.mock.calls[0]?.[0];
  if (handler === undefined) throw new Error("Native registration did not receive a handler");
  await act(async () => {
    handler({ projectID: "project-1" });
  });
  const firstRefreshCount = invalidate.mock.calls.length;
  await act(async () => {
    for (let index = 0; index < 100; index++) handler({ projectID: "project-1" });
    handler({ projectID: "another-project" });
  });
  expect(invalidate).toHaveBeenCalledTimes(firstRefreshCount);
  await act(async () => {
    pending.resolve(undefined);
    await pending.promise;
  });
  expect(invalidate).toHaveBeenCalledTimes(firstRefreshCount * 2);
});
