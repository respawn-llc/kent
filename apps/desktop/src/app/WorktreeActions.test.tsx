import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { RegistryProvider } from "@effect/atom-react";

import { useSidebarShell } from "@/app-facade";
import { appI18n } from "@/i18n";
import {
  worktreeBrowserFixtureEntry,
  worktreeBrowserFixtureRoute,
  worktreeQueryFixtureRoutes,
  worktreeAcknowledgementFixture,
  worktreeDeleteSuccessFixture,
  worktreeErrorFixture,
} from "@/test-support/api";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { changedIdentity, deferred, hydration, mainViewRead, runtimeApi } from "@/test-support/chat-runtime";
import { WorktreeFixtureSurface } from "./dev-showcase/WorktreeFixtureSurface";

const notifications = vi.hoisted(() => vi.fn());
vi.mock("@/ui", async (importOriginal) => ({
  ...(await importOriginal()),
  showStatusToast: notifications,
}));

async function setup() {
  notifications.mockClear();
  const route = worktreeBrowserFixtureRoute(
    [worktreeBrowserFixtureEntry("registered", false)],
    "session-title",
  );
  let list = route.result;
  const services = createTestServices([
    { descriptor: route.descriptor, resultFactory: () => list },
    ...worktreeQueryFixtureRoutes(),
  ]);
  const runtime = runtimeApi({ reads: [Promise.resolve(mainViewRead()), Promise.resolve(mainViewRead())] });
  const client = new QueryClient();
  const push = notifications;
  function Surface() {
    const shell = useSidebarShell();
    return <button onClick={() => shell.close()}>Dismiss fixture sidebar</button>;
  }
  render(
    <TestAppProviders services={services}>
      <QueryClientProvider client={client}>
        <RegistryProvider>
          <WorktreeFixtureSurface api={runtime.api}>
            <Surface />
          </WorktreeFixtureSurface>
        </RegistryProvider>
      </QueryClientProvider>
    </TestAppProviders>,
  );
  await waitFor(() => {
    expect(client.isFetching()).toBe(0);
  });
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "Workspace" }));
  await screen.findByRole("button", { name: appI18n.t("chat.worktree.switch") });
  await waitFor(() => {
    expect(client.isFetching()).toBe(0);
  });
  return {
    services,
    runtime,
    client,
    push,
    user,
    listReads: () =>
      services.transport.descriptorCalls.filter((call) => call.descriptor === route.descriptor).length,
    removeRows: () => {
      list = worktreeBrowserFixtureRoute().result;
    },
  };
}

it("refreshes the matching visible list on live outcomes, but not the retained closed control", async () => {
  const { services, runtime, client, push, user } = await setup();
  const handler = runtime.handlers[0];
  if (handler === undefined) throw new Error("Missing runtime handler");
  act(() => {
    handler.onOpen?.();
    handler.onEvent({ sequence: 1, kind: "hydration", payload: hydration() });
  });
  const before = services.transport.descriptorCalls.length;
  act(() => {
    handler.onEvent({
      sequence: 2,
      kind: "worktree_transition_outcome",
      payload: {
        OperationID: "operation-1",
        Transition: "delete",
        State: "completed",
      },
    });
  });
  await waitFor(() => {
    expect(services.transport.descriptorCalls).toHaveLength(before + 1);
  });
  await waitFor(() => {
    expect(client.isFetching()).toBe(0);
  });
  await user.click(screen.getByRole("button", { name: "Dismiss fixture sidebar" }));
  act(() => {
    handler.onEvent({
      sequence: 3,
      kind: "worktree_transition_outcome",
      payload: {
        OperationID: "operation-2",
        Transition: "enter",
        State: "failed",
        Failure: { Code: "failed", Detail: "Switch failed" },
      },
    });
  });
  await waitFor(() => {
    expect(push).toHaveBeenCalledTimes(1);
  });
  expect(services.transport.descriptorCalls).toHaveLength(before + 1);
});

