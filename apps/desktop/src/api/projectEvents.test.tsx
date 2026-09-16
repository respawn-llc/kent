import { RegistryProvider, useAtomSuspense } from "@effect/atom-react";
import { act, render, waitFor } from "@testing-library/react";
import * as Effect from "effect/Effect";
import * as Stream from "effect/Stream";
import * as Atom from "effect/unstable/reactivity/Atom";
import { Component, type ReactElement } from "react";

import { createTestServices } from "@/test-support/app-services";
import type { ProjectObservation } from "./projectEvents";

it("opens a Project observation only while mounted and closes it on disposal", async () => {
  const { api, transport } = createTestServices([]);
  const subscribe = vi.spyOn(transport, "subscribe");
  const events = api.subscribeProject("project-1");
  expect(subscribe).not.toHaveBeenCalled();
  const observation = Atom.make(Stream.runForEach(events, () => Effect.void).pipe(Effect.as(null)), {
    initialValue: null,
  });
  function Reader() {
    useAtomSuspense(observation);
    return null;
  }
  const view = render(
    <RegistryProvider>
      <Reader />
    </RegistryProvider>,
  );
  await waitFor(() => {
    expect(subscribe).toHaveBeenCalledTimes(1);
  });
  const subscription = subscribe.mock.results[0];
  if (subscription?.type !== "return") throw new Error("Project observation did not subscribe.");
  const close = vi.spyOn(subscription.value, "close");
  await act(async () => {
    view.unmount();
  });
  await waitFor(() => {
    expect(close).toHaveBeenCalledTimes(1);
  });
});

it("retains 1,000 queued observations and reports each incoming overflow without ending production observation", async () => {
  const { api, transport, logger } = createTestServices([]);
  const received: ProjectObservation[] = [];
  const subscribe = vi.spyOn(transport, "subscribe");
  const observation = Atom.make(
    Stream.runForEach(api.subscribeProject("project-1"), (value) =>
      Effect.sync(() => {
        received.push(value);
      }),
    ).pipe(Effect.as(null)),
    { initialValue: null },
  );
  function Reader() {
    useAtomSuspense(observation);
    return null;
  }
  render(
    <RegistryProvider>
      <Reader />
    </RegistryProvider>,
  );
  await waitFor(() => {
    expect(subscribe).toHaveBeenCalledTimes(1);
  });
  await act(async () => {
    for (let index = 0; index < 1002; index++) {
      transport.emit("workflow.project", wireEvent(index));
    }
  });
  expect(received.filter((value) => value.kind === "event")).toHaveLength(1000);
  expect(logger.entries()).toHaveLength(2);
  expect(logger.entries()[0]?.context).toMatchObject({
    projectID: "project-1",
    primaryEntityID: "task-1000",
  });
  await act(async () => {
    transport.emit("workflow.project", wireEvent(1002));
  });
  expect(received.at(-1)).toMatchObject({ kind: "event", event: { primaryEntityID: "task-1002" } });
});

function wireEvent(index: number) {
  return {
    event: {
      resource: "task",
      action: "updated",
      occurred_at_unix_ms: 1,
      primary_entity_id: `task-${String(index)}`,
      project_id: "project-1",
      workflow_id: "11111111-1111-4111-8111-111111111111",
    },
  };
}

it("surfaces debug overflow at the mounted React boundary and closes observation", async () => {
  vi.stubEnv("KENT_DEBUG", "true");
  const consoleError = vi.spyOn(console, "error").mockImplementation(() => undefined);
  try {
    const { api, transport, logger } = createTestServices([]);
    const subscribe = vi.spyOn(transport, "subscribe");
    const caught = vi.fn();
    const observation = Atom.make(
      Stream.runForEach(api.subscribeProject("project-1"), () => Effect.void).pipe(Effect.as(null)),
      { initialValue: null },
    );
    class Boundary extends Component<{ content: ReactElement }, { failed: boolean }> {
      override state = { failed: false };
      static getDerivedStateFromError() {
        return { failed: true };
      }
      override componentDidCatch(error: Error) {
        caught(error);
      }
      override render() {
        return this.state.failed ? null : this.props.content;
      }
    }
    function Reader() {
      useAtomSuspense(observation);
      return null;
    }
    render(
      <RegistryProvider>
        <Boundary content={<Reader />} />
      </RegistryProvider>,
    );
    await waitFor(() => {
      expect(subscribe).toHaveBeenCalledTimes(1);
    });
    const subscription = subscribe.mock.results[0];
    if (subscription?.type !== "return") throw new Error("Project observation did not subscribe.");
    const close = vi.spyOn(subscription.value, "close");
    await act(async () => {
      for (let index = 0; index < 1001; index++) transport.emit("workflow.project", wireEvent(index));
    });
    await waitFor(() => {
      expect(caught).toHaveBeenCalledTimes(1);
    });
    expect(logger.entries()).toHaveLength(1);
    await waitFor(() => {
      expect(close).toHaveBeenCalledTimes(1);
    });
  } finally {
    vi.unstubAllEnvs();
    consoleError.mockRestore();
  }
});
