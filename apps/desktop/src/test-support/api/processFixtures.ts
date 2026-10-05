import { create } from "@app/server-api-contract";
import {
  BackgroundProcessSchema,
  ViewService,
} from "@app/server-api-contract/gen/kent/api/process/process_pb";
import type { FakeRpcTransport, FakeRoute } from "./index";

export const processObservationFixtureRoute = {
  subscriptionDescriptor: ViewService.method.observe,
  startResult: create(ViewService.method.observe.output, { outcome: { case: "success", value: {} } }),
} satisfies FakeRoute;

export function processObservationFixture(transport: FakeRpcTransport) {
  return {
    publish(...ids: string[]) {
      transport.emitDescriptor(
        ViewService.method.observe,
        ViewService.method.event,
        create(ViewService.method.event.input, {
          processes: ids.map((id) =>
            create(BackgroundProcessSchema, {
              id,
              ownerSessionId: "session-1",
              state: "running",
              command: "sleep 30",
              workdir: "/workspace",
              startedAt: { seconds: 1n, nanos: 0 },
              lastUpdatedAt: { seconds: 1n, nanos: 0 },
              running: true,
              backgrounded: true,
            }),
          ),
        }),
      );
    },
    fail(error: Error) {
      transport.failDescriptor(ViewService.method.observe, error);
    },
  };
}