it.each([false, true])(
  "applies Switch only from the target broadcast and respects dismissal (dismissed=%s)",
  async (dismissed) => {
    const view = await setup();
    const response = deferred<Awaited<ReturnType<typeof view.services.api.switchWorktree>>>();
    const send = vi.spyOn(view.services.api, "switchWorktree").mockReturnValue(response.promise);
    await view.user.click(screen.getByRole("button", { name: appI18n.t("chat.worktree.switch") }));
    await waitFor(() => {
      expect(send).toHaveBeenCalledTimes(1);
    });
    expect(screen.getByRole("button", { name: appI18n.t("chat.worktree.switchPending") })).toBeDisabled();
    if (dismissed) {
      await view.user.click(screen.getByRole("button", { name: "Dismiss fixture sidebar" }));
      await view.user.click(screen.getByRole("button", { name: "Workspace" }));
      await view.user.click(await screen.findByRole("button", { name: appI18n.t("chat.worktree.create") }));
    }
    await act(async () => {
      response.resolve(worktreeAcknowledgementFixture());
      await response.promise;
    });
    expect(screen.getByRole("button", { name: "Workspace" })).toBeInTheDocument();
    expect(view.push).not.toHaveBeenCalled();
    expect(screen.queryAllByRole("textbox", { name: appI18n.t("chat.worktree.targetLabel") })).toHaveLength(
      dismissed ? 1 : 0,
    );
    const handler = view.runtime.handlers[0];
    if (handler === undefined) throw new Error("Runtime handler required");
    act(() => {
      handler.onOpen?.();
      handler.onEvent({ sequence: 1, kind: "hydration", payload: hydration() });
      handler.onEvent({ sequence: 2, kind: "session_identity", payload: changedIdentity() });
    });
    await screen.findByRole("button", { name: "Detached" });
    expect(send).toHaveBeenCalledTimes(1);
  },
);

it("reconnects an open creation page to the list without inferring completion or replaying Create", async () => {
  const view = await setup();
  const response = deferred<Awaited<ReturnType<typeof view.services.api.createWorktree>>>();
  const send = vi.spyOn(view.services.api, "createWorktree").mockReturnValue(response.promise);
  const switchWorktree = vi.spyOn(view.services.api, "switchWorktree");
  await view.user.click(screen.getByRole("button", { name: appI18n.t("chat.worktree.create") }));
  await view.user.click(await screen.findByRole("button", { name: appI18n.t("chat.worktree.createSubmit") }));
  await waitFor(() => {
    expect(send).toHaveBeenCalledTimes(1);
  });
  const before = view.listReads();
  act(() => {
    view.services.transport.connection.set("disconnected");
    view.services.transport.connection.set("connected");
  });
  await screen.findByRole("button", { name: appI18n.t("chat.worktree.create") });
  await waitFor(() => {
    expect(view.runtime.getMainView).toHaveBeenCalledTimes(2);
  });
  expect(view.listReads()).toBe(before + 1);
  expect(send).toHaveBeenCalledTimes(1);
  expect(switchWorktree).not.toHaveBeenCalled();
  await act(async () => {
    response.reject(worktreeErrorFixture("form"));
    await response.promise.catch(() => undefined);
  });
});

it("does not reattach Create or let its automatic Switch close a reopened creation page", async () => {
  const view = await setup();
  const releaseCreate = deferred<undefined>();
  const actualCreate = view.services.api.createWorktree.bind(view.services.api);
  const send = vi.spyOn(view.services.api, "createWorktree").mockImplementation(async (input) => {
    const result = await actualCreate(input);
    await releaseCreate.promise;
    return result;
  });
  const switchResponse = deferred<Awaited<ReturnType<typeof view.services.api.switchWorktree>>>();
  const switchWorktree = vi
    .spyOn(view.services.api, "switchWorktree")
    .mockReturnValue(switchResponse.promise);
  await view.user.click(screen.getByRole("button", { name: appI18n.t("chat.worktree.create") }));
  await view.user.click(await screen.findByRole("button", { name: appI18n.t("chat.worktree.createSubmit") }));
  await waitFor(() => {
    expect(send).toHaveBeenCalledTimes(1);
  });
  await view.user.click(screen.getByRole("button", { name: "Dismiss fixture sidebar" }));
  await view.user.click(screen.getByRole("button", { name: "Workspace" }));
  await view.user.click(await screen.findByRole("button", { name: appI18n.t("chat.worktree.create") }));
  expect(await screen.findByRole("button", { name: appI18n.t("chat.worktree.createSubmit") })).toBeEnabled();
  await act(async () => {
    releaseCreate.resolve(undefined);
  });
  await waitFor(() => {
    expect(switchWorktree).toHaveBeenCalledTimes(1);
  });
  await act(async () => {
    switchResponse.resolve(worktreeAcknowledgementFixture());
    await switchResponse.promise;
  });
  expect(screen.getByRole("textbox", { name: appI18n.t("chat.worktree.targetLabel") })).toBeInTheDocument();
  expect(send).toHaveBeenCalledTimes(1);
});

