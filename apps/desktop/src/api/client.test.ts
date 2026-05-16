import { BuilderApiClient } from "./client";
import { ContractError } from "./errors";
import { FakeRpcTransport } from "./fakeTransport";

describe("BuilderApiClient", () => {
  it("parses readiness and sends mutation params through typed method boundary", async () => {
    const transport = new FakeRpcTransport([
      {
        method: "server.readiness.get",
        result: {
          ready: true,
          server_id: "server-1",
          server_version: "1.3.0",
          protocol_version: "2",
          auth_ready: true,
          auth_required: false,
          endpoint: "ws://127.0.0.1:53082/rpc",
        },
      },
      { method: "workflow.task.start", result: {} },
    ]);
    const client = new BuilderApiClient(transport);

    await expect(client.getReadiness()).resolves.toMatchObject({ ready: true, serverID: "server-1" });
    await client.startTask("task-1");

    expect(transport.calls).toContainEqual({ method: "workflow.task.start", params: { task_id: "task-1" } });
  });

  it("rejects server contract drift before feature code receives raw data", async () => {
    const client = new BuilderApiClient(new FakeRpcTransport([{ method: "server.readiness.get", result: { ready: true } }]));

    await expect(client.getReadiness()).rejects.toBeInstanceOf(ContractError);
  });
});
