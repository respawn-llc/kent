import { create } from "@app/server-api-contract";
import { SettingsService } from "@app/server-api-contract/gen/kent/api/runtime/runtime_pb";
import { FakeRpcTransport, unexpectedProjectOverflow } from "@/test-support/api";
import { ApiClient } from "./client";

it("sets an existing Session name through the attached typed settings route", async () => {
  const method = SettingsService.method.setSessionName;
  const transport = new FakeRpcTransport([
    { descriptor: method, result: create(method.output, { outcome: { case: "success", value: {} } }) },
  ]);
  const target = {
    kind: "session",
    projectID: "project-1",
    sessionID: "123e4567-e89b-42d3-a456-426614174000",
  } as const;
  await new ApiClient(transport, unexpectedProjectOverflow).chat.setSessionName(target, {
    kind: "set",
    name: "Release review",
  });
  expect(transport.attachedSessionCalls).toHaveLength(1);
  expect(transport.descriptorCalls[0]?.request).toMatchObject({
    sessionId: target.sessionID,
    mutation: { action: { case: "set", value: "Release review" } },
  });
});

it("clears a name with a structural Clear rather than a blank Set", async () => {
  const method = SettingsService.method.setSessionName;
  const transport = new FakeRpcTransport([
    { descriptor: method, result: create(method.output, { outcome: { case: "success", value: {} } }) },
  ]);
  await new ApiClient(transport, unexpectedProjectOverflow).chat.setSessionName(
    { projectID: "project-1", sessionID: "123e4567-e89b-42d3-a456-426614174000" },
    { kind: "clear" },
  );
  expect(transport.descriptorCalls[0]?.request).toMatchObject({
    mutation: { action: { case: "clear", value: {} } },
  });
});