it.each(["setup", "pre-retention"] as const)(
  "owns dismissed %s failure without reopening or refreshing the retained control",
  async (kind) => {
    const view = await setup();
    const response = deferred<Awaited<ReturnType<typeof view.services.api.createWorktree>>>();
    const send = vi.spyOn(view.services.api, "createWorktree").mockReturnValue(response.promise);
    const diagnostic = `${kind} failure`;
    const error = worktreeErrorFixture(kind === "setup" ? "setup" : "form", diagnostic);
    await view.user.click(screen.getByRole("button", { name: appI18n.t("chat.worktree.create") }));
    await view.user.click(
      await screen.findByRole("button", { name: appI18n.t("chat.worktree.createSubmit") }),
    );
    await waitFor(() => {
      expect(send).toHaveBeenCalledTimes(1);
    });
    await view.user.click(screen.getByRole("button", { name: "Dismiss fixture sidebar" }));
    const before = view.listReads();
    await act(async () => {
      response.reject(error);
      await response.promise.catch(() => undefined);
    });
    if (kind === "setup")
      await waitFor(() => {
        expect(view.push).toHaveBeenCalledTimes(1);
      });
    expect(view.push).toHaveBeenCalledTimes(kind === "setup" ? 1 : 0);
    expect(
      screen.queryByRole("textbox", { name: appI18n.t("chat.worktree.targetLabel") }),
    ).not.toBeInTheDocument();
    expect(view.listReads()).toBe(before);
  },
);

it("keeps the original form and previous target when automatic Switch fails", async () => {
  const view = await setup();
  const switchWorktree = vi
    .spyOn(view.services.api, "switchWorktree")
    .mockRejectedValue(new Error("Automatic Switch failed"));
  await view.user.click(screen.getByRole("button", { name: appI18n.t("chat.worktree.create") }));
  const before = view.listReads();
  await view.user.click(await screen.findByRole("button", { name: appI18n.t("chat.worktree.createSubmit") }));
  await waitFor(() => {
    expect(view.push).toHaveBeenCalledTimes(1);
  });
  expect(switchWorktree).toHaveBeenCalledTimes(1);
  expect(screen.getByRole("textbox", { name: appI18n.t("chat.worktree.targetLabel") })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Workspace" })).toBeInTheDocument();
  expect(view.listReads()).toBe(before);
});

it("discards the creation draft on Back and initializes a fresh form on re-entry", async () => {
  const view = await setup();
  await view.user.click(screen.getByRole("button", { name: appI18n.t("chat.worktree.create") }));
  const targetInput = await screen.findByRole("textbox", { name: appI18n.t("chat.worktree.targetLabel") });
  expect(targetInput).toHaveValue("session-title");
  await view.user.clear(targetInput);
  await view.user.type(targetInput, "edited");
  await view.user.click(screen.getByRole("button", { name: appI18n.t("chat.worktree.back") }));
  await view.user.click(await screen.findByRole("button", { name: appI18n.t("chat.worktree.create") }));
  expect(await screen.findByRole("textbox", { name: appI18n.t("chat.worktree.targetLabel") })).toHaveValue(
    "session-title",
  );
});

it.each([false, true])(
  "refreshes non-current deletion only when the list remains open (dismissed=%s)",
  async (dismissed) => {
    const view = await setup();
    const response = deferred<Awaited<ReturnType<typeof view.services.api.deleteWorktree>>>();
    const send = vi.spyOn(view.services.api, "deleteWorktree").mockReturnValue(response.promise);
    await view.user.click(screen.getByRole("button", { name: appI18n.t("chat.worktree.delete") }));
    await view.user.click(await screen.findByRole("button", { name: appI18n.t("chat.worktree.confirm") }));
    await waitFor(() => {
      expect(send).toHaveBeenCalledTimes(1);
    });
    const before = view.listReads();
    if (dismissed) await view.user.click(screen.getByRole("button", { name: "Dismiss fixture sidebar" }));
    view.removeRows();
    await act(async () => {
      response.resolve(worktreeDeleteSuccessFixture());
      await response.promise;
    });
    await waitFor(() => {
      expect(view.client.isFetching()).toBe(0);
    });
    expect(view.listReads()).toBe(before + (dismissed ? 0 : 1));
    if (!dismissed)
      expect(
        screen.queryByRole("button", { name: appI18n.t("chat.worktree.delete") }),
      ).not.toBeInTheDocument();
  },
);
