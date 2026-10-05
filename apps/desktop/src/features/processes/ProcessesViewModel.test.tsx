import { RegistryProvider, useAtomValue, useAtomSet } from "@effect/atom-react";
import { QueryClient } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { useMemo } from "react";

import { queryKeys } from "@/app-facade";
import { createTestServices } from "@/test-support/app-services";
import { processObservationFixture, processObservationFixtureRoute } from "@/test-support/api";
import { createProcessesViewModel } from "./ProcessesViewModel";

const target = { projectID: "project-1", sessionID: "session-1" };

function setup() {
  const services = createTestServices([processObservationFixtureRoute]);
  const observation = processObservationFixture(services.transport);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData(queryKeys.processes(target.projectID, target.sessionID), []);
  const view = renderHook(
    (selected) => {
      const model = useMemo(
        () => createProcessesViewModel({ api: services.api, client, target: selected }),
        [selected],
      );
      return {
        state: useAtomValue(model.state),
        retry: useAtomSet(model.retry),
      };
    },
    {
      initialProps: target,
      wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider>,
    },
  );
  return { ...services, ...view, client, observation };
}

it("waits for this observation's initial list even when Query already holds content", async () => {
  const view = setup();
  expect(view.result.current.state.kind).toBe("loading");
  await waitFor(() => {
    expect(view.transport.descriptorSubscriptions).toHaveLength(1);
  });
  await act(async () => {
    view.observation.publish();
  });
  expect(view.result.current.state).toMatchObject({ kind: "ready", processes: [] });
  expect(view.transport.descriptorCalls).toHaveLength(0);
});

it("replaces all content in server order, including removal of previous rows", async () => {
  const view = setup();
  await waitFor(() => {
    expect(view.transport.descriptorSubscriptions).toHaveLength(1);
  });
  await act(async () => {
    view.observation.publish("a", "b");
  });
  expect(view.result.current.state).toMatchObject({ kind: "ready", processes: [{ id: "a" }, { id: "b" }] });
  await act(async () => {
    view.observation.publish("c", "a");
  });
  expect(view.result.current.state).toMatchObject({ kind: "ready", processes: [{ id: "c" }, { id: "a" }] });
  expect(view.client.getQueryData(queryKeys.processes(target.projectID, target.sessionID))).toEqual(
    view.result.current.state.kind === "ready" ? view.result.current.state.processes : undefined,
  );
  expect(view.transport.descriptorCalls).toHaveLength(0);
});

it("hides cached content on failure and retries only on an explicit action with fresh loading", async () => {
  const view = setup();
  await waitFor(() => {
    expect(view.transport.descriptorSubscriptions).toHaveLength(1);
  });
  await act(async () => {
    view.observation.publish("a");
  });
  await act(async () => {
    view.observation.fail(new Error("lost"));
  });
  expect(view.result.current.state.kind).toBe("error");
  expect(view.result.current.state).not.toHaveProperty("processes");
  await waitFor(() => {
    expect(view.transport.descriptorSubscriptions).toHaveLength(0);
  });
  expect(view.transport.descriptorSubscriptionStarts).toHaveLength(1);
  await act(async () => {
    view.result.current.retry(undefined);
  });
  expect(view.result.current.state.kind).toBe("loading");
  await waitFor(() => {
    expect(view.transport.descriptorSubscriptionStarts).toHaveLength(2);
  });
  await act(async () => {
    view.observation.publish("b");
  });
  expect(view.result.current.state).toMatchObject({ kind: "ready", processes: [{ id: "b" }] });
  expect(view.transport.descriptorCalls).toHaveLength(0);
});

it("switches observation with the selected Session and releases it on close without reading on focus", async () => {
  const view = setup();
  await waitFor(() => {
    expect(view.transport.descriptorSubscriptions).toHaveLength(1);
  });
  await act(async () => {
    view.observation.publish("a");
  });
  await act(async () => {
    window.dispatchEvent(new Event("blur"));
    window.dispatchEvent(new Event("focus"));
  });
  expect(view.transport.descriptorSubscriptionStarts).toHaveLength(1);
  view.rerender({ ...target, sessionID: "session-2" });
  expect(view.result.current.state.kind).toBe("loading");
  await waitFor(() => {
    expect(view.transport.descriptorSubscriptionStarts).toHaveLength(2);
  });
  await waitFor(() => {
    expect(view.transport.descriptorSubscriptions).toHaveLength(1);
  });
  expect(view.transport.descriptorSubscriptionStarts[1]?.request).toMatchObject({ sessionId: "session-2" });
  view.unmount();
  await waitFor(() => {
    expect(view.transport.descriptorSubscriptions).toHaveLength(0);
  });
  expect(view.transport.descriptorCalls).toHaveLength(0);
});
