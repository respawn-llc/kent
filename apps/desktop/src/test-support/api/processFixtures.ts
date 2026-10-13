import { create } from "@app/server-api-contract";
import {
  BackgroundProcessSchema,
  ViewService,
} from "@app/server-api-contract/gen/kent/api/process/process_pb";
import type { FakeRpcTransport, FakeRoute } from "./index";
import type { DesktopProcess } from "@/api";

export const processObservationFixtureRoute = {
  subscriptionDescriptor: ViewService.method.observe,
  startResult: create(ViewService.method.observe.output, { outcome: { case: "success", value: {} } }),
} satisfies FakeRoute;

export function processObservationFixture(transport: FakeRpcTransport) {
  const publishRows = (processes: readonly Pick<DesktopProcess, "id" | "state" | "running">[]) => {
    transport.emitDescriptor(
      ViewService.method.observe,
      ViewService.method.event,
      create(ViewService.method.event.input, {
        processes: processes.map((process) =>
          create(BackgroundProcessSchema, {
            id: process.id,
            ownerSessionId: "session-1",
            state: process.state,
            command: "sleep 30",
            workdir: "/workspace",
            startedAt: { seconds: 1n, nanos: 0 },
            lastUpdatedAt: { seconds: 1n, nanos: 0 },
            running: process.running,
            ...(process.running ? {} : { finishedAt: { seconds: 2n, nanos: 0 } }),
            backgrounded: true,
          }),
        ),
      }),
    );
  };
  return {
    publishRows,
    publish(...ids: string[]) {
      publishRows(ids.map((id) => ({ id, state: "running", running: true })));
    },
    fail(error: Error) {
      transport.failDescriptor(ViewService.method.observe, error);
    },
  };
}
