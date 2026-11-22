import { RegistryProvider, useAtomValue } from "@effect/atom-react";
import { act, renderHook, waitFor } from "@testing-library/react";
import { expect, it, vi } from "bun:test";
import * as Effect from "effect/Effect";
import * as Stream from "effect/Stream";
import * as Atom from "effect/reactivity/Atom";
import { create } from "@app/server-api-contract";
import { ViewService } from "@app/server-api-contract/gen/kent/api/process/process_pb";
import type { ReactNode } from "react";

import { createTestServices } from "@/test-support/app-services";
import { processObservationFixtureRoute } from "@/test-support/api";
import type { DesktopProcess, ProcessScope } from "./processes";

function fixture(
  startResult = processObservationFixtureRoute.startResult,
  target: ProcessScope = {
    kind: "session",
    projectID: "project-1",
    sessionID: "11111111-1111-4111-8111-111111111111",
  },
) {
  const services = createTestServices([{ ...processObservationFixtureRoute, startResult }]);
  const received: (readonly DesktopProcess[])[] = [];
  const failed = vi.fn();
  const stream = services.api.observeProcesses(target);
  const observation = Atom.make(
    Stream.runForEach(stream, (list) =>
      Effect.sync(() => {
        received.push(list);
      }),
    ).pipe(
      Effect.catch((error) =>
        Effect.sync(() => {
          failed(error);
        }),
      ),
    ),
  );
  return { ...services, received, failed, observation };
}

it("observes the exact Project without a Session target", async () => {
  const context = fixture(undefined, {
    kind: "project",
    projectID: "project-1",
    workspace: { workspaceID: "workspace-1" },
  });
  const view = mount(context);
  await waitFor(() => {
    expect(context.transport.descriptorSubscriptionStarts).toHaveLength(1);
  });
  expect(context.transport.descriptorSubscriptionStarts[0]?.request).toMatchObject({
    scope: { case: "projectId", value: "project-1" },
  });
  view.unmount();
});

function mount(context: ReturnType<typeof fixture>) {
  return renderHook(() => useAtomValue(context.observation), {
    wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider>,
  });
}

it("observes initial lists only while mounted and releases the descriptor subscription", async () => {
  const context = fixture();
  expect(context.transport.descriptorSubscriptions).toHaveLength(0);
  const view = mount(context);
  await waitFor(() => {
    expect(context.transport.descriptorSubscriptions).toHaveLength(1);
  });
  await act(async () => {
    context.transport.openDescriptor(ViewService.method.observe);
    context.transport.emitDescriptor(
      ViewService.method.observe,
      ViewService.method.event,
      create(ViewService.method.event.input),
    );
  });
  expect(context.received).toEqual([[]]);
  view.unmount();
  await waitFor(() => {
    expect(context.transport.descriptorSubscriptions).toHaveLength(0);
  });
  expect(context.transport.descriptorCalls).toHaveLength(0);
});

it.each([-1n, 0n, 255n, 2147483648n, 3221225477n, 4294967295n])(
  "preserves process exit status %s",
  async (exitCode) => {
    const context = fixture();
    const view = mount(context);
    await waitFor(() => {
      expect(context.transport.descriptorSubscriptions).toHaveLength(1);
    });
    await act(async () => {
      context.transport.openDescriptor(ViewService.method.observe);
      context.transport.emitDescriptor(
        ViewService.method.observe,
        ViewService.method.event,
        create(ViewService.method.event.input, {
          processes: [
            {
              id: "process-status",
              ownerSessionId: "11111111-1111-4111-8111-111111111111",
              state: "completed",
              command: "child",
              workdir: "/tmp",
              startedAt: { seconds: 1n, nanos: 0 },
              exitCode,
            },
          ],
        }),
      );
    });
    expect(context.received[0]?.[0]?.exitCode).toBe(Number(exitCode));
    view.unmount();
  },
);

it.each(["failure", "completion", "overflow", "rejection"] as const)(
  "fails observation on %s without recovery",
  async (kind) => {
    const context = fixture(
      kind === "rejection"
        ? create(ViewService.method.observe.output, {
            outcome: {
              case: "error",
              value: {
                code: "internal_failure",
                detail: {
                  case: "internalFailure",
                  value: { operation: "process.observe", cause: "unavailable" },
                },
              },
            },
          })
        : undefined,
    );
    mount(context);
    await waitFor(() => {
      expect(context.transport.descriptorSubscriptions).toHaveLength(1);
    });
    await act(async () => {
      if (kind === "rejection") context.transport.openDescriptor(ViewService.method.observe);
      else if (kind === "failure")
        context.transport.failDescriptor(ViewService.method.observe, new Error("disconnected"));
      else if (kind === "completion") {
        context.transport.completeDescriptor(
          ViewService.method.observe,
          ViewService.method.complete,
          create(ViewService.method.complete.input),
        );
      } else {
        for (let index = 0; index < 3; index++) {
          context.transport.emitDescriptor(
            ViewService.method.observe,
            ViewService.method.event,
            create(ViewService.method.event.input),
          );
        }
      }
    });
    await waitFor(() => {
      expect(context.failed).toHaveBeenCalledTimes(1);
    });
    await waitFor(() => {
      expect(context.transport.descriptorSubscriptions).toHaveLength(0);
    });
    expect(context.transport.descriptorSubscriptionStarts).toHaveLength(1);
    expect(context.transport.descriptorCalls).toHaveLength(0);
  },
);
